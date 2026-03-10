package config

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/seriouscodehere/open-source/middleware/base"
	"github.com/seriouscodehere/open-source/middleware/helper"
)

// EnvironmentConfig represents environment-specific configuration
type EnvironmentConfig struct {
	Environment string `yaml:"environment"`

	Server struct {
		Port              string        `yaml:"port"`
		Host              string        `yaml:"host"`
		ReadTimeout       time.Duration `yaml:"read_timeout"`
		WriteTimeout      time.Duration `yaml:"write_timeout"`
		IdleTimeout       time.Duration `yaml:"idle_timeout"`
		MaxHeaderBytes    int           `yaml:"max_header_bytes"`
		StructuredLogging bool          `yaml:"structured_logging"`
	} `yaml:"server"`

	TLS struct {
		Enabled  bool   `yaml:"enabled"`
		CertFile string `yaml:"cert_file"`
		KeyFile  string `yaml:"key_file"`
	} `yaml:"tls"`

	Storage struct {
		Type  string `yaml:"type"`
		Redis struct {
			Addr         string        `yaml:"addr"`
			Password     string        `yaml:"password"`
			DB           int           `yaml:"db"`
			PoolSize     int           `yaml:"pool_size"`
			MinIdleConns int           `yaml:"min_idle_conns"`
			MaxRetries   int           `yaml:"max_retries"`
			DialTimeout  time.Duration `yaml:"dial_timeout"`
			ReadTimeout  time.Duration `yaml:"read_timeout"`
			WriteTimeout time.Duration `yaml:"write_timeout"`
		} `yaml:"redis"`
		Memory struct {
			MaxTrackedIPs   int           `yaml:"max_tracked_ips"`
			MaxMemoryMB     int           `yaml:"max_memory_mb"`
			CleanupInterval time.Duration `yaml:"cleanup_interval"`
		} `yaml:"memory"`
	} `yaml:"storage"`

	GlobalLimits struct {
		Enabled                    bool          `yaml:"enabled"`
		RequestsPerSecond          float64       `yaml:"requests_per_second"`
		BurstSize                  int           `yaml:"burst_size"`
		WindowSize                 time.Duration `yaml:"window_size"`
		DistributedAttackThreshold float64       `yaml:"distributed_attack_threshold"`
		DistributedAttackWindow    time.Duration `yaml:"distributed_attack_window"`
	} `yaml:"global_limits"`

	Security struct {
		IPStrategy          string   `yaml:"ip_strategy"`
		TrustedProxies      []string `yaml:"trusted_proxies"`
		TrustedHeader       string   `yaml:"trusted_header"`
		EnableIPv6Subnet    bool     `yaml:"enable_ipv6_subnet_block"`
		IPv6SubnetSize      int      `yaml:"ipv6_subnet_size"`
		TrustedUserIDHeader bool     `yaml:"trusted_user_id_header"`
		UserIDHeader        string   `yaml:"user_id_header"`
		AdminAuthToken      string   `yaml:"admin_auth_token"`
	} `yaml:"security"`

	BotDetection struct {
		Level                string   `yaml:"level"`
		AdditionalSignatures []string `yaml:"additional_signatures"`
	} `yaml:"bot_detection"`

	// FIXED: Use int32 to match base.Config
	CircuitBreaker struct {
		Threshold int32         `yaml:"threshold"`
		Timeout   time.Duration `yaml:"timeout"`
		MaxProbes int32         `yaml:"max_probes"`
	} `yaml:"circuit_breaker"`

	ViolationHandling struct {
		BaseBlockDuration      time.Duration `yaml:"base_block_duration"`
		MaxBlockDuration       time.Duration `yaml:"max_block_duration"`
		ExponentialBackoffBase float64       `yaml:"exponential_backoff_base"`
		EnableJitter           bool          `yaml:"enable_jitter"`
		JitterMaxPercent       float64       `yaml:"jitter_max_percent"`
		DecayInterval          time.Duration `yaml:"decay_interval"`
		DecayRate              float64       `yaml:"decay_rate"`
	} `yaml:"violation_handling"`

	RateLimiting struct {
		DefaultAlgorithm    string        `yaml:"default_algorithm"`
		SlidingWindowSize   time.Duration `yaml:"sliding_window_size"`
		StrictBackendErrors bool          `yaml:"strict_backend_errors"`
	} `yaml:"rate_limiting"`

	Logging struct {
		Level         string   `yaml:"level"`
		Structured    bool     `yaml:"structured"`
		IncludeFields []string `yaml:"include_fields"`
	} `yaml:"logging"`

	Metrics struct {
		Enabled    bool `yaml:"enabled"`
		Prometheus struct {
			Enabled bool   `yaml:"enabled"`
			Path    string `yaml:"path"`
			Port    string `yaml:"port"`
		} `yaml:"prometheus"`
	} `yaml:"metrics"`

	ClientCookies struct {
		Enabled  bool   `yaml:"enabled"`
		Name     string `yaml:"name"`
		Secure   bool   `yaml:"secure"`
		SameSite string `yaml:"same_site"`
	} `yaml:"client_cookies"`

	CORS struct {
		Enabled        bool     `yaml:"enabled"`
		AllowedOrigins []string `yaml:"allowed_origins"`
		AllowedMethods []string `yaml:"allowed_methods"`
		AllowedHeaders []string `yaml:"allowed_headers"`
		MaxAge         int      `yaml:"max_age"`
	} `yaml:"cors"`

	Health struct {
		Enabled  bool          `yaml:"enabled"`
		Path     string        `yaml:"path"`
		Interval time.Duration `yaml:"interval"`
	} `yaml:"health"`

	Shutdown struct {
		Timeout      time.Duration `yaml:"timeout"`
		DrainTimeout time.Duration `yaml:"drain_timeout"`
	} `yaml:"shutdown"`

	// Service templates for dynamic API creation
	ServiceTemplates map[string]ServiceTemplate `yaml:"service_templates"`
}

