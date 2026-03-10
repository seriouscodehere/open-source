package base

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event represents a persisted API change notification
type Event struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"` // api_created, api_updated, api_deleted, endpoint_added, endpoint_updated, endpoint_removed, rules_reloaded
	APIID      string          `json:"api_id"`
	EndpointID string          `json:"endpoint_id,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
	Actor      string          `json:"actor,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	Message    string          `json:"message,omitempty"`
}

// EventStore persists events to JSON files with daily rotation
type EventStore struct {
	mu        sync.RWMutex
	dir       string
	events    []Event
	maxMem    int
	retention time.Duration
}

// NewEventStore creates a new event store
func NewEventStore(dir string) (*EventStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	return &EventStore{
		dir:       dir,
		maxMem:    1000,
		retention: 30 * 24 * time.Hour,
	}, nil
}

// Append saves an event to disk and memory
func (s *EventStore) Append(event *Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if event.ID == "" {
		event.ID = generateEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Add to memory cache
	s.events = append(s.events, *event)
	if len(s.events) > s.maxMem {
		s.events = s.events[len(s.events)-s.maxMem:]
	}

	// Persist to daily JSON file
	fileName := event.Timestamp.Format("2006-01-02") + ".json"
	filePath := filepath.Join(s.dir, fileName)

	var dayEvents []Event
	if data, err := os.ReadFile(filePath); err == nil {
		json.Unmarshal(data, &dayEvents)
	}

	dayEvents = append(dayEvents, *event)

	data, err := json.MarshalIndent(dayEvents, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

// GetRecent returns recent events from memory (newest first)
func (s *EventStore) GetRecent(limit int) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit > len(s.events) {
		limit = len(s.events)
	}

	result := make([]Event, limit)
	for i := 0; i < limit; i++ {
		result[i] = s.events[len(s.events)-1-i]
	}
	return result
}

// GetRange loads events from disk files within time range
func (s *EventStore) GetRange(from, to time.Time, filter EventFilter) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Event

	for d := from.Truncate(24 * time.Hour); !d.After(to); d = d.Add(24 * time.Hour) {
		filePath := filepath.Join(s.dir, d.Format("2006-01-02")+".json")

		data, err := os.ReadFile(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		var dayEvents []Event
		if err := json.Unmarshal(data, &dayEvents); err != nil {
			continue
		}

		for _, ev := range dayEvents {
			if ev.Timestamp.Before(from) || ev.Timestamp.After(to) {
				continue
			}
			if filter.Type != "" && ev.Type != filter.Type {
				continue
			}
			if filter.APIID != "" && ev.APIID != filter.APIID {
				continue
			}
			result = append(result, ev)
		}
	}

	return result, nil
}

// Cleanup removes old event files
func (s *EventStore) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.retention)

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if len(name) != 14 || filepath.Ext(name) != ".json" {
			continue
		}

		date, err := time.Parse("2006-01-02", name[:10])
		if err != nil {
			continue
		}

		if date.Before(cutoff) {
			os.Remove(filepath.Join(s.dir, name))
		}
	}

	return nil
}

// Close cleans up resources
func (s *EventStore) Close() error {
	return s.Cleanup()
}

type EventFilter struct {
	Type  string
	APIID string
}

func generateEventID() string {
	return fmt.Sprintf("evt_%d_%d", time.Now().UnixNano(), os.Getpid())
}
