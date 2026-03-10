package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RuleRegistry defines the minimal interface needed by metrics (decoupled from base package)
type RuleRegistry interface {
	GetAPI(ctx context.Context, id string) (*RegistryAPIInfo, error)
	ExportAll(ctx context.Context) ([]RegistryAPIInfo, error)
}

// RegistryAPIInfo contains minimal API information needed FROM the registry (input from adapter)
type RegistryAPIInfo struct {
	ID          string
	BasePath    string
	UpstreamURL string
	Endpoints   []EndpointInfo
}

// EndpointInfo contains minimal endpoint information
type EndpointInfo struct {
	ID     string
	Path   string
	Method string
}

// APIMetrics tracks request metrics for a single API (internal storage structure)
type APIMetrics struct {
	APIID         string                     `json:"api_id"`
	APIPath       string                     `json:"api_path"`
	UpstreamURL   string                     `json:"upstream_url"`
	LastUpdated   time.Time                  `json:"last_updated"`
	HourlyStats   map[string]HourlyBucket    `json:"hourly_stats"`
	DailyStats    map[string]DailyBucket     `json:"daily_stats"`
	TotalStats    TotalStats                 `json:"total_stats"`
	EndpointStats map[string]EndpointMetrics `json:"endpoint_stats"`
}

// HourlyBucket contains metrics for a specific hour
type HourlyBucket struct {
	Hour            string           `json:"hour"`
	Date            string           `json:"date"`
	Timestamp       time.Time        `json:"timestamp"`
	TotalRequests   int64            `json:"total_requests"`
	Allowed         int64            `json:"allowed"`
	Blocked         int64            `json:"blocked"`
	BotBlocked      int64            `json:"bot_blocked"`
	RateLimited     int64            `json:"rate_limited"`
	AvgResponseTime float64          `json:"avg_response_time_ms"`
	StatusCodes     map[int]int64    `json:"status_codes"`
	UniqueIPs       int              `json:"unique_ips"`
	IPs             map[string]int64 `json:"-"`
}

// DailyBucket aggregates hourly data for a day
type DailyBucket struct {
	Date            string        `json:"date"`
	TotalRequests   int64         `json:"total_requests"`
	Allowed         int64         `json:"allowed"`
	Blocked         int64         `json:"blocked"`
	BotBlocked      int64         `json:"bot_blocked"`
	RateLimited     int64         `json:"rate_limited"`
	AvgResponseTime float64       `json:"avg_response_time_ms"`
	PeakHour        string        `json:"peak_hour"`
	PeakRequests    int64         `json:"peak_requests"`
	StatusCodes     map[int]int64 `json:"status_codes"`
	UniqueIPs       int           `json:"unique_ips"`
}

// TotalStats tracks all-time statistics
type TotalStats struct {
	FirstSeen        time.Time `json:"first_seen"`
	LastSeen         time.Time `json:"last_seen"`
	TotalRequests    int64     `json:"total_requests"`
	TotalAllowed     int64     `json:"total_allowed"`
	TotalBlocked     int64     `json:"total_blocked"`
	TotalBotBlocked  int64     `json:"total_bot_blocked"`
	TotalRateLimited int64     `json:"total_rate_limited"`
}

// EndpointMetrics tracks metrics per endpoint
type EndpointMetrics struct {
	EndpointID  string                  `json:"endpoint_id"`
	Path        string                  `json:"path"`
	Method      string                  `json:"method"`
	HourlyStats map[string]HourlyBucket `json:"hourly_stats"`
	DailyStats  map[string]DailyBucket  `json:"daily_stats"`
	TotalCalls  int64                   `json:"total_calls"`
}

// MetricsCollector manages metrics for all APIs
type MetricsCollector struct {
	mu            sync.RWMutex
	metricsDir    string
	apiMetrics    map[string]*APIMetrics
	registry      RuleRegistry
	flushInterval time.Duration
	retentionDays int
}

