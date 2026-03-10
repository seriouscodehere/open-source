package routing

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"middleware/metrics"

	"github.com/go-chi/chi/v5"
)

// MetricsHandler handles metrics and monitoring endpoints
type MetricsHandler struct {
	Collector *metrics.MetricsCollector
}

// NewMetricsHandler creates a new metrics handler
func NewMetricsHandler(collector *metrics.MetricsCollector) *MetricsHandler {
	return &MetricsHandler{
		Collector: collector,
	}
}

// Routes returns the router with all metrics routes
func (h *MetricsHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// List all APIs with basic metrics
	r.Get("/", h.ListAPIs)

	// Specific API metrics with time range
	r.Get("/{apiID}", h.GetAPIMetrics)
	r.Get("/{apiID}/current", h.GetCurrentMetrics)     // Real-time current hour
	r.Get("/{apiID}/today", h.GetTodayMetrics)         // Today only
	r.Get("/{apiID}/yesterday", h.GetYesterdayMetrics) // Yesterday
	r.Get("/{apiID}/week", h.GetLastWeekMetrics)       // Last 7 days
	r.Get("/{apiID}/month", h.GetLastMonthMetrics)     // Last 30 days
	r.Get("/{apiID}/year", h.GetLastYearMetrics)       // Last 365 days

	// Custom range
	r.Get("/{apiID}/range", h.GetCustomRangeMetrics)

	// Real-time streaming (SSE)
	r.Get("/{apiID}/stream", h.StreamMetrics)

	// Export metrics
	r.Get("/{apiID}/export", h.ExportMetrics)

	return r
}

// ListAPIs returns all APIs with their current metrics summary
func (h *MetricsHandler) ListAPIs(w http.ResponseWriter, r *http.Request) {
	apis := h.Collector.GetAllAPIs()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"apis":         apis,
		"count":        len(apis),
		"generated_at": time.Now(),
	})
}

// GetAPIMetrics handles generic metrics query with query parameters
func (h *MetricsHandler) GetAPIMetrics(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")

	// Parse time range from query params
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	rangeStr := r.URL.Query().Get("range") // "1h", "24h", "7d", "30d", "1y"

	now := time.Now()
	var from, to time.Time

	if fromStr != "" && toStr != "" {
		// Custom date range
		from, _ = time.Parse(time.RFC3339, fromStr)
		to, _ = time.Parse(time.RFC3339, toStr)
	} else if rangeStr != "" {
		// Relative range
		from, to = parseRange(rangeStr, now)
	} else {
		// Default last 24 hours
		from = now.Add(-24 * time.Hour)
		to = now
	}

	if from.IsZero() {
		from = now.Add(-24 * time.Hour)
	}
	if to.IsZero() {
		to = now
	}

	result, err := h.Collector.QueryMetrics(apiID, from, to)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	result.QueryRange = rangeStr
	if result.QueryRange == "" {
		result.QueryRange = "custom"
	}

	json.NewEncoder(w).Encode(result)
}

// GetCurrentMetrics returns current hour metrics
func (h *MetricsHandler) GetCurrentMetrics(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")
	now := time.Now()
	from := now.Truncate(time.Hour)
	to := from.Add(time.Hour)

	result, err := h.Collector.QueryMetrics(apiID, from, to)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	result.QueryRange = "current_hour"
	json.NewEncoder(w).Encode(result)
}

// GetTodayMetrics returns today's metrics
func (h *MetricsHandler) GetTodayMetrics(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")
	now := time.Now()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	to := now

	result, err := h.Collector.QueryMetrics(apiID, from, to)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	result.QueryRange = "today"
	json.NewEncoder(w).Encode(result)
}

// GetYesterdayMetrics returns yesterday's metrics
func (h *MetricsHandler) GetYesterdayMetrics(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	from := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, yesterday.Location())
	to := from.Add(24 * time.Hour)

	result, err := h.Collector.QueryMetrics(apiID, from, to)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	result.QueryRange = "yesterday"
	json.NewEncoder(w).Encode(result)
}

