// sdk/go/middleware.go
package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

func (c *Client) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)

		// Build headers map from request
		headers := map[string]string{
			"User-Agent": r.UserAgent(),
			"Accept":     r.Header.Get("Accept"),
		}

		result, err := c.Check(r.Context(), r.URL.Path, ip, headers)

		if err != nil {
			// Log error but don't block on SDK errors - fail open or closed based on your policy
			// Here we fail open (allow) to prevent SDK issues from breaking the app
			// Change to http.Error if you prefer fail-closed
			next.ServeHTTP(w, r)
			return
		}

		if !result.Allowed {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", result.RetryAfter))

			status := http.StatusTooManyRequests
			if result.Blocked {
				status = http.StatusForbidden
			}

			http.Error(w, fmt.Sprintf(`{"error": "%s", "retry_after": %d}`, result.Reason, result.RetryAfter), status)
			return
		}

		w.Header().Set("X-RateLimit-Status", "allowed")
		next.ServeHTTP(w, r)
	})
}

func getClientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take first IP if multiple
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return xff
	}

	xri := r.Header.Get("X-Real-Ip")
	if xri != "" {
		return xri
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
