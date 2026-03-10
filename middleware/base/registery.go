// base/registry.go
package base

import (
	"context"
	"time"
)

// RuleRegistry defines the interface for rule storage
type RuleRegistry interface {
	// API Management
	RegisterAPI(ctx context.Context, api *APIRegistration) error
	GetAPI(ctx context.Context, id string) (*APIRegistration, error)
	UpdateAPI(ctx context.Context, id string, req *APIUpdateRequest) error
	DeleteAPI(ctx context.Context, id string) error
	ListAPIs(ctx context.Context, filter APIFilter) ([]APISummary, error)

	// Endpoint Management
	AddEndpoint(ctx context.Context, apiID string, endpoint *Endpoint) error
	UpdateEndpoint(ctx context.Context, apiID, endpointID string, req *EndpointUpdateRequest) error
	RemoveEndpoint(ctx context.Context, apiID, endpointID string) error
	GetEndpoint(ctx context.Context, apiID, endpointID string) (*Endpoint, error)

	// Bulk Operations
	BulkImport(ctx context.Context, req *BulkImportRequest) (*BulkImportResponse, error)
	ExportAll(ctx context.Context) ([]APIRegistration, error)

	// Real-time Updates
	SubscribeChanges(ctx context.Context) (<-chan RuleChangeEvent, error)
	PublishChange(ctx context.Context, event RuleChangeEvent) error

	// Reloading
	ReloadRules(ctx context.Context) error
	GetCompiledRule(ctx context.Context, path string) (*CompiledRule, error)
}

// APIFilter for listing APIs
type APIFilter struct {
	ServiceID string
	Status    string
	Tags      []string
	Search    string
	Limit     int
	Offset    int
}

// CompiledRule for runtime matching
type CompiledRule struct {
	APIID             string
	EndpointID        string
	ServiceID         string
	Path              string
	Method            string
	RequestsPerSecond float64
	BurstSize         int
	WindowSize        time.Duration
	BlockDuration     time.Duration
	UseSlidingWindow  bool
	Priority          int
	Enabled           bool
	IsExcluded        bool
	UpstreamURL       string // Added: for reverse proxy
}
