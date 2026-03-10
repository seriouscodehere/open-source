// storage/file/registry.go
package file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"middleware/base"
)

// FileRegistry implements RuleRegistry using JSON file storage
type FileRegistry struct {
	mu       sync.RWMutex
	filePath string
	apis     map[string]*base.APIRegistration
	changes  chan base.RuleChangeEvent
	closed   bool
}

// NewFileRegistry creates a new file-based registry
func NewFileRegistry(filePath string) *FileRegistry {
	return &FileRegistry{
		filePath: filePath,
		apis:     make(map[string]*base.APIRegistration),
		changes:  make(chan base.RuleChangeEvent, 100),
	}
}

// Load reads APIs from JSON file
func (f *FileRegistry) Load() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	fmt.Printf("=== FileRegistry.Load() called ===\n")

	// Ensure directory exists
	dir := filepath.Dir(f.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(f.filePath); os.IsNotExist(err) {
		fmt.Printf("=== FileRegistry: No existing file, starting fresh ===\n")
		return nil // No file yet, start empty
	}

	// Read file
	fmt.Printf("=== FileRegistry: Reading file: %s ===\n", f.filePath)
	data, err := os.ReadFile(f.filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Parse JSON
	fmt.Printf("=== FileRegistry: Parsing JSON (%d bytes) ===\n", len(data))
	var apis []base.APIRegistration
	if err := json.Unmarshal(data, &apis); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Load into map
	for i := range apis {
		api := &apis[i]
		f.apis[api.ID] = api
	}

	fmt.Printf("=== FileRegistry: Loaded %d APIs ===\n", len(f.apis))
	return nil
}

// saveLocked writes APIs to JSON file - MUST be called with lock held
func (f *FileRegistry) saveLocked() error {
	fmt.Printf("=== saveLocked called ===\n")

	// Convert map to slice for JSON marshaling
	apis := make([]base.APIRegistration, 0, len(f.apis))
	for _, api := range f.apis {
		apis = append(apis, *api)
	}

	fmt.Printf("=== saveLocked: Prepared %d APIs for saving ===\n", len(apis))

	// Marshal with indentation for readability
	fmt.Printf("=== saveLocked: Marshaling to JSON ===\n")
	data, err := json.MarshalIndent(apis, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(f.filePath)
	fmt.Printf("=== saveLocked: Ensuring directory exists: %s ===\n", dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write to temporary file first, then rename (atomic operation)
	tmpFile := f.filePath + ".tmp"
	fmt.Printf("=== saveLocked: Writing to temp file: %s ===\n", tmpFile)
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	fmt.Printf("=== saveLocked: Renaming %s to %s ===\n", tmpFile, f.filePath)
	if err := os.Rename(tmpFile, f.filePath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename file: %w", err)
	}

	fmt.Printf("=== saveLocked: Success ===\n")
	return nil
}

// RegisterAPI registers a new API with upstream URL support
func (f *FileRegistry) RegisterAPI(ctx context.Context, api *base.APIRegistration) error {
	fmt.Printf("=== RegisterAPI called for: %s ===\n", api.ID)

	f.mu.Lock()
	defer f.mu.Unlock()

	fmt.Printf("=== Lock acquired, checking for existing API ===\n")

	if _, exists := f.apis[api.ID]; exists {
		return fmt.Errorf("API with ID %s already exists", api.ID)
	}

	// Validate required fields for reverse proxy mode
	if api.UpstreamURL == "" {
		return fmt.Errorf("upstream_url is required for reverse proxy mode")
	}

	// Set defaults
	now := time.Now()
	if api.CreatedAt.IsZero() {
		api.CreatedAt = now
	}
	api.UpdatedAt = now
	if api.Status == "" {
		api.Status = "active"
	}

	// Ensure base path starts with /
	if api.BasePath != "" && !strings.HasPrefix(api.BasePath, "/") {
		api.BasePath = "/" + api.BasePath
	}

	// Store in memory first
	fmt.Printf("=== Storing API in memory ===\n")
	f.apis[api.ID] = api

	// Save to file
	fmt.Printf("=== Calling saveLocked ===\n")
	if err := f.saveLocked(); err != nil {
		// Rollback on error
		fmt.Printf("=== saveLocked failed: %v ===\n", err)
		delete(f.apis, api.ID)
		return err
	}

	// Notify
	fmt.Printf("=== Sending change notification ===\n")
	select {
	case f.changes <- base.RuleChangeEvent{
		Type:      "created",
		APIID:     api.ID,
		Timestamp: now,
		Data:      api,
	}:
	default:
	}

	fmt.Printf("=== RegisterAPI completed successfully ===\n")
	return nil
}

// GetAPI retrieves an API by ID
func (f *FileRegistry) GetAPI(ctx context.Context, id string) (*base.APIRegistration, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	api, exists := f.apis[id]
	if !exists {
		return nil, fmt.Errorf("API not found: %s", id)
	}

	// Return copy
	result := *api
	return &result, nil
}

// UpdateAPI updates an existing API
func (f *FileRegistry) UpdateAPI(ctx context.Context, id string, req *base.APIUpdateRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	api, exists := f.apis[id]
	if !exists {
		return fmt.Errorf("API not found: %s", id)
	}

	// Apply updates
	if req.Name != "" {
		api.Name = req.Name
	}
	if req.Description != "" {
		api.Description = req.Description
	}
	if req.UpstreamURL != "" {
		api.UpstreamURL = req.UpstreamURL
	}
	if req.BasePath != "" {
		if !strings.HasPrefix(req.BasePath, "/") {
			req.BasePath = "/" + req.BasePath
		}
		api.BasePath = req.BasePath
	}
	if req.DefaultLimits != nil {
		api.DefaultLimits = *req.DefaultLimits
	}
	if req.Status != "" {
		api.Status = req.Status
	}
	if req.Tags != nil {
		api.Tags = req.Tags
	}
	if req.Metadata != nil {
		api.Metadata = req.Metadata
	}
	api.UpdatedAt = time.Now()

	// Save to file
	if err := f.saveLocked(); err != nil {
		return err
	}

	select {
	case f.changes <- base.RuleChangeEvent{
		Type:      "updated",
		APIID:     id,
		Timestamp: time.Now(),
		Data:      api,
	}:
	default:
	}

	return nil
}

// DeleteAPI removes an API
func (f *FileRegistry) DeleteAPI(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.apis[id]; !exists {
		return fmt.Errorf("API not found: %s", id)
	}

	delete(f.apis, id)

	// Save to file
	if err := f.saveLocked(); err != nil {
		return err
	}

	select {
	case f.changes <- base.RuleChangeEvent{
		Type:      "deleted",
		APIID:     id,
		Timestamp: time.Now(),
	}:
	default:
	}

	return nil
}

// ListAPIs lists all APIs with filtering
func (f *FileRegistry) ListAPIs(ctx context.Context, filter base.APIFilter) ([]base.APISummary, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var summaries []base.APISummary
	for id, api := range f.apis {
		// Apply filters
		if filter.ServiceID != "" && api.ServiceID != filter.ServiceID {
			continue
		}
		if filter.Status != "" && api.Status != filter.Status {
			continue
		}
		if filter.Search != "" && !strings.Contains(strings.ToLower(api.Name), strings.ToLower(filter.Search)) {
			continue
		}
		if len(filter.Tags) > 0 && !hasAnyTag(api.Tags, filter.Tags) {
			continue
		}

		summaries = append(summaries, base.APISummary{
			ID:            id,
			ServiceID:     api.ServiceID,
			Name:          api.Name,
			BasePath:      api.BasePath,
			UpstreamURL:   api.UpstreamURL,
			EndpointCount: len(api.Endpoints),
			Status:        api.Status,
			CreatedAt:     api.CreatedAt,
		})
	}

	// Apply pagination
	if filter.Offset > 0 && filter.Offset < len(summaries) {
		summaries = summaries[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(summaries) {
		summaries = summaries[:filter.Limit]
	}

	return summaries, nil
}

// AddEndpoint adds an endpoint to an API
func (f *FileRegistry) AddEndpoint(ctx context.Context, apiID string, endpoint *base.Endpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	api, exists := f.apis[apiID]
	if !exists {
		return fmt.Errorf("API not found: %s", apiID)
	}

	// Check for duplicate
	for _, ep := range api.Endpoints {
		if ep.ID == endpoint.ID {
			return fmt.Errorf("endpoint %s already exists", endpoint.ID)
		}
	}

	// Set defaults
	if endpoint.Method == "" {
		endpoint.Method = "*"
	}
	if endpoint.Priority == 0 {
		endpoint.Priority = 100
	}
	endpoint.Enabled = true

	api.Endpoints = append(api.Endpoints, *endpoint)
	api.UpdatedAt = time.Now()

	// Save to file
	if err := f.saveLocked(); err != nil {
		// Rollback
		api.Endpoints = api.Endpoints[:len(api.Endpoints)-1]
		return err
	}

	select {
	case f.changes <- base.RuleChangeEvent{
		Type:       "endpoint_added",
		APIID:      apiID,
		EndpointID: endpoint.ID,
		Timestamp:  time.Now(),
		Data:       endpoint,
	}:
	default:
	}

	return nil
}

// UpdateEndpoint updates an endpoint
func (f *FileRegistry) UpdateEndpoint(ctx context.Context, apiID, endpointID string, req *base.EndpointUpdateRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	api, exists := f.apis[apiID]
	if !exists {
		return fmt.Errorf("API not found: %s", apiID)
	}

	// Find endpoint
	found := false
	for i := range api.Endpoints {
		if api.Endpoints[i].ID == endpointID {
			ep := &api.Endpoints[i]

			if req.Method != "" {
				ep.Method = req.Method
			}
			if req.Pattern != "" {
				ep.Pattern = req.Pattern
			}
			if req.Path != "" {
				ep.Path = req.Path
			}
			if req.Limits != nil {
				ep.Limits = *req.Limits
			}

			if req.Priority > 0 {
				ep.Priority = req.Priority
			}
			if req.Enabled != nil {
				ep.Enabled = *req.Enabled
			}

			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("endpoint not found: %s", endpointID)
	}

	api.UpdatedAt = time.Now()

	// Save to file
	if err := f.saveLocked(); err != nil {
		return err
	}

	select {
	case f.changes <- base.RuleChangeEvent{
		Type:       "endpoint_updated",
		APIID:      apiID,
		EndpointID: endpointID,
		Timestamp:  time.Now(),
	}:
	default:
	}

	return nil
}

// RemoveEndpoint removes an endpoint
func (f *FileRegistry) RemoveEndpoint(ctx context.Context, apiID, endpointID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	api, exists := f.apis[apiID]
	if !exists {
		return fmt.Errorf("API not found: %s", apiID)
	}

	// Find and remove
	found := false
	newEndpoints := make([]base.Endpoint, 0, len(api.Endpoints))
	for _, ep := range api.Endpoints {
		if ep.ID == endpointID {
			found = true
			continue
		}
		newEndpoints = append(newEndpoints, ep)
	}

	if !found {
		return fmt.Errorf("endpoint not found: %s", endpointID)
	}

	api.Endpoints = newEndpoints
	api.UpdatedAt = time.Now()

	// Save to file
	if err := f.saveLocked(); err != nil {
		return err
	}

	select {
	case f.changes <- base.RuleChangeEvent{
		Type:       "endpoint_removed",
		APIID:      apiID,
		EndpointID: endpointID,
		Timestamp:  time.Now(),
	}:
	default:
	}

	return nil
}

// GetEndpoint retrieves a specific endpoint
func (f *FileRegistry) GetEndpoint(ctx context.Context, apiID, endpointID string) (*base.Endpoint, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	api, exists := f.apis[apiID]
	if !exists {
		return nil, fmt.Errorf("API not found: %s", apiID)
	}

	for _, ep := range api.Endpoints {
		if ep.ID == endpointID {
			result := ep
			return &result, nil
		}
	}

	return nil, fmt.Errorf("endpoint not found: %s", endpointID)
}

// BulkImport imports multiple APIs
func (f *FileRegistry) BulkImport(ctx context.Context, req *base.BulkImportRequest) (*base.BulkImportResponse, error) {
	resp := &base.BulkImportResponse{}

	for _, api := range req.APIs {
		_, exists := f.apis[api.ID]

		if exists && !req.OverwriteExisting {
			resp.Failed++
			resp.Errors = append(resp.Errors, fmt.Sprintf("API %s already exists", api.ID))
			continue
		}

		if exists {
			// Update
			if err := f.UpdateAPI(ctx, api.ID, &base.APIUpdateRequest{
				Name:          api.Name,
				Description:   api.Description,
				UpstreamURL:   api.UpstreamURL,
				BasePath:      api.BasePath,
				DefaultLimits: &api.DefaultLimits,
				Status:        api.Status,
				Tags:          api.Tags,
				Metadata:      api.Metadata,
			}); err != nil {
				resp.Failed++
				resp.Errors = append(resp.Errors, fmt.Sprintf("Failed to update %s: %v", api.ID, err))
				continue
			}
			resp.Updated++
		} else {
			// Create new
			if err := f.RegisterAPI(ctx, &api); err != nil {
				resp.Failed++
				resp.Errors = append(resp.Errors, fmt.Sprintf("Failed to register %s: %v", api.ID, err))
				continue
			}
			resp.Imported++
		}
	}

	return resp, nil
}

// ExportAll exports all APIs
func (f *FileRegistry) ExportAll(ctx context.Context) ([]base.APIRegistration, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	apis := make([]base.APIRegistration, 0, len(f.apis))
	for _, api := range f.apis {
		apis = append(apis, *api)
	}

	return apis, nil
}

// SubscribeChanges subscribes to real-time rule changes
func (f *FileRegistry) SubscribeChanges(ctx context.Context) (<-chan base.RuleChangeEvent, error) {
	ch := make(chan base.RuleChangeEvent)

	go func() {
		defer close(ch)
		for {
			select {
			case event, ok := <-f.changes:
				if !ok {
					return
				}
				select {
				case ch <- event:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// PublishChange publishes a change event
func (f *FileRegistry) PublishChange(ctx context.Context, event base.RuleChangeEvent) error {
	select {
	case f.changes <- event:
	default:
	}
	return nil
}

// ReloadRules reloads all rules from file
func (f *FileRegistry) ReloadRules(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Clear current state
	f.apis = make(map[string]*base.APIRegistration)

	// Reload from file
	return f.Load()
}

// GetCompiledRule gets the compiled rule for a path with proper pattern matching
func (f *FileRegistry) GetCompiledRule(ctx context.Context, path string) (*base.CompiledRule, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var matches []base.CompiledRule

	for _, api := range f.apis {
		if api.Status != "active" {
			continue
		}

		// Check if path matches API base path first
		if !strings.HasPrefix(path, api.BasePath) {
			continue
		}

		// Get the relative path from base path
		relativePath := strings.TrimPrefix(path, api.BasePath)
		// Ensure relative path starts with /
		if !strings.HasPrefix(relativePath, "/") {
			relativePath = "/" + relativePath
		}

		for _, ep := range api.Endpoints {
			if !ep.Enabled {
				continue
			}

			// Match against endpoint pattern or path
			if matchesEndpoint(relativePath, ep) {
				rule := base.CompiledRule{
					APIID:             api.ID,
					EndpointID:        ep.ID,
					ServiceID:         api.ServiceID,
					Path:              path,
					Method:            ep.Method,
					RequestsPerSecond: ep.Limits.RequestsPerSecond,
					BurstSize:         ep.Limits.BurstSize,
					WindowSize:        ep.Limits.WindowSize,
					BlockDuration:     ep.Limits.BlockDuration,
					UseSlidingWindow:  ep.Limits.UseSlidingWindow,
					Priority:          ep.Priority,
					Enabled:           ep.Enabled,
					UpstreamURL:       api.UpstreamURL,
				}

				// Apply default limits from API if not set on endpoint
				if rule.RequestsPerSecond == 0 {
					rule.RequestsPerSecond = api.DefaultLimits.RequestsPerSecond
				}
				if rule.BurstSize == 0 {
					rule.BurstSize = api.DefaultLimits.BurstSize
				}
				if rule.WindowSize == 0 {
					rule.WindowSize = api.DefaultLimits.WindowSize
				}
				if rule.BlockDuration == 0 {
					rule.BlockDuration = api.DefaultLimits.BlockDuration
				}

				matches = append(matches, rule)
			}
		}
	}

	if len(matches) == 0 {
		return &base.CompiledRule{
			Path:       path,
			IsExcluded: true,
		}, nil
	}

	// Sort by priority (highest first), then by specificity (longer pattern first)
	highest := matches[0]
	for _, m := range matches[1:] {
		if m.Priority > highest.Priority {
			highest = m
		} else if m.Priority == highest.Priority {
			// If same priority, prefer more specific (longer) pattern
			if len(m.Path) > len(highest.Path) {
				highest = m
			}
		}
	}

	return &highest, nil
}

// Close closes the registry
func (f *FileRegistry) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.closed {
		close(f.changes)
		f.closed = true
	}
	return nil
}

// Helper functions
func hasAnyTag(apiTags, filterTags []string) bool {
	for _, ft := range filterTags {
		for _, at := range apiTags {
			if strings.EqualFold(ft, at) {
				return true
			}
		}
	}
	return false
}

// matchesEndpoint checks if a relative path matches an endpoint definition
func matchesEndpoint(relativePath string, ep base.Endpoint) bool {
	// If pattern is set, use it (supports wildcards)
	if ep.Pattern != "" {
		return matchesPattern(relativePath, ep.Pattern)
	}

	// Otherwise match against the Path field
	if ep.Path != "" {
		// Exact match
		if ep.Path == relativePath {
			return true
		}
		// Pattern matching with wildcards
		if matchesPattern(relativePath, ep.Path) {
			return true
		}
	}

	return false
}

// matchesPattern checks if path matches pattern (supports * wildcards)
func matchesPattern(path, pattern string) bool {
	if pattern == "" {
		return false
	}

	// Convert pattern to regex: * -> .*, ? -> .
	regexPattern := "^" + regexp.QuoteMeta(pattern) + "$"
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, `.*`)
	regexPattern = strings.ReplaceAll(regexPattern, `\?`, `.`)

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		// Fallback to simple prefix/suffix matching
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			return strings.HasPrefix(path, prefix)
		}
		return path == pattern
	}

	return re.MatchString(path)
}

func compilePattern(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	regexPattern := "^" + regexp.QuoteMeta(pattern) + "$"
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, `.*`)
	regexPattern = strings.ReplaceAll(regexPattern, `\?`, `.`)
	return regexp.Compile(regexPattern)
}