// MetricsQueryResult represents aggregated metrics response (HTTP API response)
type MetricsQueryResult struct {
	APIID              string            `json:"api_id"`
	APIPath            string            `json:"api_path"`
	UpstreamURL        string            `json:"upstream_url"`
	QueryRange         string            `json:"query_range"`
	From               time.Time         `json:"from"`
	To                 time.Time         `json:"to"`
	TotalRequests      int64             `json:"total_requests"`
	Allowed            int64             `json:"allowed"`
	Blocked            int64             `json:"blocked"`
	BotBlocked         int64             `json:"bot_blocked"`
	RateLimited        int64             `json:"rate_limited"`
	AvgRequestsPerHour float64           `json:"avg_requests_per_hour"`
	PeakRequests       int64             `json:"peak_requests"`
	PeakTime           string            `json:"peak_time"`
	StatusCodes        map[int]int64     `json:"status_codes"`
	Endpoints          []EndpointSummary `json:"endpoints"`
	HourlyBreakdown    []HourlyPoint     `json:"hourly_breakdown,omitempty"`
	DailyBreakdown     []DailyPoint      `json:"daily_breakdown,omitempty"`
}

// EndpointSummary for API responses
type EndpointSummary struct {
	EndpointID string `json:"endpoint_id"`
	Path       string `json:"path"`
	Method     string `json:"method"`
	Requests   int64  `json:"requests"`
	Blocked    int64  `json:"blocked"`
}

// HourlyPoint for time-series data
type HourlyPoint struct {
	Hour     string `json:"hour"`
	Requests int64  `json:"requests"`
	Allowed  int64  `json:"allowed"`
	Blocked  int64  `json:"blocked"`
}

// DailyPoint for time-series data
type DailyPoint struct {
	Date     string `json:"date"`
	Requests int64  `json:"requests"`
	Allowed  int64  `json:"allowed"`
	Blocked  int64  `json:"blocked"`
}

// RecordRequest represents a single request record for metrics
type RecordRequest struct {
	APIID        string
	EndpointID   string
	Path         string
	Method       string
	IP           string
	StatusCode   int
	Allowed      bool
	Blocked      bool
	BotBlocked   bool
	RateLimited  bool
	ResponseTime time.Duration
	Timestamp    time.Time
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(metricsDir string, registry RuleRegistry) (*MetricsCollector, error) {
	if err := os.MkdirAll(metricsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create metrics directory: %w", err)
	}

	mc := &MetricsCollector{
		metricsDir:    metricsDir,
		apiMetrics:    make(map[string]*APIMetrics),
		registry:      registry,
		flushInterval: 5 * time.Minute,
		retentionDays: 30,
	}

	if err := mc.LoadAll(); err != nil {
		return nil, err
	}

	go mc.backgroundFlush()

	return mc, nil
}

func (mc *MetricsCollector) Record(req RecordRequest) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	now := req.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	apiMetrics, exists := mc.apiMetrics[req.APIID]
	if !exists {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		api, err := mc.registry.GetAPI(ctx, req.APIID)
		cancel()

		apiMetrics = &APIMetrics{
			APIID:         req.APIID,
			HourlyStats:   make(map[string]HourlyBucket),
			DailyStats:    make(map[string]DailyBucket),
			EndpointStats: make(map[string]EndpointMetrics),
		}

		if err == nil && api != nil {
			apiMetrics.APIPath = api.BasePath
			apiMetrics.UpstreamURL = api.UpstreamURL
		}

		mc.apiMetrics[req.APIID] = apiMetrics
	}

	hourKey := now.Format("2006-01-02-15")
	dateKey := now.Format("2006-01-02")

	hourBucket := apiMetrics.HourlyStats[hourKey]
	hourBucket.Hour = hourKey
	hourBucket.Date = dateKey
	hourBucket.Timestamp = now.Truncate(time.Hour)
	hourBucket.TotalRequests++

	if req.Allowed {
		hourBucket.Allowed++
	}
	if req.Blocked {
		hourBucket.Blocked++
	}
	if req.BotBlocked {
		hourBucket.BotBlocked++
	}
	if req.RateLimited {
		hourBucket.RateLimited++
	}

	if hourBucket.StatusCodes == nil {
		hourBucket.StatusCodes = make(map[int]int64)
	}
	hourBucket.StatusCodes[req.StatusCode]++

	if hourBucket.IPs == nil {
		hourBucket.IPs = make(map[string]int64)
	}
	hourBucket.IPs[req.IP]++
	hourBucket.UniqueIPs = len(hourBucket.IPs)

	if req.ResponseTime > 0 {
		count := hourBucket.TotalRequests
		hourBucket.AvgResponseTime = (hourBucket.AvgResponseTime*float64(count-1) + float64(req.ResponseTime.Milliseconds())) / float64(count)
	}

	apiMetrics.HourlyStats[hourKey] = hourBucket

	mc.aggregateDaily(apiMetrics, dateKey)

	if apiMetrics.TotalStats.FirstSeen.IsZero() {
		apiMetrics.TotalStats.FirstSeen = now
	}
	apiMetrics.TotalStats.LastSeen = now
	apiMetrics.TotalStats.TotalRequests++
	if req.Allowed {
		apiMetrics.TotalStats.TotalAllowed++
	}
	if req.Blocked {
		apiMetrics.TotalStats.TotalBlocked++
	}
	if req.BotBlocked {
		apiMetrics.TotalStats.TotalBotBlocked++
	}
	if req.RateLimited {
		apiMetrics.TotalStats.TotalRateLimited++
	}

	if req.EndpointID != "" {
		mc.updateEndpointStats(apiMetrics, req, hourKey, dateKey)
	}

	apiMetrics.LastUpdated = now
}

