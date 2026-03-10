// base/cicuit_breaker.go
package base

import (
	"sync"
	"sync/atomic"
	"time"
)

type CircuitBreaker struct {
	failures     int32
	lastFailure  int64
	threshold    int32
	timeout      time.Duration
	state        int32
	halfOpenReqs int32
	maxProbes    int32
	mu           sync.RWMutex
	metrics      Metrics
}

func NewCircuitBreaker(threshold int32, timeout time.Duration, maxProbes int32, metrics Metrics) *CircuitBreaker {
	if maxProbes <= 0 {
		maxProbes = 1
	}
	return &CircuitBreaker{
		threshold: threshold,
		timeout:   timeout,
		state:     int32(StateClosed),
		maxProbes: maxProbes,
		metrics:   metrics,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	state := CircuitBreakerState(atomic.LoadInt32(&cb.state))

	if state == StateClosed {
		return true
	}

	if state == StateOpen {
		lastFail := atomic.LoadInt64(&cb.lastFailure)
		if time.Since(time.Unix(0, lastFail)) > cb.timeout {
			if atomic.CompareAndSwapInt32(&cb.state, int32(StateOpen), int32(StateHalfOpen)) {
				atomic.StoreInt32(&cb.halfOpenReqs, 0)
				cb.metrics.CircuitBreakerStateChanged("half_open")
			}
		} else {
			return false
		}
	}

	for {
		current := atomic.LoadInt32(&cb.halfOpenReqs)
		if current >= cb.maxProbes {
			return false
		}
		if atomic.CompareAndSwapInt32(&cb.halfOpenReqs, current, current+1) {
			return true
		}
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	atomic.StoreInt32(&cb.failures, 0)
	state := CircuitBreakerState(atomic.LoadInt32(&cb.state))
	if state == StateHalfOpen {
		if atomic.CompareAndSwapInt32(&cb.state, int32(StateHalfOpen), int32(StateClosed)) {
			atomic.StoreInt32(&cb.halfOpenReqs, 0)
			cb.metrics.CircuitBreakerStateChanged("closed")
		}
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	atomic.StoreInt64(&cb.lastFailure, time.Now().UnixNano())
	count := atomic.AddInt32(&cb.failures, 1)

	if count >= cb.threshold {
		prevState := CircuitBreakerState(atomic.LoadInt32(&cb.state))
		atomic.StoreInt32(&cb.state, int32(StateOpen))
		atomic.StoreInt32(&cb.halfOpenReqs, 0)
		if prevState != StateOpen {
			cb.metrics.CircuitBreakerStateChanged("open")
		}
	}
}

func (cb *CircuitBreaker) ReleaseProbe() {
	if CircuitBreakerState(atomic.LoadInt32(&cb.state)) == StateHalfOpen {
		atomic.AddInt32(&cb.halfOpenReqs, -1)
	}
}
