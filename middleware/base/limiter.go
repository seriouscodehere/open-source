// base/limiter.go
package base

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// TrafficStats holds traffic statistics - exported for external access
type TrafficStats struct {
	Mu           sync.RWMutex
	RequestCount int64
	WindowStart  time.Time
}

type distributedAttackDetector struct {
	mu        sync.RWMutex
	requests  map[int64]int64
	window    time.Duration
	threshold float64
}

// NewDistributedAttackDetector creates a new distributed attack detector
func NewDistributedAttackDetector(window time.Duration, threshold float64) *distributedAttackDetector {
	return &distributedAttackDetector{
		requests:  make(map[int64]int64),
		window:    window,
		threshold: threshold,
	}
}

func (d *distributedAttackDetector) Record() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	bucket := now.Truncate(time.Second).Unix()

	d.requests[bucket]++

	cutoff := now.Add(-d.window).Unix()
	for ts := range d.requests {
		if ts < cutoff {
			delete(d.requests, ts)
		}
	}

	var total int64
	for ts, count := range d.requests {
		if ts >= cutoff {
			total += count
		}
	}

	return float64(total) > d.threshold
}

// Limiter is the main rate limiter struct
type Limiter struct {
	Config                   Config
	Extractor                IPExtractorInterface
	Backend                  Backend
	SF                       singleflight.Group
	Ctx                      context.Context
	Cancel                   context.CancelFunc
	WG                       sync.WaitGroup
	Allowed                  int64
	Blocked                  int64
	BotBlocked               int64
	Registry                 RuleRegistry
	TrafficStats             *TrafficStats
	InFlight                 int64
	DistributedAttackCounter *distributedAttackDetector
}

// NewLimiter creates a new Limiter instance
func NewLimiter(config Config, extractor IPExtractorInterface, backend Backend) *Limiter {
	ctx, cancel := context.WithCancel(context.Background())
	return &Limiter{
		Config:                   config,
		Extractor:                extractor,
		Backend:                  backend,
		Ctx:                      ctx,
		Cancel:                   cancel,
		TrafficStats:             &TrafficStats{WindowStart: time.Now()},
		DistributedAttackCounter: NewDistributedAttackDetector(config.DistributedAttackWindow, config.DistributedAttackThreshold),
	}
}
