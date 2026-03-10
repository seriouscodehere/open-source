package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"middleware/base"
	"middleware/helper"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

type Middleware struct {
	Limiter  *base.Limiter
	Config   base.Config
	Registry base.RuleRegistry
}

// NewMiddleware creates middleware with optional registry for dynamic rules
func NewMiddleware(limiter *base.Limiter, registry base.RuleRegistry) *Middleware {
	return &Middleware{
		Limiter:  limiter,
		Config:   limiter.Config,
		Registry: registry,
	}
}

// CORSMiddleware returns CORS middleware based on config
func (m *Middleware) CORSMiddleware() func(http.Handler) http.Handler {
	if !m.Config.CORSEnabled {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return cors.Handler(cors.Options{
		AllowedOrigins:   m.Config.CORSAllowedOrigins,
		AllowedMethods:   m.Config.CORSAllowedMethods,
		AllowedHeaders:   m.Config.CORSAllowedHeaders,
		MaxAge:           m.Config.CORSMaxAge,
		AllowCredentials: true,
	})
}

// AdminRouter creates legacy admin routes
func AdminRouter(limiter *base.Limiter) http.Handler {
	r := chi.NewRouter()

	// Authentication middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			expectedToken := "Bearer " + limiter.Config.AdminAuthToken
			if token != expectedToken || limiter.Config.AdminAuthToken == "" {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	r.Get("/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := map[string]interface{}{
			"allowed":      atomic.LoadInt64(&limiter.Allowed),
			"blocked":      atomic.LoadInt64(&limiter.Blocked),
			"bot_blocked":  atomic.LoadInt64(&limiter.BotBlocked),
			"in_flight":    atomic.LoadInt64(&limiter.InFlight),
			"window_start": limiter.TrafficStats.WindowStart,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	r.Post("/unblock", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			IP string `json:"ip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error": "invalid request"}`, http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := limiter.Backend.Block(ctx, req.IP, -1*time.Second)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "unblocked"})
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	return r
}

func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&m.Limiter.InFlight, 1)
		defer atomic.AddInt64(&m.Limiter.InFlight, -1)

		// CRITICAL: Preserve request body for downstream handlers (including proxy)
		var bodyBytes []byte
		var err error
		if r.Body != nil {
			bodyBytes, err = io.ReadAll(r.Body)
			if err != nil {
				m.Config.Logger.Error("Failed to read request body", "error", err)
				http.Error(w, "Bad Request", http.StatusBadRequest)
				return
			}
			r.Body.Close()

			// Check body size limit
			if m.Config.MaxRequestBodySize > 0 && int64(len(bodyBytes)) > m.Config.MaxRequestBodySize {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}

			// Restore body for current handler and downstream
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			r.ContentLength = int64(len(bodyBytes))
		}

		ctx := r.Context()
		ip := m.Limiter.Extractor.Extract(r)
		ip = helper.NormalizeIP(ip, m.Config.EnableIPv6SubnetBlock, m.Config.IPv6SubnetSize)

		limitKey := ip
		if m.Config.UserIDHeader != "" && m.Config.TrustedUserIDHeader {
			if m.Config.ProxyValidationFunc == nil || m.Config.ProxyValidationFunc(r) {
				if userID := r.Header.Get(m.Config.UserIDHeader); userID != "" {
					limitKey = "user:" + userID
				}
			}
		}

		// Get rate limits - check dynamic registry first, then fallback to static config
		rps, burst, windowSize, blockDuration, useSlidingWindow := m.getRateLimits(r)

		// Global rate limit
		if m.Config.GlobalRateLimit > 0 {
			var globalAllowed bool
			var globalRetryAfter time.Duration
			var err error

			// Always use sliding window for global limits
			globalLimit := int(m.Config.GlobalRateLimit * m.Config.GlobalWindowSize.Seconds())
			globalAllowed, globalRetryAfter, err = m.Limiter.Backend.AllowSlidingWindow(ctx, "global", globalLimit, m.Config.GlobalWindowSize)

			if err != nil {
				m.Config.Logger.Error("Global limit backend error", "error", err)
				if m.Config.StrictBackendErrors {
					m.httpError(w, base.ErrBackendUnavailable, http.StatusServiceUnavailable, 0, ip)
					return
				}
			} else if !globalAllowed {
				m.Config.Metrics.RateLimitHit(ip, r.URL.Path)
				atomic.AddInt64(&m.Limiter.Blocked, 1)
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.0f", m.Config.GlobalRateLimit))
				w.Header().Set("X-RateLimit-Remaining", "0")
				m.httpError(w, base.ErrRateLimitExceeded, http.StatusTooManyRequests, globalRetryAfter, ip)
				return
			}
		}

		// Distributed attack check
		if m.Limiter.DistributedAttackCounter.Record() {
			m.Config.Metrics.DistributedAttackDetected(int(atomic.LoadInt64(&m.Limiter.Blocked)))
			m.Config.Logger.Warn("Distributed attack detected", "ip", ip)
		}

		if blocked, retryAfter, err := m.isBlocked(ctx, limitKey); blocked {
			m.Config.Metrics.RateLimitHit(ip, r.URL.Path)
			atomic.AddInt64(&m.Limiter.Blocked, 1)
			m.httpError(w, base.ErrIPBlocked, http.StatusForbidden, retryAfter, ip)
			return
		} else if err != nil && m.Config.StrictBackendErrors {
			m.Config.Logger.Error("Backend error checking block status", "error", err)
			m.httpError(w, base.ErrBackendUnavailable, http.StatusServiceUnavailable, 0, ip)
			return
		}

		if isBot, reason := m.detectBot(r); isBot {
			m.Config.Metrics.BotDetected(ip, reason)
			m.Config.Logger.Warn("Bot detected", "ip", ip, "reason", reason)
			atomic.AddInt64(&m.Limiter.BotBlocked, 1)
			m.blockIP(ctx, limitKey, m.Config.BlockDuration)
			m.httpError(w, base.ErrBotDetected, http.StatusForbidden, m.Config.BlockDuration, ip)
			return
		}

		allowed, retryAfter, err := m.allowRequest(ctx, limitKey, rps, burst, windowSize, blockDuration)

		if err != nil {
			m.Config.Logger.Error("Backend error", "error", err)
			m.Config.Metrics.BackendFailure(err.Error())
			if m.Config.StrictBackendErrors {
				m.httpError(w, base.ErrBackendUnavailable, http.StatusServiceUnavailable, 0, ip)
				return
			}
		} else if !allowed {
			m.Config.Metrics.RateLimitHit(ip, r.URL.Path)
			atomic.AddInt64(&m.Limiter.Blocked, 1)
			m.httpError(w, base.ErrRateLimitExceeded, http.StatusTooManyRequests, retryAfter, ip)
			return
		}

		atomic.AddInt64(&m.Limiter.Allowed, 1)
		m.Config.Metrics.RequestAllowed(ip, r.URL.Path)
		m.updateTrafficStats()

		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.0f", rps))

		var remaining float64
		if useSlidingWindow {
			count, _ := m.Limiter.Backend.GetSlidingWindowCount(ctx, limitKey, windowSize)
			remaining = float64(burst - count)
		} else {
			remaining = m.getRemainingTokens(ctx, limitKey)
		}
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%.0f", math.Max(0, remaining)))

		m.clearBlockCookie(w)

		// CRITICAL: Restore body again before calling next handler (in case it was consumed)
		if len(bodyBytes) > 0 {
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			r.ContentLength = int64(len(bodyBytes))
		}

		next.ServeHTTP(w, r)
	})
}