func (mc *MetricsCollector) updateEndpointStats(apiMetrics *APIMetrics, req RecordRequest, hourKey, dateKey string) {
	epMetrics, exists := apiMetrics.EndpointStats[req.EndpointID]
	if !exists {
		epMetrics = EndpointMetrics{
			EndpointID:  req.EndpointID,
			Path:        req.Path,
			Method:      req.Method,
			HourlyStats: make(map[string]HourlyBucket),
			DailyStats:  make(map[string]DailyBucket),
		}
	}

	epHour := epMetrics.HourlyStats[hourKey]
	epHour.Hour = hourKey
	epHour.Date = dateKey
	epHour.Timestamp = time.Now().Truncate(time.Hour)
	epHour.TotalRequests++
	if req.Allowed {
		epHour.Allowed++
	}
	if req.Blocked {
		epHour.Blocked++
	}
	if epHour.StatusCodes == nil {
		epHour.StatusCodes = make(map[int]int64)
	}
	epHour.StatusCodes[req.StatusCode]++
	epMetrics.HourlyStats[hourKey] = epHour

	epDaily := epMetrics.DailyStats[dateKey]
	epDaily.Date = dateKey
	epDaily.TotalRequests++
	if req.Allowed {
		epDaily.Allowed++
	}
	if req.Blocked {
		epDaily.Blocked++
	}
	epMetrics.DailyStats[dateKey] = epDaily
	epMetrics.TotalCalls++

	apiMetrics.EndpointStats[req.EndpointID] = epMetrics
}

func (mc *MetricsCollector) aggregateDaily(apiMetrics *APIMetrics, dateKey string) {
	var totalReqs, totalAllowed, totalBlocked, totalBotBlocked, totalRateLimited int64
	var totalRespTime float64
	var hourCount int64
	peakRequests := int64(0)
	peakHour := ""
	statusCodes := make(map[int]int64)
	uniqueIPs := make(map[string]bool)

	for _, hour := range apiMetrics.HourlyStats {
		if hour.Date == dateKey {
			totalReqs += hour.TotalRequests
			totalAllowed += hour.Allowed
			totalBlocked += hour.Blocked
			totalBotBlocked += hour.BotBlocked
			totalRateLimited += hour.RateLimited
			totalRespTime += hour.AvgResponseTime
			hourCount++

			if hour.TotalRequests > peakRequests {
				peakRequests = hour.TotalRequests
				peakHour = hour.Hour
			}

			for code, count := range hour.StatusCodes {
				statusCodes[code] += count
			}

			for ip := range hour.IPs {
				uniqueIPs[ip] = true
			}
		}
	}

	daily := DailyBucket{
		Date:          dateKey,
		TotalRequests: totalReqs,
		Allowed:       totalAllowed,
		Blocked:       totalBlocked,
		BotBlocked:    totalBotBlocked,
		RateLimited:   totalRateLimited,
		PeakHour:      peakHour,
		PeakRequests:  peakRequests,
		StatusCodes:   statusCodes,
		UniqueIPs:     len(uniqueIPs),
	}

	if hourCount > 0 {
		daily.AvgResponseTime = totalRespTime / float64(hourCount)
	}

	apiMetrics.DailyStats[dateKey] = daily
}

