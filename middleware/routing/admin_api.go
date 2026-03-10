package routing

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/seriouscodehere/open-source/middleware/base"
	"github.com/seriouscodehere/open-source/middleware/config"

	"github.com/go-chi/chi/v5"
)

// AdminAPIHandler handles admin API management endpoints
type AdminAPIHandler struct {
	Registry         base.RuleRegistry
	Limiter          *base.Limiter
	ServiceTemplates map[string]config.ServiceTemplate
	EventStore       *base.EventStore
}

// NewAdminAPIHandler creates a new admin API handler
func NewAdminAPIHandler(registry base.RuleRegistry, limiter *base.Limiter, templates map[string]config.ServiceTemplate, eventStore *base.EventStore) *AdminAPIHandler {
	if templates == nil {
		templates = make(map[string]config.ServiceTemplate)
	}
	return &AdminAPIHandler{
		Registry:         registry,
		Limiter:          limiter,
		ServiceTemplates: templates,
		EventStore:       eventStore,
	}
}

// Routes returns the router with all admin API routes
func (h *AdminAPIHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// API Management
	r.Get("/", h.ListAPIs)
	r.Post("/", h.RegisterAPI)
	r.Get("/{apiID}", h.GetAPI)
	r.Put("/{apiID}", h.UpdateAPI)
	r.Delete("/{apiID}", h.DeleteAPI)

	// Endpoint Management
	r.Get("/{apiID}/endpoints", h.ListEndpoints)
	r.Post("/{apiID}/endpoints", h.AddEndpoint)
	r.Get("/{apiID}/endpoints/{endpointID}", h.GetEndpoint)
	r.Put("/{apiID}/endpoints/{endpointID}", h.UpdateEndpoint)
	r.Delete("/{apiID}/endpoints/{endpointID}", h.RemoveEndpoint)

	// Bulk Operations
	r.Post("/import", h.BulkImport)
	r.Get("/export", h.ExportAll)

	// Template Management
	r.Get("/templates", h.ListTemplates)
	r.Post("/from-template", h.CreateFromTemplate)

	// Rule Testing
	r.Post("/test", h.TestRule)
	r.Get("/resolve", h.ResolvePath)

	// Real-time Events (with persistence)
	r.Get("/events", h.SubscribeEvents)
	r.Get("/events/history", h.GetEventHistory)

	// System
	r.Post("/reload", h.ReloadRules)

	return r
}

// RegisterAPI creates a new API registration
func (h *AdminAPIHandler) RegisterAPI(w http.ResponseWriter, r *http.Request) {
	var api base.APIRegistration
	if err := json.NewDecoder(r.Body).Decode(&api); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	if api.ID == "" || api.ServiceID == "" || api.UpstreamURL == "" {
		http.Error(w, `{"error": "id, service_id, and upstream_url are required"}`, http.StatusBadRequest)
		return
	}

	if len(api.Endpoints) == 0 {
		http.Error(w, `{"error": "at least one endpoint is required"}`, http.StatusBadRequest)
		return
	}

	if api.Status == "" {
		api.Status = "active"
	}
	api.CreatedAt = time.Now()
	api.UpdatedAt = time.Now()

	api.DefaultLimits.UseSlidingWindow = true

	if api.DefaultLimits.BlockDuration == 0 {
		api.DefaultLimits.BlockDuration = 300000000000
	}

	for i := range api.Endpoints {
		if api.Endpoints[i].Path == "" {
			http.Error(w, `{"error": "endpoint path is required"}`, http.StatusBadRequest)
			return
		}
		if api.Endpoints[i].Method == "" {
			http.Error(w, `{"error": "endpoint method is required"}`, http.StatusBadRequest)
			return
		}

		api.Endpoints[i].Method = strings.ToUpper(api.Endpoints[i].Method)
		validMethods := map[string]bool{
			"GET": true, "POST": true, "PUT": true, "PATCH": true,
			"DELETE": true, "HEAD": true, "OPTIONS": true,
		}
		if !validMethods[api.Endpoints[i].Method] {
			http.Error(w, `{"error": "invalid HTTP method: `+api.Endpoints[i].Method+`"}`, http.StatusBadRequest)
			return
		}

		if !api.Endpoints[i].Enabled {
			api.Endpoints[i].Enabled = true
		}

		api.Endpoints[i].Limits.UseSlidingWindow = true

		if api.Endpoints[i].Limits.BlockDuration == 0 {
			api.Endpoints[i].Limits.BlockDuration = api.DefaultLimits.BlockDuration
		}
	}

	if err := h.Registry.RegisterAPI(r.Context(), &api); err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusConflict)
		return
	}

	// Persist event
	apiData, _ := json.Marshal(api)
	h.EventStore.Append(&base.Event{
		Type:    "api_created",
		APIID:   api.ID,
		Actor:   extractActor(r),
		Data:    apiData,
		Message: fmt.Sprintf("API '%s' created with %d endpoints", api.Name, len(api.Endpoints)),
	})

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(api)
}

