package base

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Errors
var (
	ErrRateLimitExceeded    = errors.New("rate limit exceeded")
	ErrIPBlocked            = errors.New("IP address blocked")
	ErrBotDetected          = errors.New("bot detected")
	ErrVerificationRequired = errors.New("verification required")
	ErrInvalidConfig        = errors.New("invalid configuration")
	ErrBackendUnavailable   = errors.New("backend unavailable")
	ErrUnauthorized         = errors.New("unauthorized")
)

// Enums
type BotDetectionLevel int

const (
	DetectionNone BotDetectionLevel = iota
	DetectionLow
	DetectionMedium
	DetectionHigh
)

type IPExtractorStrategy int

const (
	StrategyDirect IPExtractorStrategy = iota
	StrategyTrustedProxy
	StrategyCloudflare
	StrategyAWS
)

type BackendType int

const (
	BackendMemory BackendType = iota
	BackendRedis
)

type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateOpen
	StateHalfOpen
)

// Config structs
type RateLimitConfig struct {
	RequestsPerSecond float64
	BurstSize         int
	WindowSize        time.Duration
}

type PathLimit struct {
	Prefix   string
	Config   RateLimitConfig
	Priority int
}

// Interfaces
type Logger interface {
	Debug(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
}

type Metrics interface {
	RateLimitHit(ip string, path string)
	RequestAllowed(ip string, path string)
	BlockApplied(ip string, duration time.Duration)
	BotDetected(ip string, reason string)
	MemoryPressureTriggered(currentMB int64)
	BackendFailure(error string)
	CircuitBreakerStateChanged(state string)
	DistributedAttackDetected(count int)
}

// Backend interface
type Backend interface {
	Allow(ctx context.Context, key string, rps float64, burst int, windowSize time.Duration) (bool, time.Duration, error)
	AllowSlidingWindow(ctx context.Context, key string, limit int, windowSize time.Duration) (bool, time.Duration, error)
	Block(ctx context.Context, key string, duration time.Duration) error
	IsBlocked(ctx context.Context, key string) (bool, time.Time, error)
	GetTokenBucket(ctx context.Context, key string) (tokens float64, lastUpdate time.Time, err error)
	SetTokenBucket(ctx context.Context, key string, tokens float64, lastUpdate time.Time) error
	GetSlidingWindowCount(ctx context.Context, key string, windowSize time.Duration) (int, error)
	GetViolationCount(ctx context.Context, key string) (int32, error)
	IncrementViolation(ctx context.Context, key string) (int32, error)
	DecayViolations(ctx context.Context, keys []string, decayInterval time.Duration) error
	Close() error
}

// DefaultLogger implementation
type DefaultLogger struct {
	Structured bool
}

func (d DefaultLogger) Debug(msg string, kv ...interface{}) {
	if d.Structured {
		d.logStructured("DEBUG", msg, kv)
	} else {
		fmt.Printf("[DEBUG] "+msg+"\n", kv...)
	}
}

func (d DefaultLogger) Info(msg string, kv ...interface{}) {
	if d.Structured {
		d.logStructured("INFO", msg, kv)
	} else {
		fmt.Printf("[INFO] "+msg+"\n", kv...)
	}
}

func (d DefaultLogger) Warn(msg string, kv ...interface{}) {
	if d.Structured {
		d.logStructured("WARN", msg, kv)
	} else {
		fmt.Printf("[WARN] "+msg+"\n", kv...)
	}
}

func (d DefaultLogger) Error(msg string, kv ...interface{}) {
	if d.Structured {
		d.logStructured("ERROR", msg, kv)
	} else {
		fmt.Printf("[ERROR] "+msg+"\n", kv...)
	}
}

func (d DefaultLogger) logStructured(level, msg string, kv []interface{}) {
	fields := make(map[string]interface{})
	fields["level"] = level
	fields["msg"] = msg
	fields["ts"] = time.Now().UTC().Format(time.RFC3339)

	for i := 0; i < len(kv)-1; i += 2 {
		if key, ok := kv[i].(string); ok {
			fields[key] = kv[i+1]
		}
	}

	json.NewEncoder(os.Stdout).Encode(fields)
}

// NoopMetrics implementation
type NoopMetrics struct{}

func (n NoopMetrics) RateLimitHit(ip, path string)            {}
func (n NoopMetrics) RequestAllowed(ip, path string)          {}
func (n NoopMetrics) BlockApplied(ip string, d time.Duration) {}
func (n NoopMetrics) BotDetected(ip string, reason string)    {}
func (n NoopMetrics) MemoryPressureTriggered(currentMB int64) {}
func (n NoopMetrics) BackendFailure(error string)             {}
func (n NoopMetrics) CircuitBreakerStateChanged(state string) {}
func (n NoopMetrics) DistributedAttackDetected(count int)     {}