func (mc *MetricsCollector) QueryMetrics(apiID string, from, to time.Time) (*MetricsQueryResult, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	apiMetrics, exists := mc.apiMetrics[apiID]
	if !exists {
		return nil, fmt.Errorf("no metrics found for API: %s", apiID)
	}

	result := &MetricsQueryResult{
		APIID:       apiID,
		APIPath:     apiMetrics.APIPath,
		UpstreamURL: apiMetrics.UpstreamURL,
		From:        from,
		To:          to,
		StatusCodes: make(map[int]int64),
		Endpoints:   make([]EndpointSummary, 0),
	}

	duration := to.Sub(from)

	if duration <= 24*time.Hour {
		result.HourlyBreakdown = mc.getHourlyBreakdown(apiMetrics, from, to)
	} else {
		result.DailyBreakdown = mc.getDailyBreakdown(apiMetrics, from, to)
	}

	for _, daily := range apiMetrics.DailyStats {
		dailyTime, _ := time.Parse("2006-01-02", daily.Date)
		if !dailyTime.Before(from) && !dailyTime.After(to) {
			result.TotalRequests += daily.TotalRequests
			result.Allowed += daily.Allowed
			result.Blocked += daily.Blocked
			result.BotBlocked += daily.BotBlocked
			result.RateLimited += daily.RateLimited

			for code, count := range daily.StatusCodes {
				result.StatusCodes[code] += count
			}

			if daily.TotalRequests > result.PeakRequests {
				result.PeakRequests = daily.TotalRequests
				result.PeakTime = daily.Date
			}
		}
	}

	hours := duration.Hours()
	if hours > 0 {
		result.AvgRequestsPerHour = float64(result.TotalRequests) / hours
	}

	for _, ep := range apiMetrics.EndpointStats {
		var epTotal int64
		var epBlocked int64
		for _, daily := range ep.DailyStats {
			dailyTime, _ := time.Parse("2006-01-02", daily.Date)
			if !dailyTime.Before(from) && !dailyTime.After(to) {
				epTotal += daily.TotalRequests
				epBlocked += daily.Blocked
			}
		}

		result.Endpoints = append(result.Endpoints, EndpointSummary{
			EndpointID: ep.EndpointID,
			Path:       ep.Path,
			Method:     ep.Method,
			Requests:   epTotal,
			Blocked:    epBlocked,
		})
	}

	sort.Slice(result.Endpoints, func(i, j int) bool {
		return result.Endpoints[i].Requests > result.Endpoints[j].Requests
	})

	return result, nil
}

func (mc *MetricsCollector) getHourlyBreakdown(apiMetrics *APIMetrics, from, to time.Time) []HourlyPoint {
	points := make([]HourlyPoint, 0)

	for t := from.Truncate(time.Hour); !t.After(to); t = t.Add(time.Hour) {
		hourKey := t.Format("2006-01-02-15")
		if bucket, exists := apiMetrics.HourlyStats[hourKey]; exists {
			points = append(points, HourlyPoint{
				Hour:     hourKey,
				Requests: bucket.TotalRequests,
				Allowed:  bucket.Allowed,
				Blocked:  bucket.Blocked,
			})
		} else {
			points = append(points, HourlyPoint{
				Hour:     hourKey,
				Requests: 0,
				Allowed:  0,
				Blocked:  0,
			})
		}
	}

	return points
}

func (mc *MetricsCollector) getDailyBreakdown(apiMetrics *APIMetrics, from, to time.Time) []DailyPoint {
	points := make([]DailyPoint, 0)

	for t := from.Truncate(24 * time.Hour); !t.After(to); t = t.Add(24 * time.Hour) {
		dateKey := t.Format("2006-01-02")
		if bucket, exists := apiMetrics.DailyStats[dateKey]; exists {
			points = append(points, DailyPoint{
				Date:     dateKey,
				Requests: bucket.TotalRequests,
				Allowed:  bucket.Allowed,
				Blocked:  bucket.Blocked,
			})
		} else {
			points = append(points, DailyPoint{
				Date:     dateKey,
				Requests: 0,
				Allowed:  0,
				Blocked:  0,
			})
		}
	}

	return points
}

// GetAllAPIs returns list of all APIs with metrics for the HTTP API response
func (mc *MetricsCollector) GetAllAPIs() []MetricsAPIInfo {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	apis := make([]MetricsAPIInfo, 0, len(mc.apiMetrics))
	for apiID, metrics := range mc.apiMetrics {
		apis = append(apis, MetricsAPIInfo{
			APIID:         apiID,
			APIPath:       metrics.APIPath,
			UpstreamURL:   metrics.UpstreamURL,
			LastUpdated:   metrics.LastUpdated,
			TotalRequests: metrics.TotalStats.TotalRequests,
			TodayRequests: mc.getTodayRequests(metrics),
		})
	}

	sort.Slice(apis, func(i, j int) bool {
		return apis[i].TotalRequests > apis[j].TotalRequests
	})

	return apis
}