// ListAPIs lists all registered APIs
func (h *AdminAPIHandler) ListAPIs(w http.ResponseWriter, r *http.Request) {
	filter := base.APIFilter{
		ServiceID: r.URL.Query().Get("service_id"),
		Status:    r.URL.Query().Get("status"),
		Search:    r.URL.Query().Get("search"),
	}

	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &filter.Limit)
	}
	if offsetStr != "" {
		fmt.Sscanf(offsetStr, "%d", &filter.Offset)
	}

	apis, err := h.Registry.ListAPIs(r.Context(), filter)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"apis":  apis,
		"count": len(apis),
	})
}

// GetAPI gets a specific API
func (h *AdminAPIHandler) GetAPI(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")

	api, err := h.Registry.GetAPI(r.Context(), apiID)
	if err != nil {
		http.Error(w, `{"error": "API not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(api)
}

// UpdateAPI updates an API
func (h *AdminAPIHandler) UpdateAPI(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")

	var req base.APIUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.DefaultLimits != nil {
		req.DefaultLimits.UseSlidingWindow = true
	}

	if err := h.Registry.UpdateAPI(r.Context(), apiID, &req); err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	api, _ := h.Registry.GetAPI(r.Context(), apiID)

	// Persist event
	apiData, _ := json.Marshal(api)
	h.EventStore.Append(&base.Event{
		Type:    "api_updated",
		APIID:   apiID,
		Actor:   extractActor(r),
		Data:    apiData,
		Message: fmt.Sprintf("API '%s' updated", apiID),
	})

	json.NewEncoder(w).Encode(api)
}

// DeleteAPI removes an API
func (h *AdminAPIHandler) DeleteAPI(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")

	// Get API before deletion for event data
	api, _ := h.Registry.GetAPI(r.Context(), apiID)
	apiData, _ := json.Marshal(api)

	if err := h.Registry.DeleteAPI(r.Context(), apiID); err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	// Persist event
	h.EventStore.Append(&base.Event{
		Type:    "api_deleted",
		APIID:   apiID,
		Actor:   extractActor(r),
		Data:    apiData,
		Message: fmt.Sprintf("API '%s' deleted", apiID),
	})

	w.WriteHeader(http.StatusNoContent)
}

// AddEndpoint adds an endpoint to an API
func (h *AdminAPIHandler) AddEndpoint(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")

	var ep base.Endpoint
	if err := json.NewDecoder(r.Body).Decode(&ep); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	if ep.ID == "" || ep.Path == "" || ep.Method == "" {
		http.Error(w, `{"error": "id, path, and method are required"}`, http.StatusBadRequest)
		return
	}

	ep.Method = strings.ToUpper(ep.Method)
	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "PATCH": true,
		"DELETE": true, "HEAD": true, "OPTIONS": true,
	}
	if !validMethods[ep.Method] {
		http.Error(w, `{"error": "invalid HTTP method: `+ep.Method+`"}`, http.StatusBadRequest)
		return
	}

	if !ep.Enabled {
		ep.Enabled = true
	}

	ep.Limits.UseSlidingWindow = true

	if ep.Limits.BlockDuration == 0 {
		api, err := h.Registry.GetAPI(r.Context(), apiID)
		if err == nil {
			ep.Limits.BlockDuration = api.DefaultLimits.BlockDuration
		} else {
			ep.Limits.BlockDuration = 300000000000
		}
	}

	if err := h.Registry.AddEndpoint(r.Context(), apiID, &ep); err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	// Persist event
	epData, _ := json.Marshal(ep)
	h.EventStore.Append(&base.Event{
		Type:       "endpoint_added",
		APIID:      apiID,
		EndpointID: ep.ID,
		Actor:      extractActor(r),
		Data:       epData,
		Message:    fmt.Sprintf("Endpoint '%s' (%s %s) added to API '%s'", ep.ID, ep.Method, ep.Path, apiID),
	})

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ep)
}

// ListEndpoints lists all endpoints for an API
func (h *AdminAPIHandler) ListEndpoints(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")

	api, err := h.Registry.GetAPI(r.Context(), apiID)
	if err != nil {
		http.Error(w, `{"error": "API not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"endpoints": api.Endpoints,
		"count":     len(api.Endpoints),
	})
}

// GetEndpoint gets a specific endpoint
func (h *AdminAPIHandler) GetEndpoint(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")
	endpointID := chi.URLParam(r, "endpointID")

	ep, err := h.Registry.GetEndpoint(r.Context(), apiID, endpointID)
	if err != nil {
		http.Error(w, `{"error": "Endpoint not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(ep)
}

// UpdateEndpoint updates an endpoint
func (h *AdminAPIHandler) UpdateEndpoint(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")
	endpointID := chi.URLParam(r, "endpointID")

	var req base.EndpointUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Method != "" {
		req.Method = strings.ToUpper(req.Method)
		validMethods := map[string]bool{
			"GET": true, "POST": true, "PUT": true, "PATCH": true,
			"DELETE": true, "HEAD": true, "OPTIONS": true,
		}
		if !validMethods[req.Method] {
			http.Error(w, `{"error": "invalid HTTP method: `+req.Method+`"}`, http.StatusBadRequest)
			return
		}
	}

	if req.Limits != nil {
		req.Limits.UseSlidingWindow = true
	}

	if err := h.Registry.UpdateEndpoint(r.Context(), apiID, endpointID, &req); err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	ep, _ := h.Registry.GetEndpoint(r.Context(), apiID, endpointID)

	// Persist event
	epData, _ := json.Marshal(ep)
	h.EventStore.Append(&base.Event{
		Type:       "endpoint_updated",
		APIID:      apiID,
		EndpointID: endpointID,
		Actor:      extractActor(r),
		Data:       epData,
		Message:    fmt.Sprintf("Endpoint '%s' in API '%s' updated", endpointID, apiID),
	})

	json.NewEncoder(w).Encode(ep)
}

// RemoveEndpoint removes an endpoint
func (h *AdminAPIHandler) RemoveEndpoint(w http.ResponseWriter, r *http.Request) {
	apiID := chi.URLParam(r, "apiID")
	endpointID := chi.URLParam(r, "endpointID")

	// Get endpoint before deletion for event data
	ep, _ := h.Registry.GetEndpoint(r.Context(), apiID, endpointID)
	epData, _ := json.Marshal(ep)

	if err := h.Registry.RemoveEndpoint(r.Context(), apiID, endpointID); err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	// Persist event
	h.EventStore.Append(&base.Event{
		Type:       "endpoint_removed",
		APIID:      apiID,
		EndpointID: endpointID,
		Actor:      extractActor(r),
		Data:       epData,
		Message:    fmt.Sprintf("Endpoint '%s' removed from API '%s'", endpointID, apiID),
	})

	w.WriteHeader(http.StatusNoContent)
}

// ListTemplates returns available service templates
func (h *AdminAPIHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"templates": h.ServiceTemplates,
		"count":     len(h.ServiceTemplates),
	})
}

// CreateFromTemplate creates an API from a template
func (h *AdminAPIHandler) CreateFromTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIID       string          `json:"api_id"`
		ServiceID   string          `json:"service_id"`
		Name        string          `json:"name"`
		UpstreamURL string          `json:"upstream_url"`
		Template    string          `json:"template"`
		Endpoints   []base.Endpoint `json:"endpoints"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	template, exists := h.ServiceTemplates[req.Template]
	if !exists {
		http.Error(w, `{"error": "template not found"}`, http.StatusNotFound)
		return
	}

	api := &base.APIRegistration{
		ID:          req.APIID,
		ServiceID:   req.ServiceID,
		Name:        req.Name,
		UpstreamURL: req.UpstreamURL,
		Status:      "active",
		DefaultLimits: base.RateLimits{
			RequestsPerSecond: template.RequestsPerSecond,
			BurstSize:         template.BurstSize,
			WindowSize:        template.WindowSize,
			BlockDuration:     template.BlockDuration,
			UseSlidingWindow:  true,
		},
		Endpoints: req.Endpoints,
	}

	for i := range api.Endpoints {
		if api.Endpoints[i].Method != "" {
			api.Endpoints[i].Method = strings.ToUpper(api.Endpoints[i].Method)
		}

		if !api.Endpoints[i].Enabled {
			api.Endpoints[i].Enabled = true
		}
		api.Endpoints[i].Limits.UseSlidingWindow = true
	}

	if err := h.Registry.RegisterAPI(r.Context(), api); err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusConflict)
		return
	}

	// Persist event
	apiData, _ := json.Marshal(api)
	h.EventStore.Append(&base.Event{
		Type:    "api_created",
		APIID:   api.ID,
		Actor:   extractActor(r),
		Data:    apiData,
		Message: fmt.Sprintf("API '%s' created from template '%s'", api.Name, req.Template),
	})

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(api)
}

// BulkImport imports multiple APIs
func (h *AdminAPIHandler) BulkImport(w http.ResponseWriter, r *http.Request) {
	var req base.BulkImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	for i := range req.APIs {
		req.APIs[i].DefaultLimits.UseSlidingWindow = true

		for j := range req.APIs[i].Endpoints {
			if req.APIs[i].Endpoints[j].Method != "" {
				req.APIs[i].Endpoints[j].Method = strings.ToUpper(req.APIs[i].Endpoints[j].Method)
			}

			if !req.APIs[i].Endpoints[j].Enabled {
				req.APIs[i].Endpoints[j].Enabled = true
			}
			req.APIs[i].Endpoints[j].Limits.UseSlidingWindow = true
		}
	}

	resp, err := h.Registry.BulkImport(r.Context(), &req)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// Persist events for each imported API
	for _, api := range req.APIs {
		apiData, _ := json.Marshal(api)
		h.EventStore.Append(&base.Event{
			Type:    "api_created",
			APIID:   api.ID,
			Actor:   extractActor(r),
			Data:    apiData,
			Message: fmt.Sprintf("API '%s' imported via bulk import", api.ID),
		})
	}

	json.NewEncoder(w).Encode(resp)
}

// ExportAll exports all APIs
func (h *AdminAPIHandler) ExportAll(w http.ResponseWriter, r *http.Request) {
	apis, err := h.Registry.ExportAll(r.Context())
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=apis-export.json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"exported_at": time.Now(),
		"count":       len(apis),
		"apis":        apis,
	})
}

