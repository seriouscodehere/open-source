// sdk/go/middleware.go
package ratelimit

import (
	"fmt"
	"net"
	"net/http"
)

func (c *Client) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)

		result, err := c.Check(r.Context(), r.URL.Path, ip, map[string]string{
			"User-Agent": r.UserAgent(),
		})

		if err != nil {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return
		}

		if !result.Allowed {
			// REMOVED: Challenge handling - no longer supported
			// Simple rate limit response only
			w.Header().Set("Retry-After", fmt.Sprintf("%d", result.RetryAfter))
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		w.Header().Set("X-RateLimit-Status", "allowed")
		next.ServeHTTP(w, r)
	})
}

func getClientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		return xff
	}

	xri := r.Header.Get("X-Real-Ip")
	if xri != "" {
		return xri
	}

	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}