// MetricsAPIInfo is for the HTTP API response (different from RegistryAPIInfo)
type MetricsAPIInfo struct {
	APIID         string    `json:"api_id"`
	APIPath       string    `json:"api_path"`
	UpstreamURL   string    `json:"upstream_url"`
	LastUpdated   time.Time `json:"last_updated"`
	TotalRequests int64     `json:"total_requests"`
	TodayRequests int64     `json:"today_requests"`
}

func (mc *MetricsCollector) getTodayRequests(metrics *APIMetrics) int64 {
	today := time.Now().Format("2006-01-02")
	if daily, exists := metrics.DailyStats[today]; exists {
		return daily.TotalRequests
	}
	return 0
}

func (mc *MetricsCollector) Save(apiID string) error {
	mc.mu.RLock()
	metrics, exists := mc.apiMetrics[apiID]
	mc.mu.RUnlock()

	if !exists {
		return fmt.Errorf("API %s not found", apiID)
	}

	metricsCopy := mc.cleanForSave(metrics)

	filename := filepath.Join(mc.metricsDir, fmt.Sprintf("%s_metrics.json", apiID))
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(metricsCopy)
}

func (mc *MetricsCollector) cleanForSave(m *APIMetrics) *APIMetrics {
	cleaned := &APIMetrics{
		APIID:         m.APIID,
		APIPath:       m.APIPath,
		UpstreamURL:   m.UpstreamURL,
		LastUpdated:   m.LastUpdated,
		HourlyStats:   make(map[string]HourlyBucket),
		DailyStats:    m.DailyStats,
		TotalStats:    m.TotalStats,
		EndpointStats: m.EndpointStats,
	}

	for k, v := range m.HourlyStats {
		v.IPs = nil
		cleaned.HourlyStats[k] = v
	}

	return cleaned
}

func (mc *MetricsCollector) LoadAll() error {
	files, err := os.ReadDir(mc.metricsDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		apiID := file.Name()
		if len(apiID) > 13 && strings.HasSuffix(apiID, "_metrics.json") {
			apiID = apiID[:len(apiID)-13]
		} else if strings.HasSuffix(apiID, ".json") {
			apiID = apiID[:len(apiID)-5]
		}

		if err := mc.Load(apiID); err != nil {
			continue
		}
	}

	return nil
}

func (mc *MetricsCollector) Load(apiID string) error {
	filename := filepath.Join(mc.metricsDir, fmt.Sprintf("%s_metrics.json", apiID))

	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var metrics APIMetrics
	if err := json.Unmarshal(data, &metrics); err != nil {
		return err
	}

	if metrics.HourlyStats == nil {
		metrics.HourlyStats = make(map[string]HourlyBucket)
	}
	if metrics.DailyStats == nil {
		metrics.DailyStats = make(map[string]DailyBucket)
	}
	if metrics.EndpointStats == nil {
		metrics.EndpointStats = make(map[string]EndpointMetrics)
	}

	mc.mu.Lock()
	mc.apiMetrics[apiID] = &metrics
	mc.mu.Unlock()

	return nil
}

func (mc *MetricsCollector) backgroundFlush() {
	ticker := time.NewTicker(mc.flushInterval)
	defer ticker.Stop()

	for range ticker.C {
		mc.FlushAll()
		mc.cleanupOldData()
	}
}

func (mc *MetricsCollector) FlushAll() {
	mc.mu.RLock()
	apiIDs := make([]string, 0, len(mc.apiMetrics))
	for apiID := range mc.apiMetrics {
		apiIDs = append(apiIDs, apiID)
	}
	mc.mu.RUnlock()

	for _, apiID := range apiIDs {
		if err := mc.Save(apiID); err != nil {
			// Log error but continue
		}
	}
}

func (mc *MetricsCollector) cleanupOldData() {
	cutoff := time.Now().AddDate(0, 0, -mc.retentionDays)
	cutoffKey := cutoff.Format("2006-01-02-15")

	mc.mu.Lock()
	defer mc.mu.Unlock()

	for _, metrics := range mc.apiMetrics {
		for hourKey := range metrics.HourlyStats {
			if hourKey < cutoffKey {
				delete(metrics.HourlyStats, hourKey)
			}
		}
	}
}

func (mc *MetricsCollector) GetMetricsFilePath(apiID string) string {
	return filepath.Join(mc.metricsDir, fmt.Sprintf("%s_metrics.json", apiID))
}

func (mc *MetricsCollector) Close() error {
	mc.FlushAll()
	return nil
}