// TestRule tests a rule against a path without applying it
func (h *AdminAPIHandler) TestRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path   string          `json:"path"`
		Method string          `json:"method"`
		Rule   base.RateLimits `json:"rule"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	rule, err := h.Registry.GetCompiledRule(r.Context(), req.Path)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"path":         req.Path,
		"method":       req.Method,
		"matched_rule": rule,
		"would_apply":  !rule.IsExcluded,
		"test_limits":  req.Rule,
	})
}

// ResolvePath shows which rule would apply to a path
func (h *AdminAPIHandler) ResolvePath(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, `{"error": "path parameter required"}`, http.StatusBadRequest)
		return
	}

	rule, err := h.Registry.GetCompiledRule(r.Context(), path)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"path":          path,
		"resolved_rule": rule,
		"excluded":      rule.IsExcluded,
	})
}

// ReloadRules triggers a rule reload
func (h *AdminAPIHandler) ReloadRules(w http.ResponseWriter, r *http.Request) {
	if err := h.Registry.ReloadRules(r.Context()); err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// Persist event
	h.EventStore.Append(&base.Event{
		Type:      "rules_reloaded",
		Actor:     extractActor(r),
		Timestamp: time.Now(),
		Message:   "Rules reloaded from registry file",
	})

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "reloaded",
		"message": "Rules reloaded successfully",
	})
}

// GetEventHistory returns persisted events
func (h *AdminAPIHandler) GetEventHistory(w http.ResponseWriter, r *http.Request) {
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	apiID := r.URL.Query().Get("api_id")
	eventType := r.URL.Query().Get("type")
	limit := 100

	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	var from, to time.Time
	if fromStr != "" {
		from, _ = time.Parse(time.RFC3339, fromStr)
	} else {
		from = time.Now().Add(-24 * time.Hour)
	}
	if toStr != "" {
		to, _ = time.Parse(time.RFC3339, toStr)
	} else {
		to = time.Now()
	}

	filter := base.EventFilter{
		Type:  eventType,
		APIID: apiID,
	}

	events, err := h.EventStore.GetRange(from, to, filter)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	if len(events) > limit {
		events = events[len(events)-limit:]
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
		"count":  len(events),
		"from":   from,
		"to":     to,
	})
}

// SubscribeEvents provides Server-Sent Events with persistence
func (h *AdminAPIHandler) SubscribeEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send recent history from persistent store
	recent := h.EventStore.GetRecent(50)
	for _, ev := range recent {
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Subscribe to new changes
	events, err := h.Registry.SubscribeChanges(r.Context())
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// Send connection confirmation
	fmt.Fprintf(w, "data: %s\n\n", `{"type": "connected", "timestamp": "`+time.Now().Format(time.RFC3339)+`"}`)
	flusher.Flush()

	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}

			// Convert interface{} Data to json.RawMessage if present
			var rawData json.RawMessage
			if event.Data != nil {
				rawData, _ = json.Marshal(event.Data)
			}

			// Persist before broadcasting
			h.EventStore.Append(&base.Event{
				Type:       event.Type,
				APIID:      event.APIID,
				EndpointID: event.EndpointID,
				Actor:      extractActor(r), // Get actor from request context
				Data:       rawData,
				Message:    generateEventMessage(event), // Generate message from event type
			})

			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}

// extractActor extracts user identifier from request
func extractActor(r *http.Request) string {
	// Try Authorization header
	auth := r.Header.Get("Authorization")
	if auth != "" {
		// If it's a Bearer token, you could decode the JWT here
		// For now, return a placeholder or hash of the token
		if len(auth) > 20 {
			return "user_" + auth[7:15] // Simple hash of token prefix
		}
		return "authenticated"
	}

	// Try X-User-ID header (if your auth middleware sets it)
	if userID := r.Header.Get("X-User-ID"); userID != "" {
		return userID
	}

	return "system"
}

// generateEventMessage creates a human-readable message from RuleChangeEvent
func generateEventMessage(event base.RuleChangeEvent) string {
	switch event.Type {
	case "api_created":
		return fmt.Sprintf("API '%s' created", event.APIID)
	case "api_updated":
		return fmt.Sprintf("API '%s' updated", event.APIID)
	case "api_deleted":
		return fmt.Sprintf("API '%s' deleted", event.APIID)
	case "endpoint_added":
		return fmt.Sprintf("Endpoint '%s' added to API '%s'", event.EndpointID, event.APIID)
	case "endpoint_updated":
		return fmt.Sprintf("Endpoint '%s' updated in API '%s'", event.EndpointID, event.APIID)
	case "endpoint_removed":
		return fmt.Sprintf("Endpoint '%s' removed from API '%s'", event.EndpointID, event.APIID)
	case "reload":
		return "Rules reloaded from registry file"
	default:
		return fmt.Sprintf("Event '%s' on API '%s'", event.Type, event.APIID)
	}
}
