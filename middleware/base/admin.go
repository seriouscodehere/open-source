// base/admin.go
package base

import (
	"time"
)

// APIRegistration represents a registered API service
type APIRegistration struct {
	ID            string            `json:"id" yaml:"id"`
	ServiceID     string            `json:"service_id" yaml:"service_id"`
	Name          string            `json:"name" yaml:"name"`
	Description   string            `json:"description" yaml:"description"`
	UpstreamURL   string            `json:"upstream_url" yaml:"upstream_url"` // Reverse proxy target
	BasePath      string            `json:"base_path" yaml:"base_path"`       // e.g., /api/payments
	Endpoints     []Endpoint        `json:"endpoints" yaml:"endpoints"`
	DefaultLimits RateLimits        `json:"default_limits" yaml:"default_limits"`
	CreatedAt     time.Time         `json:"created_at" yaml:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at" yaml:"updated_at"`
	CreatedBy     string            `json:"created_by" yaml:"created_by"`
	Status        string            `json:"status" yaml:"status"` // active, inactive, deprecated
	Tags          []string          `json:"tags" yaml:"tags"`
	Metadata      map[string]string `json:"metadata" yaml:"metadata"`
}

// Endpoint represents a specific endpoint in an API
type Endpoint struct {
	ID       string     `json:"id" yaml:"id"`
	Path     string     `json:"path" yaml:"path"`       // e.g., /api/payments/charge
	Method   string     `json:"method" yaml:"method"`   // GET, POST, *, etc.
	Pattern  string     `json:"pattern" yaml:"pattern"` // glob pattern for matching
	Limits   RateLimits `json:"limits" yaml:"limits"`
	Priority int        `json:"priority" yaml:"priority"` // Higher = checked first
	Enabled  bool       `json:"enabled" yaml:"enabled"`
}

// RateLimits defines rate limiting parameters
type RateLimits struct {
	RequestsPerSecond float64       `json:"requests_per_second" yaml:"requests_per_second"`
	BurstSize         int           `json:"burst_size" yaml:"burst_size"`
	WindowSize        time.Duration `json:"window_size" yaml:"window_size"`
	BlockDuration     time.Duration `json:"block_duration" yaml:"block_duration"`
	UseSlidingWindow  bool          `json:"use_sliding_window" yaml:"use_sliding_window"`
}

// APIUpdateRequest represents an update to an API
type APIUpdateRequest struct {
	Name          string            `json:"name,omitempty"`
	Description   string            `json:"description,omitempty"`
	UpstreamURL   string            `json:"upstream_url,omitempty"` // Added: reverse proxy target
	BasePath      string            `json:"base_path,omitempty"`    // Added: base path
	DefaultLimits *RateLimits       `json:"default_limits,omitempty"`
	Status        string            `json:"status,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// EndpointUpdateRequest represents an update to an endpoint
type EndpointUpdateRequest struct {
	Method   string      `json:"method,omitempty"`
	Pattern  string      `json:"pattern,omitempty"`
	Path     string      `json:"path,omitempty"`
	Limits   *RateLimits `json:"limits,omitempty"`
	Priority int         `json:"priority,omitempty"`
	Enabled  *bool       `json:"enabled,omitempty"`
}

// APISummary for list views
type APISummary struct {
	ID            string    `json:"id"`
	ServiceID     string    `json:"service_id"`
	Name          string    `json:"name"`
	BasePath      string    `json:"base_path"`
	UpstreamURL   string    `json:"upstream_url,omitempty"` // Added
	EndpointCount int       `json:"endpoint_count"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// BulkImportRequest for importing multiple APIs
type BulkImportRequest struct {
	APIs              []APIRegistration `json:"apis"`
	OverwriteExisting bool              `json:"overwrite_existing"`
}

// BulkImportResponse results of import
type BulkImportResponse struct {
	Imported int      `json:"imported"`
	Updated  int      `json:"updated"`
	Failed   int      `json:"failed"`
	Errors   []string `json:"errors,omitempty"`
}

// RuleChangeEvent for real-time updates
type RuleChangeEvent struct {
	Type       string      `json:"type"` // created, updated, deleted, reload
	APIID      string      `json:"api_id,omitempty"`
	EndpointID string      `json:"endpoint_id,omitempty"`
	Timestamp  time.Time   `json:"timestamp"`
	Data       interface{} `json:"data,omitempty"`
}
