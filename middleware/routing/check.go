package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/seriouscodehere/open-source/middleware/base"
	"github.com/seriouscodehere/open-source/middleware/helper"

	"github.com/go-chi/chi/v5"
)

// CheckHandler handles rate limit check requests from SDK clients
type CheckHandler struct {
	Limiter  *base.Limiter
	Config   base.Config
	Registry base.RuleRegistry
}

// NewCheckHandler creates a new check handler
func NewCheckHandler(limiter *base.Limiter, registry base.RuleRegistry) *CheckHandler {
	return &CheckHandler{
		Limiter:  limiter,
		Config:   limiter.Config,
		Registry: registry,
	}
}

// Routes returns the router with check endpoint
func (h *CheckHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.CheckRateLimit)
	return r
}

// CheckRequest matches your SDK's CheckRequest struct
type CheckRequest struct {
	ServiceID string            `json:"service_id"`
	Endpoint  string            `json:"endpoint"`
	IP        string            `json:"ip"`
	UserID    string            `json:"user_id,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// CheckResponse matches your SDK's CheckResponse struct
type CheckResponse struct {
	Allowed        bool   `json:"allowed"`
	Blocked        bool   `json:"blocked"`
	BlockRemaining int    `json:"block_remaining_seconds"`
	RetryAfter     int    `json:"retry_after_seconds"`
	Reason         string `json:"reason"`
}

// CheckRateLimit performs a rate limit check without consuming quota (dry-run)
func (h *CheckHandler) CheckRateLimit(w http.ResponseWriter, r *http.Request) {
	var req CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ServiceID == "" || req.Endpoint == "" || req.IP == "" {
		http.Error(w, `{"error": "service_id, endpoint, and ip are required"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	ip := helper.NormalizeIP(req.IP, h.Config.EnableIPv6SubnetBlock, h.Config.IPv6SubnetSize)

	// Determine limit key (user ID takes precedence if provided)
	limitKey := ip
	if req.UserID != "" {
		limitKey = "user:" + req.UserID
	}

	// Check if currently blocked
	blocked, blockedUntil, err := h.isBlocked(ctx, limitKey)
	if err != nil {
		h.respondWithError(w, "backend_error", http.StatusInternalServerError)
		return
	}

	if blocked {
		remaining := int(time.Until(blockedUntil).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		h.respond(w, CheckResponse{
			Allowed:        false,
			Blocked:        true,
			BlockRemaining: remaining,
			RetryAfter:     remaining,
			Reason:         "ip_blocked",
		})
		return
	}

	// Check bot detection if enabled
	if isBot, reason := h.detectBot(req.Headers); isBot {
		h.respond(w, CheckResponse{
			Allowed:        false,
			Blocked:        true,
			BlockRemaining: int(h.Config.BlockDuration.Seconds()),
			RetryAfter:     int(h.Config.BlockDuration.Seconds()),
			Reason:         "bot_detected:" + reason,
		})
		return
	}

	// Get rate limits from registry or config
	rps, burst, windowSize, _, useSlidingWindow := h.getRateLimits(req.Endpoint, r.Method)

	// Perform dry-run rate limit check (peek without incrementing)
	allowed, retryAfter, err := h.checkLimitDryRun(ctx, limitKey, rps, burst, windowSize, useSlidingWindow)
	if err != nil {
		h.respondWithError(w, "backend_error", http.StatusInternalServerError)
		return
	}

	if !allowed {
		retrySecs := int(retryAfter.Seconds())
		h.respond(w, CheckResponse{
			Allowed:        false,
			Blocked:        false, // Not blocked, just rate limited
			BlockRemaining: 0,
			RetryAfter:     retrySecs,
			Reason:         "rate_limit_exceeded",
		})
		return
	}

	// All checks passed
	h.respond(w, CheckResponse{
		Allowed:        true,
		Blocked:        false,
		BlockRemaining: 0,
		RetryAfter:     0,
		Reason:         "",
	})
}

// checkLimitDryRun checks if request would be allowed without consuming quota
func (h *CheckHandler) checkLimitDryRun(ctx context.Context, key string, rps float64, burst int, windowSize time.Duration, useSlidingWindow bool) (bool, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	// For dry-run, we check current count vs limit without incrementing
	limit := int(rps * windowSize.Seconds())
	if limit < burst {
		limit = burst
	}

	// Get current count in window
	currentCount, err := h.Limiter.Backend.GetSlidingWindowCount(ctx, key, windowSize)
	if err != nil {
		return false, 0, err
	}

	if currentCount >= limit {
		// Calculate retry after based on window expiration
		// This is an approximation - in production you'd want exact TTL
		retryAfter := windowSize / time.Duration(limit)
		return false, retryAfter, nil
	}

	return true, 0, nil
}

// getRateLimits determines rate limits (reuses logic from middleware)
func (h *CheckHandler) getRateLimits(path, method string) (rps float64, burst int, windowSize, blockDuration time.Duration, useSlidingWindow bool) {
	// Default to static config
	rps = h.Config.RequestsPerSecond
	burst = h.Config.BurstSize
	windowSize = h.Config.SlidingWindowSize
	blockDuration = h.Config.BlockDuration
	useSlidingWindow = true

	// Check static path limits
	for _, pathLimit := range h.Config.PathLimits {
		if strings.HasPrefix(path, pathLimit.Prefix) {
			rps = pathLimit.Config.RequestsPerSecond
			burst = pathLimit.Config.BurstSize
			if pathLimit.Config.WindowSize > 0 {
				windowSize = pathLimit.Config.WindowSize
			}
			return
		}
	}

	// Check dynamic registry rules
	if h.Registry != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		rule, err := h.Registry.GetCompiledRule(ctx, path)
		if err == nil && rule != nil && !rule.IsExcluded {
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
				useSlidingWindow = true
			}
		}
	}

	return
}

// isBlocked checks if IP/user is currently blocked
func (h *CheckHandler) isBlocked(ctx context.Context, key string) (bool, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	return h.Limiter.Backend.IsBlocked(ctx, key)
}

// detectBot checks for bot signatures in headers
func (h *CheckHandler) detectBot(headers map[string]string) (bool, string) {
	if h.Config.BotDetection == base.DetectionNone {
		return false, ""
	}

	ua := strings.ToLower(headers["User-Agent"])
	if ua == "" {
		return true, "missing_user_agent"
	}

	botSignatures := []string{
		"bot", "crawler", "spider", "scraper", "curl", "wget", "httpclient",
		"python-requests", "axios", "postman", "insomnia", "puppeteer", "selenium",
		"playwright", "headless", "phantomjs", "slimerjs",
	}

	for _, sig := range botSignatures {
		if strings.Contains(ua, sig) {
			return true, "bot_signature:" + sig
		}
	}

	// Medium/High detection checks
	if h.Config.BotDetection >= base.DetectionMedium {
		if headers["Accept"] == "" || headers["Accept-Language"] == "" {
			return true, "missing_standard_headers"
		}
	}

	return false, ""
}

func (h *CheckHandler) respond(w http.ResponseWriter, resp CheckResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *CheckHandler) respondWithError(w http.ResponseWriter, reason string, status int) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(CheckResponse{
		Allowed: false,
		Blocked: true,
		Reason:  reason,
	})
}