// GetLastWeekMetrics returns last 7 days metrics
func (h *MetricsHandler) GetLastWeekMetrics(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")
	now := time.Now()
	from := now.AddDate(0, 0, -7)
	to := now

	result, err := h.Collector.QueryMetrics(apiID, from, to)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	result.QueryRange = "last_7_days"
	json.NewEncoder(w).Encode(result)
}

// GetLastMonthMetrics returns last 30 days metrics
func (h *MetricsHandler) GetLastMonthMetrics(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")
	now := time.Now()
	from := now.AddDate(0, 0, -30)
	to := now

	result, err := h.Collector.QueryMetrics(apiID, from, to)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	result.QueryRange = "last_30_days"
	json.NewEncoder(w).Encode(result)
}

// GetLastYearMetrics returns last 365 days metrics
func (h *MetricsHandler) GetLastYearMetrics(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")
	now := time.Now()
	from := now.AddDate(-1, 0, 0)
	to := now

	result, err := h.Collector.QueryMetrics(apiID, from, to)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	result.QueryRange = "last_year"
	json.NewEncoder(w).Encode(result)
}

// GetCustomRangeMetrics handles custom date ranges
func (h *MetricsHandler) GetCustomRangeMetrics(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")

	daysStr := r.URL.Query().Get("days")
	hoursStr := r.URL.Query().Get("hours")

	now := time.Now()
	var from time.Time

	if daysStr != "" {
		days, _ := strconv.Atoi(daysStr)
		from = now.AddDate(0, 0, -days)
	} else if hoursStr != "" {
		hours, _ := strconv.Atoi(hoursStr)
		from = now.Add(-time.Duration(hours) * time.Hour)
	} else {
		// Default 3 days
		from = now.AddDate(0, 0, -3)
	}

	to := now

	result, err := h.Collector.QueryMetrics(apiID, from, to)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	result.QueryRange = "custom"
	json.NewEncoder(w).Encode(result)
}

// StreamMetrics provides Server-Sent Events for real-time metrics
func (h *MetricsHandler) StreamMetrics(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send initial data
	h.sendMetricsEvent(w, flusher, apiID, "connected")

	// Stream updates every 5 seconds
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.sendMetricsEvent(w, flusher, apiID, "update")
		case <-r.Context().Done():
			return
		}
	}
}

func (h *MetricsHandler) sendMetricsEvent(w http.ResponseWriter, flusher http.Flusher, apiID, eventType string) {
	now := time.Now()
	from := now.Truncate(time.Hour)
	to := from.Add(time.Hour)

	result, err := h.Collector.QueryMetrics(apiID, from, to)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", `{"error": "`+err.Error()+`"}`)
		flusher.Flush()
		return
	}

	result.QueryRange = "realtime"
	data, _ := json.Marshal(map[string]interface{}{
		"type":      eventType,
		"timestamp": time.Now().Format(time.RFC3339),
		"metrics":   result,
	})

	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// ExportMetrics returns raw metrics data for export
func (h *MetricsHandler) ExportMetrics(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	// Get full metrics data
	filePath := h.Collector.GetMetricsFilePath(apiID)
	data, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, `{"error": "metrics file not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s_metrics.json", apiID))
	w.Write(data)
}

// parseRange converts relative time strings to actual times
func parseRange(rangeStr string, now time.Time) (from, to time.Time) {
	to = now

	switch rangeStr {
	case "1h":
		from = now.Add(-1 * time.Hour)
	case "6h":
		from = now.Add(-6 * time.Hour)
	case "12h":
		from = now.Add(-12 * time.Hour)
	case "24h", "1d":
		from = now.Add(-24 * time.Hour)
	case "3d":
		from = now.AddDate(0, 0, -3)
	case "7d", "1w":
		from = now.AddDate(0, 0, -7)
	case "30d", "1m":
		from = now.AddDate(0, 0, -30)
	case "90d", "3m":
		from = now.AddDate(0, 0, -90)
	case "1y", "365d":
		from = now.AddDate(-1, 0, 0)
	default:
		from = now.Add(-24 * time.Hour)
	}

	return
}
