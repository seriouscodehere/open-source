// base/config.go
package base

import (
	"fmt"
	"net/http"
	"sort"
	"time"
)

type Config struct {
	// Server settings
	Server struct {
		Port         string
		Host         string
		ReadTimeout  time.Duration
		WriteTimeout time.Duration
		IdleTimeout  time.Duration
	} `json:"-"`

	// TLS Settings (Added)
	TLSCertFile string
	TLSKeyFile  string

	// Core rate limiting
	RequestsPerSecond float64
	BurstSize         int
	BlockDuration     time.Duration
	MaxBlockDuration  time.Duration
	BotDetection      BotDetectionLevel
	IPStrategy        IPExtractorStrategy
	TrustedProxies    []string
	TrustedHeader     string
	MaxMemoryMB       int64
	MaxTrackedIPs     int
	CleanupInterval   time.Duration
	BackendType       BackendType
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
	RedisKeyPrefix    string
	Logger            Logger
	Metrics           Metrics

	// Admin & Security
	AdminAuthToken        string
	PathLimits            []PathLimit
	UserIDHeader          string
	TrustedUserIDHeader   bool
	ProxyValidationFunc   func(r *http.Request) bool
	EnableIPv6SubnetBlock bool
	IPv6SubnetSize        int

	// Circuit breaker
	CircuitBreakerThreshold int32
	CircuitBreakerTimeout   time.Duration
	CircuitBreakerMaxProbes int32

	// Violation handling
	DecayInterval              time.Duration
	StructuredLogging          bool
	MaxRequestBodySize         int64
	GlobalRateLimit            float64
	GlobalBurst                int
	GlobalWindowSize           time.Duration
	UseSlidingWindow           bool
	SlidingWindowSize          time.Duration
	EnableJitter               bool
	JitterMaxPercent           float64
	ExponentialBackoffBase     float64
	StrictBackendErrors        bool
	DistributedAttackThreshold float64
	DistributedAttackWindow    time.Duration

	// Cookies
	BlockStatusCookieName    string
	BlockStatusCookieEnabled bool
	CookieSecure             bool
	CookieSameSite           http.SameSite

	// CORS (Added)
	CORSEnabled        bool
	CORSAllowedOrigins []string
	CORSAllowedMethods []string
	CORSAllowedHeaders []string
	CORSMaxAge         int

	// Health check (Added)
	HealthEnabled bool
	HealthPath    string

	// Shutdown (Added)
	ShutdownTimeout      time.Duration
	ShutdownDrainTimeout time.Duration

	// Bot detection signatures (Added)
	AdditionalBotSignatures []string
}

func (c *Config) Validate() error {
	if c.RequestsPerSecond <= 0 {
		return fmt.Errorf("%w: RequestsPerSecond must be positive", ErrInvalidConfig)
	}
	if c.BurstSize < 1 {
		return fmt.Errorf("%w: BurstSize must be at least 1", ErrInvalidConfig)
	}
	if c.BlockDuration <= 0 {
		return fmt.Errorf("%w: BlockDuration must be positive", ErrInvalidConfig)
	}
	if c.MaxBlockDuration > 0 && c.MaxBlockDuration < c.BlockDuration {
		return fmt.Errorf("%w: MaxBlockDuration must be >= BlockDuration", ErrInvalidConfig)
	}
	if c.CleanupInterval <= 0 {
		c.CleanupInterval = 5 * time.Minute
	}
	if c.MaxMemoryMB == 0 {
		c.MaxMemoryMB = 512
	}
	if c.MaxTrackedIPs == 0 {
		c.MaxTrackedIPs = 100000
	}
	if c.BackendType == BackendRedis && c.RedisAddr == "" {
		return fmt.Errorf("%w: RedisAddr required when using Redis backend", ErrInvalidConfig)
	}

	if c.CircuitBreakerThreshold == 0 {
		c.CircuitBreakerThreshold = 5
	}
	if c.CircuitBreakerTimeout == 0 {
		c.CircuitBreakerTimeout = 30 * time.Second
	}
	if c.CircuitBreakerMaxProbes == 0 {
		c.CircuitBreakerMaxProbes = 1
	}
	if c.DecayInterval == 0 {
		c.DecayInterval = 1 * time.Hour
	}
	if c.RedisKeyPrefix == "" {
		c.RedisKeyPrefix = "ratelimit:"
	}
	if c.MaxRequestBodySize == 0 {
		c.MaxRequestBodySize = 10 * 1024 * 1024
	}
	if c.GlobalWindowSize == 0 {
		c.GlobalWindowSize = time.Minute
	}
	if c.UseSlidingWindow && c.SlidingWindowSize == 0 {
		c.SlidingWindowSize = time.Second
	}
	if c.JitterMaxPercent == 0 {
		c.JitterMaxPercent = 0.1
	}
	if c.ExponentialBackoffBase == 0 {
		c.ExponentialBackoffBase = 2.0
	}
	if c.IPv6SubnetSize == 0 {
		c.IPv6SubnetSize = 56
	}
	if c.DistributedAttackThreshold == 0 {
		c.DistributedAttackThreshold = 1000
	}
	if c.DistributedAttackWindow == 0 {
		c.DistributedAttackWindow = time.Minute
	}

	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	if c.ShutdownDrainTimeout == 0 {
		c.ShutdownDrainTimeout = 10 * time.Second
	}
	if c.HealthPath == "" {
		c.HealthPath = "/health"
	}

	sort.Slice(c.PathLimits, func(i, j int) bool {
		return c.PathLimits[i].Priority > c.PathLimits[j].Priority
	})

	return nil
}

func DefaultConfig() Config {
	cfg := Config{
		RequestsPerSecond:          10,
		BurstSize:                  20,
		BlockDuration:              15 * time.Minute,
		MaxBlockDuration:           24 * time.Hour,
		BotDetection:               DetectionMedium,
		IPStrategy:                 StrategyDirect,
		CleanupInterval:            5 * time.Minute,
		MaxMemoryMB:                512,
		MaxTrackedIPs:              100000,
		BackendType:                BackendMemory,
		DecayInterval:              1 * time.Hour,
		RedisKeyPrefix:             "ratelimit:",
		CircuitBreakerMaxProbes:    1,
		MaxRequestBodySize:         10 * 1024 * 1024,
		PathLimits:                 make([]PathLimit, 0),
		SlidingWindowSize:          time.Second,
		GlobalWindowSize:           time.Minute,
		JitterMaxPercent:           0.1,
		ExponentialBackoffBase:     2.0,
		IPv6SubnetSize:             56,
		DistributedAttackThreshold: 1000,
		DistributedAttackWindow:    time.Minute,
		BlockStatusCookieName:      "__block_status",
		BlockStatusCookieEnabled:   true,
		CookieSecure:               false,
		CookieSameSite:             http.SameSiteLaxMode,
		HealthEnabled:              true,
		HealthPath:                 "/health",
		ShutdownTimeout:            30 * time.Second,
		ShutdownDrainTimeout:       10 * time.Second,
		CORSEnabled:                true,
		CORSAllowedOrigins:         []string{"*"},
		CORSAllowedMethods:         []string{"GET", "POST", "OPTIONS"},
		CORSAllowedHeaders:         []string{"Content-Type", "Authorization", "X-API-Key"},
		CORSMaxAge:                 86400,
	}

	// Initialize Server struct
	cfg.Server.Port = "8080"
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.ReadTimeout = 5 * time.Second
	cfg.Server.WriteTimeout = 10 * time.Second
	cfg.Server.IdleTimeout = 120 * time.Second

	return cfg
}