// ServiceTemplate defines default limits for a service type
type ServiceTemplate struct {
	RequestsPerSecond float64       `yaml:"requests_per_second"`
	BurstSize         int           `yaml:"burst_size"`
	WindowSize        time.Duration `yaml:"window_size"`
	BlockDuration     time.Duration `yaml:"block_duration"`
	Description       string        `yaml:"description"`
}

// LoadEnvironmentConfig loads environment-specific configuration
func LoadEnvironmentConfig(env string) (*EnvironmentConfig, error) {
	if env == "" {
		env = "development"
	}

	// Sanitize environment name
	env = strings.ToLower(strings.TrimSpace(env))

	configDir := helper.GetEnv("CONFIG_DIR", "config")
	configPath := filepath.Join(configDir, "environments", fmt.Sprintf("%s.yaml", env))

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Fall back to default if file doesn't exist
			return getDefaultConfig(env), nil
		}
		return nil, fmt.Errorf("failed to read environment config %s: %w", configPath, err)
	}

	var config EnvironmentConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse environment config: %w", err)
	}

	// Expand environment variables in strings
	expandEnvVars(&config)

	config.Environment = env
	return &config, nil
}

// expandEnvVars expands ${VAR} and $VAR in config strings
func expandEnvVars(config *EnvironmentConfig) {
	config.Security.AdminAuthToken = os.ExpandEnv(config.Security.AdminAuthToken)
	config.Storage.Redis.Addr = os.ExpandEnv(config.Storage.Redis.Addr)
	config.Storage.Redis.Password = os.ExpandEnv(config.Storage.Redis.Password)
}