// getRateLimits determines rate limits from registry or static config
func (m *Middleware) getRateLimits(r *http.Request) (rps float64, burst int, windowSize, blockDuration time.Duration, useSlidingWindow bool) {
	path := r.URL.Path
	method := r.Method

	// Default to static config
	rps = m.Config.RequestsPerSecond
	burst = m.Config.BurstSize
	windowSize = m.Config.SlidingWindowSize
	blockDuration = m.Config.BlockDuration
	useSlidingWindow = true // Always default to true

	// Check static path limits first
	for _, pathLimit := range m.Config.PathLimits {
		if strings.HasPrefix(path, pathLimit.Prefix) {
			rps = pathLimit.Config.RequestsPerSecond
			burst = pathLimit.Config.BurstSize
			if pathLimit.Config.WindowSize > 0 {
				windowSize = pathLimit.Config.WindowSize
			}
			return
		}
	}

	// If registry available, check dynamic rules
	if m.Registry != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		rule, err := m.Registry.GetCompiledRule(ctx, path)
		if err == nil && rule != nil && !rule.IsExcluded {
			// Check method match
			if rule.Method == "*" || rule.Method == method || rule.Method == "" {
				if rule.RequestsPerSecond > 0 {
					rps = rule.RequestsPerSecond
				}
				if rule.BurstSize > 0 {
					burst = rule.BurstSize
				}
				if rule.WindowSize > 0 {
					windowSize = rule.WindowSize
				}
				if rule.BlockDuration > 0 {
					blockDuration = rule.BlockDuration
				}
				// Always use sliding window for API-based rules
				useSlidingWindow = true

				// REMOVED: auth_type check - no longer supported
				// All requests are processed through rate limiting regardless of auth

				// FIXED: Use structured logging properly
				m.Config.Logger.Debug("Using dynamic rate limit rule",
					"path", path,
					"api_id", rule.APIID,
					"endpoint_id", rule.EndpointID,
					"rps", fmt.Sprintf("%.2f", rps),
				)
			}
		}
	}

	return
}

