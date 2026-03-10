package environment

import (
	"time"

	"middleware/base"
	"middleware/helper"
)

// BaseConfig returns the foundational configuration from environment variables
func BaseConfig() base.Config {
	config := base.DefaultConfig()

	// Security credentials from env
	config.AdminAuthToken = helper.GetEnv("ADMIN_AUTH_TOKEN", "")

	// Resource limits from env
	config.MaxTrackedIPs = helper.GetIntEnv("MAX_TRACKED_IPS", 10000)
	config.MaxMemoryMB = int64(helper.GetIntEnv("MAX_MEMORY_MB", 256))
	config.CleanupInterval = helper.GetDurationEnv("CLEANUP_INTERVAL_MINUTES", 5) * time.Minute
	config.MaxRequestBodySize = int64(helper.GetIntEnv("MAX_REQUEST_BODY_SIZE_MB", 1)) * 1024 * 1024

	// Violation handling from env
	config.BlockDuration = helper.GetDurationEnv("RATE_LIMIT_BLOCK_MINUTES", 15) * time.Minute
	config.MaxBlockDuration = helper.GetDurationEnv("RATE_LIMIT_MAX_BLOCK_HOURS", 1) * time.Hour
	config.ExponentialBackoffBase = helper.GetFloatEnv("EXPONENTIAL_BACKOFF_BASE", 2.0)
	config.EnableJitter = helper.GetBoolEnv("ENABLE_JITTER", true)
	config.JitterMaxPercent = helper.GetFloatEnv("JITTER_MAX_PERCENT", 0.1)
	config.DecayInterval = helper.GetDurationEnv("DECAY_INTERVAL_HOURS", 1) * time.Hour

	// Rate limits from env
	config.RequestsPerSecond = helper.GetFloatEnv("RATE_LIMIT_RPS", 0.2)
	config.BurstSize = helper.GetIntEnv("RATE_LIMIT_BURST", 3)
	config.GlobalRateLimit = helper.GetFloatEnv("GLOBAL_RATE_LIMIT_RPS", 10.0)
	config.GlobalBurst = helper.GetIntEnv("GLOBAL_BURST", 20)
	config.GlobalWindowSize = helper.GetDurationEnv("GLOBAL_WINDOW_SIZE_MINUTES", 1) * time.Minute

	// Circuit breaker from env
	config.CircuitBreakerThreshold = int32(helper.GetIntEnv("CIRCUIT_BREAKER_THRESHOLD", 5))
	config.CircuitBreakerTimeout = helper.GetDurationEnv("CIRCUIT_BREAKER_TIMEOUT_SECONDS", 30) * time.Second
	config.CircuitBreakerMaxProbes = int32(helper.GetIntEnv("CIRCUIT_BREAKER_MAX_PROBES", 3))
	// Redis from env
	config.RedisAddr = helper.GetEnv("REDIS_ADDR", "localhost:6379")
	config.RedisPassword = helper.GetEnv("REDIS_PASSWORD", "")
	config.RedisDB = helper.GetIntEnv("REDIS_DB", 0)
	config.RedisKeyPrefix = helper.GetEnv("REDIS_KEY_PREFIX", "ratelimit:")

	// Logging
	config.StructuredLogging = helper.GetBoolEnv("STRUCTURED_LOGGING", false)

	return config
}

// DevelopmentConfig returns configuration for localhost with relaxed limits
func DevelopmentConfig() base.Config {
	config := BaseConfig()
	config.BotDetection = base.DetectionNone
	config.BackendType = base.BackendMemory
	config.StructuredLogging = false
	config.StrictBackendErrors = false
	config.RequestsPerSecond = 100.0
	config.BurstSize = 100
	config.BlockDuration = 1 * time.Minute

	return config
}

// StagingConfig returns configuration for pre-production testing
func StagingConfig() base.Config {
	config := BaseConfig()

	config.BotDetection = base.DetectionMedium
	config.BackendType = base.BackendRedis
	config.StructuredLogging = true
	config.StrictBackendErrors = false
	config.RequestsPerSecond = 1.0
	config.BurstSize = 5
	config.BlockDuration = 15 * time.Minute

	return config
}

// ProductionConfig returns hardened configuration for production
func ProductionConfig() base.Config {
	config := BaseConfig()

	config.BotDetection = base.DetectionHigh
	config.BackendType = base.BackendRedis
	config.RedisKeyPrefix = "prod:ratelimit:"
	config.StructuredLogging = true
	config.StrictBackendErrors = true
	config.EnableIPv6SubnetBlock = true
	config.IPv6SubnetSize = 56
	config.TrustedUserIDHeader = true
	config.UserIDHeader = "X-Authenticated-User-ID"
	config.RequestsPerSecond = 0.2
	config.BurstSize = 3
	config.UseSlidingWindow = true
	config.SlidingWindowSize = time.Second
	config.DistributedAttackThreshold = 1000
	config.DistributedAttackWindow = time.Minute
	config.BlockDuration = 15 * time.Minute
	config.MaxBlockDuration = 24 * time.Hour

	return config
}

// ApplyEnvironmentConfig selects the appropriate config based on ENV variable
func ApplyEnvironmentConfig(config base.Config) base.Config {
	switch helper.GetEnv("ENV", "development") {
	case "production", "prod":
		return ProductionConfig()
	case "staging":
		return StagingConfig()
	default:
		return DevelopmentConfig()
	}
}