// getDefaultConfig returns sensible defaults based on environment
func getDefaultConfig(env string) *EnvironmentConfig {
	cfg := &EnvironmentConfig{
		Environment: env,
	}

	// Set defaults based on environment
	switch env {
	case "production":
		cfg.Server.Port = "8080"
		cfg.Server.StructuredLogging = true
		cfg.Storage.Type = "redis"
		cfg.GlobalLimits.Enabled = true
		cfg.GlobalLimits.RequestsPerSecond = 10000
		cfg.GlobalLimits.BurstSize = 20000
		cfg.Security.IPStrategy = "cloudflare"
		cfg.BotDetection.Level = "high"
		cfg.CircuitBreaker.Threshold = 3 // int32
		cfg.CircuitBreaker.MaxProbes = 1 // int32
		cfg.ViolationHandling.BaseBlockDuration = 15 * time.Minute
		cfg.ViolationHandling.MaxBlockDuration = 24 * time.Hour
		cfg.Logging.Level = "warn"
		cfg.Logging.Structured = true
		cfg.ClientCookies.Enabled = true
		cfg.ClientCookies.Secure = true

	case "staging":
		cfg.Server.Port = "8080"
		cfg.Server.StructuredLogging = true
		cfg.Storage.Type = "redis"
		cfg.GlobalLimits.Enabled = true
		cfg.GlobalLimits.RequestsPerSecond = 500
		cfg.GlobalLimits.BurstSize = 1000
		cfg.Security.IPStrategy = "trusted_proxy"
		cfg.BotDetection.Level = "medium"
		cfg.CircuitBreaker.Threshold = 5 // int32
		cfg.CircuitBreaker.MaxProbes = 3 // int32
		cfg.ViolationHandling.BaseBlockDuration = 15 * time.Minute
		cfg.ViolationHandling.MaxBlockDuration = 2 * time.Hour
		cfg.Logging.Level = "info"
		cfg.Logging.Structured = true
		cfg.ClientCookies.Enabled = true

	default: // development
		cfg.Server.Port = "8080"
		cfg.Server.StructuredLogging = false
		cfg.Storage.Type = "memory"
		cfg.GlobalLimits.Enabled = false
		cfg.Security.IPStrategy = "direct"
		cfg.BotDetection.Level = "none"
		cfg.CircuitBreaker.Threshold = 100 // int32
		cfg.CircuitBreaker.MaxProbes = 1   // int32
		cfg.ViolationHandling.BaseBlockDuration = 1 * time.Minute
		cfg.ViolationHandling.MaxBlockDuration = 5 * time.Minute
		cfg.Logging.Level = "debug"
		cfg.Logging.Structured = false
		cfg.ClientCookies.Enabled = false
	}

	return cfg
}

// ToBaseConfig converts EnvironmentConfig to *base.Config
// FIXED: Returns pointer instead of value
func (ec *EnvironmentConfig) ToBaseConfig() *base.Config {
	cfg := base.DefaultConfig()

	// Server settings
	if ec.Server.Port != "" {
		os.Setenv("PORT", ec.Server.Port)
	}
	cfg.StructuredLogging = ec.Server.StructuredLogging

	// Copy server settings to cfg.Server
	cfg.Server.Port = ec.Server.Port
	cfg.Server.Host = ec.Server.Host
	cfg.Server.ReadTimeout = ec.Server.ReadTimeout
	cfg.Server.WriteTimeout = ec.Server.WriteTimeout
	cfg.Server.IdleTimeout = ec.Server.IdleTimeout

	// Storage settings
	switch ec.Storage.Type {
	case "redis":
		cfg.BackendType = base.BackendRedis
		cfg.RedisAddr = ec.Storage.Redis.Addr
		cfg.RedisPassword = ec.Storage.Redis.Password
		cfg.RedisDB = ec.Storage.Redis.DB
	case "memory":
		cfg.BackendType = base.BackendMemory
		if ec.Storage.Memory.MaxTrackedIPs > 0 {
			cfg.MaxTrackedIPs = ec.Storage.Memory.MaxTrackedIPs
		}
	}

	// Security settings
	cfg.AdminAuthToken = ec.Security.AdminAuthToken
	cfg.IPStrategy = parseIPStrategy(ec.Security.IPStrategy)
	cfg.TrustedProxies = ec.Security.TrustedProxies
	cfg.TrustedHeader = ec.Security.TrustedHeader
	cfg.EnableIPv6SubnetBlock = ec.Security.EnableIPv6Subnet
	cfg.IPv6SubnetSize = ec.Security.IPv6SubnetSize
	cfg.TrustedUserIDHeader = ec.Security.TrustedUserIDHeader
	cfg.UserIDHeader = ec.Security.UserIDHeader

	// Global limits
	if ec.GlobalLimits.Enabled {
		cfg.GlobalRateLimit = ec.GlobalLimits.RequestsPerSecond
		cfg.GlobalBurst = ec.GlobalLimits.BurstSize
		cfg.GlobalWindowSize = ec.GlobalLimits.WindowSize
		cfg.DistributedAttackThreshold = ec.GlobalLimits.DistributedAttackThreshold
		cfg.DistributedAttackWindow = ec.GlobalLimits.DistributedAttackWindow
	}

	// Bot detection
	cfg.BotDetection = parseBotDetectionLevel(ec.BotDetection.Level)
	cfg.AdditionalBotSignatures = ec.BotDetection.AdditionalSignatures
	// Circuit breaker - FIXED: Now both are int32
	cfg.CircuitBreakerThreshold = ec.CircuitBreaker.Threshold
	cfg.CircuitBreakerTimeout = ec.CircuitBreaker.Timeout
	cfg.CircuitBreakerMaxProbes = ec.CircuitBreaker.MaxProbes

	// Violation handling
	cfg.BlockDuration = ec.ViolationHandling.BaseBlockDuration
	cfg.MaxBlockDuration = ec.ViolationHandling.MaxBlockDuration
	cfg.ExponentialBackoffBase = ec.ViolationHandling.ExponentialBackoffBase
	cfg.EnableJitter = ec.ViolationHandling.EnableJitter
	cfg.JitterMaxPercent = ec.ViolationHandling.JitterMaxPercent
	cfg.DecayInterval = ec.ViolationHandling.DecayInterval

	// Rate limiting algorithm
	cfg.UseSlidingWindow = ec.RateLimiting.DefaultAlgorithm == "sliding_window"
	if ec.RateLimiting.SlidingWindowSize > 0 {
		cfg.SlidingWindowSize = ec.RateLimiting.SlidingWindowSize
	}
	cfg.StrictBackendErrors = ec.RateLimiting.StrictBackendErrors

	// Cookie settings
	cfg.BlockStatusCookieEnabled = ec.ClientCookies.Enabled
	cfg.BlockStatusCookieName = ec.ClientCookies.Name
	cfg.CookieSecure = ec.ClientCookies.Secure
	cfg.CookieSameSite = parseSameSite(ec.ClientCookies.SameSite)

	// CORS settings
	cfg.CORSEnabled = ec.CORS.Enabled
	cfg.CORSAllowedOrigins = ec.CORS.AllowedOrigins
	cfg.CORSAllowedMethods = ec.CORS.AllowedMethods
	cfg.CORSAllowedHeaders = ec.CORS.AllowedHeaders
	cfg.CORSMaxAge = ec.CORS.MaxAge

	// Health settings
	cfg.HealthEnabled = ec.Health.Enabled
	cfg.HealthPath = ec.Health.Path

	// Shutdown settings
	cfg.ShutdownTimeout = ec.Shutdown.Timeout
	cfg.ShutdownDrainTimeout = ec.Shutdown.DrainTimeout

	// Note: PathLimits is intentionally left empty - APIs are registered dynamically via admin API

	// FIXED: Return pointer
	return &cfg
}