func (m *Middleware) allowRequest(ctx context.Context, ip string, rps float64, burst int, windowSize, blockDuration time.Duration) (bool, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	var allowed bool
	var retryAfter time.Duration
	var err error

	// Always use sliding window for API-based rate limiting
	limit := int(rps * windowSize.Seconds())
	if limit < burst {
		limit = burst
	}
	allowed, retryAfter, err = m.Limiter.Backend.AllowSlidingWindow(ctx, ip, limit, windowSize)

	if err != nil {
		return false, 0, err
	}

	if !allowed && retryAfter == 0 {
		violations, err := m.Limiter.Backend.IncrementViolation(ctx, ip)
		if err != nil {
			m.Config.Logger.Warn("Failed to increment violation", "error", err)
		}

		duration := m.calculateBlockDuration(violations, blockDuration)
		m.Limiter.Backend.Block(ctx, ip, duration)
		m.Config.Metrics.BlockApplied(ip, duration)
		m.Config.Logger.Info("IP blocked", "ip", ip, "duration", duration, "violations", violations)
		return false, duration, nil
	}

	return allowed, retryAfter, nil
}

func (m *Middleware) calculateBlockDuration(violations int32, baseDuration time.Duration) time.Duration {
	if baseDuration == 0 {
		baseDuration = m.Config.BlockDuration
	}

	if violations > 1 {
		multiplier := math.Pow(m.Config.ExponentialBackoffBase, float64(violations-1))
		baseDuration = time.Duration(float64(baseDuration) * multiplier)
	}

	if m.Config.MaxBlockDuration > 0 && baseDuration > m.Config.MaxBlockDuration {
		baseDuration = m.Config.MaxBlockDuration
	}

	if m.Config.EnableJitter {
		jitter := time.Duration(float64(baseDuration) * m.Config.JitterMaxPercent * (float64(time.Now().UnixNano()%100) / 100.0))
		baseDuration = baseDuration + jitter
	}

	return baseDuration
}

func (m *Middleware) isBlocked(ctx context.Context, ip string) (bool, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	blocked, blockedUntil, err := m.Limiter.Backend.IsBlocked(ctx, ip)
	if err != nil {
		return false, 0, err
	}
	if blocked {
		retryAfter := time.Until(blockedUntil)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return true, retryAfter, nil
	}
	return false, 0, nil
}

func (m *Middleware) blockIP(ctx context.Context, ip string, duration time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := m.Limiter.Backend.Block(ctx, ip, duration); err != nil {
		m.Config.Logger.Error("Failed to block IP", "ip", ip, "error", err)
	}
}

func (m *Middleware) getRemainingTokens(ctx context.Context, ip string) float64 {
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	tokens, _, err := m.Limiter.Backend.GetTokenBucket(ctx, ip)
	if err != nil {
		m.Config.Logger.Debug("Failed to get remaining tokens", "error", err)
		return 0
	}
	return tokens
}

func (m *Middleware) detectBot(r *http.Request) (bool, string) {
	switch m.Config.BotDetection {
	case base.DetectionNone:
		return false, ""
	case base.DetectionLow:
		return m.basicBotCheck(r)
	case base.DetectionMedium:
		return m.mediumBotCheck(r)
	case base.DetectionHigh:
		return m.aggressiveBotCheck(r)
	default:
		return false, ""
	}
}