// Helper functions
func parseIPStrategy(s string) base.IPExtractorStrategy {
	switch strings.ToLower(s) {
	case "cloudflare":
		return base.StrategyCloudflare
	case "trusted_proxy":
		return base.StrategyTrustedProxy
	case "aws":
		return base.StrategyAWS
	default:
		return base.StrategyDirect
	}
}

func parseBotDetectionLevel(s string) base.BotDetectionLevel {
	switch strings.ToLower(s) {
	case "none":
		return base.DetectionNone
	case "low":
		return base.DetectionLow
	case "medium":
		return base.DetectionMedium
	case "high":
		return base.DetectionHigh
	default:
		return base.DetectionMedium
	}
}

func parseSameSite(s string) http.SameSite {
	switch strings.ToLower(s) {
	case "strict":
		return http.SameSiteStrictMode
	case "lax":
		return http.SameSiteLaxMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

// LoadConfig is the main entry point for loading configuration
func LoadConfig() (*base.Config, *EnvironmentConfig, error) {
	env := helper.GetEnv("ENV", "development")

	// Load environment-specific config
	envConfig, err := LoadEnvironmentConfig(env)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load environment config: %w", err)
	}

	// Convert to base.Config - now returns pointer
	baseConfig := envConfig.ToBaseConfig()

	// Apply any additional environment variable overrides
	applyEnvOverrides(baseConfig)

	// Validate final config
	if err := baseConfig.Validate(); err != nil {
		return nil, nil, fmt.Errorf("config validation failed: %w", err)
	}

	return baseConfig, envConfig, nil
}

// applyEnvOverrides applies environment variable overrides
func applyEnvOverrides(cfg *base.Config) {
	// These override whatever was set in YAML
	if v := helper.GetEnv("ADMIN_AUTH_TOKEN", ""); v != "" {
		cfg.AdminAuthToken = v
	}
	if v := helper.GetEnv("REDIS_ADDR", ""); v != "" {
		cfg.RedisAddr = v
	}
	if v := helper.GetEnv("REDIS_PASSWORD", ""); v != "" {
		cfg.RedisPassword = v
	}
}