func (m *Middleware) basicBotCheck(r *http.Request) (bool, string) {
	ua := strings.ToLower(r.UserAgent())
	if ua == "" {
		return true, "missing_user_agent"
	}

	// Default signatures
	botSignatures := []string{
		"bot", "crawler", "spider", "scraper", "curl", "wget", "httpclient",
		"python-requests", "axios", "postman", "insomnia", "puppeteer", "selenium",
		"playwright", "headless", "phantomjs", "slimerjs",
	}

	// Add custom signatures from config
	botSignatures = append(botSignatures, m.Config.AdditionalBotSignatures...)

	for _, sig := range botSignatures {
		if strings.Contains(ua, sig) {
			return true, "bot_signature:" + sig
		}
	}
	return false, ""
}

func (m *Middleware) mediumBotCheck(r *http.Request) (bool, string) {
	if isBot, reason := m.basicBotCheck(r); isBot {
		return true, reason
	}
	if r.Header.Get("Accept") == "" || r.Header.Get("Accept-Language") == "" {
		return true, "missing_standard_headers"
	}
	secFetch := r.Header.Get("Sec-Fetch-Site")
	if secFetch != "" {
		if r.Header.Get("Sec-Fetch-Mode") == "" {
			return true, "incomplete_sec_fetch_headers"
		}
	}
	if strings.Contains(r.UserAgent(), "Chrome") && r.Header.Get("Sec-Ch-Ua") == "" {
		return true, "missing_client_hints"
	}
	return false, ""
}

func (m *Middleware) aggressiveBotCheck(r *http.Request) (bool, string) {
	if isBot, reason := m.mediumBotCheck(r); isBot {
		return true, reason
	}

	if m.Config.BackendType == base.BackendMemory {
		ip := m.Limiter.Extractor.Extract(r)
		key := helper.NormalizeIP(ip, m.Config.EnableIPv6SubnetBlock, m.Config.IPv6SubnetSize)

		result, _, _ := m.Limiter.SF.Do("stats:"+key, func() (interface{}, error) {
			return m.Limiter.Backend.GetViolationCount(context.Background(), key)
		})

		if violations, ok := result.(int32); ok && violations > 5 {
			return true, "high_violation_count"
		}
	}
	return false, ""
}

func (m *Middleware) updateTrafficStats() {
	m.Limiter.TrafficStats.Mu.Lock()
	m.Limiter.TrafficStats.RequestCount++
	m.Limiter.TrafficStats.Mu.Unlock()
}

func (m *Middleware) httpError(w http.ResponseWriter, err error, status int, retryAfter time.Duration, ip string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.0f", m.Config.RequestsPerSecond))
	w.Header().Set("X-RateLimit-Remaining", "0")

	if retryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%.0f", math.Ceil(retryAfter.Seconds())))
		if status == http.StatusForbidden || status == http.StatusTooManyRequests {
			blockedUntil := time.Now().Add(retryAfter)
			reason := "rate_limited"
			switch err {
			case base.ErrBotDetected:
				reason = "bot_detected"
			case base.ErrIPBlocked:
				reason = "ip_blocked"
			}
			m.setBlockCookie(w, ip, blockedUntil, reason)
		}
	}

	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":         err.Error(),
		"status":        status,
		"retry_after":   math.Ceil(retryAfter.Seconds()),
		"blocked_until": time.Now().Add(retryAfter).Unix(),
	})
}

func (m *Middleware) setBlockCookie(w http.ResponseWriter, ip string, blockedUntil time.Time, reason string) {
	if !m.Config.BlockStatusCookieEnabled || m.Config.BlockStatusCookieName == "" {
		return
	}

	status := helper.BlockStatusCookie{
		IP:           ip,
		BlockedUntil: blockedUntil,
		Reason:       reason,
	}

	cookie := &http.Cookie{
		Name:     m.Config.BlockStatusCookieName,
		Value:    helper.EncodeBlockStatus(status),
		Path:     "/",
		Expires:  blockedUntil,
		HttpOnly: false,
		Secure:   m.Config.CookieSecure,
		SameSite: m.Config.CookieSameSite,
	}
	http.SetCookie(w, cookie)
}

func (m *Middleware) clearBlockCookie(w http.ResponseWriter) {
	if !m.Config.BlockStatusCookieEnabled || m.Config.BlockStatusCookieName == "" {
		return
	}

	cookie := &http.Cookie{
		Name:     m.Config.BlockStatusCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: false,
		Secure:   m.Config.CookieSecure,
		SameSite: m.Config.CookieSameSite,
	}
	http.SetCookie(w, cookie)
}
