package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"middleware/base"
	"middleware/config"
	"middleware/helper"
	"middleware/metrics"
	"middleware/routing"
	"middleware/storage/file"
	"middleware/storage/memory"
	redisstore "middleware/storage/redis"

	"github.com/go-redis/redis/v8"
)

// ConfigInfo holds information about loaded configuration
type ConfigInfo struct {
	Environment     string            `json:"environment"`
	ConfigFiles     []string          `json:"config_files_loaded"`
	ConfigValues    map[string]string `json:"config_values"`
	EnvironmentVars map[string]string `json:"environment_variables"`
	StorageType     string            `json:"storage_type"`
	RegistryType    string            `json:"registry_type"`
	RegistryFile    string            `json:"registry_file,omitempty"`
	BackendType     string            `json:"backend_type"`
	AdminAPIEnabled bool              `json:"admin_api_enabled"`
	ServerPort      string            `json:"server_port"`
	RedisAddr       string            `json:"redis_addr,omitempty"`
}

var (
	loadedConfigFiles []string
)

func logInfo(msg string, fields map[string]interface{}) {
	fmt.Printf("[INFO] %s", msg)
	if len(fields) > 0 {
		fmt.Printf(" | ")
		first := true
		for k, v := range fields {
			if !first {
				fmt.Printf(", ")
			}
			fmt.Printf("%s=%v", k, v)
			first = false
		}
	}
	fmt.Println()
}

func main() {
	fmt.Println("=== Starting Rate Limiter (Reverse Proxy Mode) ===")

	configDir := helper.GetEnv("CONFIG_DIR", "config")
	env := helper.GetEnv("ENV", "development")

	envConfigPath := filepath.Join(configDir, "environments", fmt.Sprintf("%s.yaml", env))
	loadedConfigFiles = append(loadedConfigFiles, envConfigPath)

	if _, err := os.Stat(envConfigPath); os.IsNotExist(err) {
		fmt.Printf("Warning: Config file not found: %s (using defaults)\n", envConfigPath)
	}

	cfg, envCfg, err := config.LoadConfig()
	if err != nil {
		panic(fmt.Sprintf("Failed to load config: %v", err))
	}

	if cfg.Logger == nil {
		cfg.Logger = helper.DefaultLogger{Structured: cfg.StructuredLogging}
	}
	if cfg.Metrics == nil {
		cfg.Metrics = helper.NoopMetrics{}
	}

	logInfo("Starting rate limiter", map[string]interface{}{
		"environment":   envCfg.Environment,
		"storage":       envCfg.Storage.Type,
		"config_file":   envConfigPath,
		"config_exists": fileExists(envConfigPath),
	})

	extractor, err := helper.NewIPExtractor(int(cfg.IPStrategy), cfg.TrustedProxies, cfg.TrustedHeader)
	if err != nil {
		panic(fmt.Sprintf("Failed to create IP extractor: %v", err))
	}

	var redisClient *redis.Client

	if cfg.BackendType == base.BackendRedis || cfg.RedisAddr != "" {
		redisOpts := &redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		}

		if envCfg.Storage.Redis.PoolSize > 0 {
			redisOpts.PoolSize = envCfg.Storage.Redis.PoolSize
		}
		if envCfg.Storage.Redis.MinIdleConns > 0 {
			redisOpts.MinIdleConns = envCfg.Storage.Redis.MinIdleConns
		}
		if envCfg.Storage.Redis.MaxRetries > 0 {
			redisOpts.MaxRetries = envCfg.Storage.Redis.MaxRetries
		}
		if envCfg.Storage.Redis.DialTimeout > 0 {
			redisOpts.DialTimeout = envCfg.Storage.Redis.DialTimeout
		}
		if envCfg.Storage.Redis.ReadTimeout > 0 {
			redisOpts.ReadTimeout = envCfg.Storage.Redis.ReadTimeout
		}
		if envCfg.Storage.Redis.WriteTimeout > 0 {
			redisOpts.WriteTimeout = envCfg.Storage.Redis.WriteTimeout
		}

		redisClient = redis.NewClient(redisOpts)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := redisClient.Ping(ctx).Err(); err != nil {
			cancel()
			panic(fmt.Sprintf("Redis connection failed: %v", err))
		}
		cancel()
	}

	registryFile := helper.GetEnv("REGISTRY_FILE", "data/apis.json")

	fmt.Printf("=== Initializing FileRegistry: %s ===\n", registryFile)

	fileReg := file.NewFileRegistry(registryFile)

	fmt.Println("=== Loading registry file ===")
	if err := fileReg.Load(); err != nil {
		logInfo("Failed to load registry file, starting fresh", map[string]interface{}{
			"error": err.Error(),
		})
	} else {
		fmt.Println("=== Registry loaded successfully ===")
	}

	var registry base.RuleRegistry = fileReg

	logInfo("Using file-based registry for API persistence", map[string]interface{}{
		"file": registryFile,
	})

	// Initialize Event Store
	eventsDir := helper.GetEnv("EVENTS_DIR", "data/events")
	eventStore, err := base.NewEventStore(eventsDir)
	if err != nil {
		panic(fmt.Sprintf("Failed to create event store: %v", err))
	}
	defer eventStore.Close()

	logInfo("Event store initialized", map[string]interface{}{
		"events_dir": eventsDir,
	})

	registryAdapter := &RegistryAdapter{registry: registry}

	metricsDir := helper.GetEnv("METRICS_DIR", "data/metrics")
	metricsCollector, err := metrics.NewMetricsCollector(metricsDir, registryAdapter)
	if err != nil {
		panic(fmt.Sprintf("Failed to create metrics collector: %v", err))
	}
	defer metricsCollector.Close()

	logInfo("Metrics collector initialized", map[string]interface{}{
		"metrics_dir": metricsDir,
	})

	cb := base.NewCircuitBreaker(
		cfg.CircuitBreakerThreshold,
		cfg.CircuitBreakerTimeout,
		cfg.CircuitBreakerMaxProbes,
		cfg.Metrics,
	)

	var backend base.Backend

	if cfg.BackendType == base.BackendRedis && redisClient != nil {
		redisBackend, err := redisstore.NewBackendWithClient(
			redisClient,
			cb,
			cfg.RedisKeyPrefix,
			cfg.DecayInterval,
		)
		if err != nil {
			panic(fmt.Sprintf("Failed to create Redis backend: %v", err))
		}
		backend = redisBackend
		logInfo("Using Redis backend", nil)
	} else {
		windowSize := cfg.SlidingWindowSize
		if !cfg.UseSlidingWindow {
			windowSize = time.Duration(float64(cfg.BurstSize)/cfg.RequestsPerSecond) * time.Second
		}

		maxIPs := cfg.MaxTrackedIPs
		if envCfg.Storage.Memory.MaxTrackedIPs > 0 {
			maxIPs = envCfg.Storage.Memory.MaxTrackedIPs
		}

		backend = memory.NewBackend(
			maxIPs,
			true,
			windowSize,
			cfg.RequestsPerSecond,
			cfg.BurstSize,
		)
		logInfo("Using memory backend", nil)
	}

	ctx, cancel := context.WithCancel(context.Background())

	limiter := &base.Limiter{
		Config:                   *cfg,
		Extractor:                extractor,
		Backend:                  backend,
		Registry:                 registry,
		Ctx:                      ctx,
		Cancel:                   cancel,
		TrafficStats:             &base.TrafficStats{WindowStart: time.Now()},
		DistributedAttackCounter: base.NewDistributedAttackDetector(cfg.DistributedAttackWindow, cfg.DistributedAttackThreshold),
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	rateMiddleware := routing.NewMiddleware(limiter, registry)
	r.Use(rateMiddleware.Handler)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	r.Get("/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		configValues := map[string]string{
			"ENV":                   envCfg.Environment,
			"CONFIG_DIR":            configDir,
			"PORT":                  cfg.Server.Port,
			"HOST":                  cfg.Server.Host,
			"ADMIN_AUTH_TOKEN":      maskIfSet(cfg.AdminAuthToken),
			"REDIS_ADDR":            cfg.RedisAddr,
			"REDIS_PASSWORD":        maskIfSet(cfg.RedisPassword),
			"REDIS_DB":              fmt.Sprintf("%d", cfg.RedisDB),
			"REQUESTS_PER_SECOND":   fmt.Sprintf("%.2f", cfg.RequestsPerSecond),
			"BURST_SIZE":            fmt.Sprintf("%d", cfg.BurstSize),
			"BLOCK_DURATION":        cfg.BlockDuration.String(),
			"MAX_BLOCK_DURATION":    cfg.MaxBlockDuration.String(),
			"GLOBAL_RATE_LIMIT":     fmt.Sprintf("%.2f", cfg.GlobalRateLimit),
			"GLOBAL_BURST":          fmt.Sprintf("%d", cfg.GlobalBurst),
			"CORS_ENABLED":          fmt.Sprintf("%t", cfg.CORSEnabled),
			"BOT_DETECTION_LEVEL":   fmt.Sprintf("%d", cfg.BotDetection),
			"TLS_CERT_FILE":         cfg.TLSCertFile,
			"TLS_KEY_FILE":          cfg.TLSKeyFile,
			"LOG_LEVEL":             envCfg.Logging.Level,
			"REGISTRY_FILE":         registryFile,
			"METRICS_DIR":           metricsDir,
			"EVENTS_DIR":            eventsDir,
			"STORAGE_TYPE":          envCfg.Storage.Type,
			"BACKEND_TYPE":          fmt.Sprintf("%d", cfg.BackendType),
			"STRUCTURED_LOGGING":    fmt.Sprintf("%t", cfg.StructuredLogging),
			"IP_STRATEGY":           fmt.Sprintf("%d", cfg.IPStrategy),
			"USE_SLIDING_WINDOW":    fmt.Sprintf("%t", cfg.UseSlidingWindow),
			"SLIDING_WINDOW_SIZE":   cfg.SlidingWindowSize.String(),
			"DECAY_INTERVAL":        cfg.DecayInterval.String(),
			"MAX_TRACKED_IPS":       fmt.Sprintf("%d", cfg.MaxTrackedIPs),
			"ENABLE_IPV6_SUBNET":    fmt.Sprintf("%t", cfg.EnableIPv6SubnetBlock),
			"IPV6_SUBNET_SIZE":      fmt.Sprintf("%d", cfg.IPv6SubnetSize),
			"TRUSTED_PROXIES":       fmt.Sprintf("%v", cfg.TrustedProxies),
			"TRUSTED_HEADER":        cfg.TrustedHeader,
			"MAX_REQUEST_BODY_SIZE": fmt.Sprintf("%d", cfg.MaxRequestBodySize),
			"READ_TIMEOUT":          envCfg.Server.ReadTimeout.String(),
			"WRITE_TIMEOUT":         envCfg.Server.WriteTimeout.String(),
			"IDLE_TIMEOUT":          envCfg.Server.IdleTimeout.String(),
			"SHUTDOWN_TIMEOUT":      envCfg.Shutdown.Timeout.String(),
		}

		envVars := map[string]string{
			"ENV":                 getEnvStatus("ENV"),
			"CONFIG_DIR":          getEnvStatus("CONFIG_DIR"),
			"PORT":                getEnvStatus("PORT"),
			"ADMIN_AUTH_TOKEN":    maskEnvIfSet("ADMIN_AUTH_TOKEN"),
			"REDIS_ADDR":          getEnvStatus("REDIS_ADDR"),
			"REDIS_PASSWORD":      maskEnvIfSet("REDIS_PASSWORD"),
			"REDIS_DB":            getEnvStatus("REDIS_DB"),
			"REQUESTS_PER_SECOND": getEnvStatus("REQUESTS_PER_SECOND"),
			"BURST_SIZE":          getEnvStatus("BURST_SIZE"),
			"BLOCK_DURATION":      getEnvStatus("BLOCK_DURATION"),
			"MAX_BLOCK_DURATION":  getEnvStatus("MAX_BLOCK_DURATION"),
			"GLOBAL_RATE_LIMIT":   getEnvStatus("GLOBAL_RATE_LIMIT"),
			"GLOBAL_BURST":        getEnvStatus("GLOBAL_BURST"),
			"CORS_ENABLED":        getEnvStatus("CORS_ENABLED"),
			"BOT_DETECTION_LEVEL": getEnvStatus("BOT_DETECTION_LEVEL"),
			"TLS_CERT_FILE":       getEnvStatus("TLS_CERT_FILE"),
			"TLS_KEY_FILE":        getEnvStatus("TLS_KEY_FILE"),
			"LOG_LEVEL":           getEnvStatus("LOG_LEVEL"),
			"REGISTRY_FILE":       getEnvStatus("REGISTRY_FILE"),
			"EVENTS_DIR":          getEnvStatus("EVENTS_DIR"),
		}

		filesWithStatus := make([]string, 0, len(loadedConfigFiles))
		for _, f := range loadedConfigFiles {
			status := "✓ loaded"
			if !fileExists(f) {
				status = "✗ not found"
			}
			absPath, _ := filepath.Abs(f)
			filesWithStatus = append(filesWithStatus, fmt.Sprintf("%s [%s]", absPath, status))
		}

		apis, _ := registry.ExportAll(context.Background())

		info := ConfigInfo{
			Environment:     envCfg.Environment,
			ConfigFiles:     filesWithStatus,
			ConfigValues:    configValues,
			EnvironmentVars: envVars,
			StorageType:     envCfg.Storage.Type,
			RegistryType:    "file",
			RegistryFile:    registryFile,
			BackendType:     fmt.Sprintf("%d", cfg.BackendType),
			AdminAPIEnabled: cfg.AdminAuthToken != "",
			ServerPort:      cfg.Server.Port,
		}

		if cfg.RedisAddr != "" {
			info.RedisAddr = cfg.RedisAddr
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"info":       info,
			"apis_count": len(apis),
		})
	})

	r.Get("/debug/env", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		allEnv := make(map[string]string)
		for _, e := range os.Environ() {
			pair := splitEnv(e)
			if len(pair) == 2 {
				if containsSensitive(pair[0]) {
					allEnv[pair[0]] = "********"
				} else {
					allEnv[pair[0]] = pair[1]
				}
			}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"count":    len(allEnv),
			"env_vars": allEnv,
		})
	})

	adminRouter := chi.NewRouter()

	adminRouter.Mount("/", routing.AdminRouter(limiter))

	// Pass eventStore to the handler
	adminAPIHandler := routing.NewAdminAPIHandler(registry, limiter, envCfg.ServiceTemplates, eventStore)
	adminRouter.Mount("/apis", adminAPIHandler.Routes())

	metricsHandler := routing.NewMetricsHandler(metricsCollector)
	adminRouter.Mount("/metrics", metricsHandler.Routes())

	r.Mount("/admin", adminRouter)

	r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		path := r.URL.Path
		if strings.HasPrefix(path, "/admin") ||
			strings.HasPrefix(path, "/health") ||
			strings.HasPrefix(path, "/config") ||
			strings.HasPrefix(path, "/debug") {
			http.NotFound(w, r)
			return
		}

		rule, err := registry.GetCompiledRule(r.Context(), path)
		if err != nil || rule.IsExcluded {
			http.Error(w, `{"error": "no_upstream", "message": "No upstream configured for this path"}`, http.StatusNotFound)
			return
		}

		requestMethod := strings.ToUpper(r.Method)
		if rule.Method != "*" && rule.Method != "" && rule.Method != requestMethod {
			logInfo("Method not allowed", map[string]interface{}{
				"path":            path,
				"expected_method": rule.Method,
				"received_method": requestMethod,
				"endpoint_id":     rule.EndpointID,
			})
			http.Error(w, `{"error": "method_not_allowed", "message": "Method `+requestMethod+` not allowed for this endpoint. Expected: `+rule.Method+`"}`, http.StatusMethodNotAllowed)
			return
		}

		api, err := registry.GetAPI(r.Context(), rule.APIID)
		if err != nil {
			http.Error(w, `{"error": "api_config_error", "message": "API configuration error"}`, http.StatusInternalServerError)
			return
		}

		if api.UpstreamURL == "" {
			http.Error(w, `{"error": "no_upstream_url", "message": "No upstream URL configured for this API"}`, http.StatusBadGateway)
			return
		}

		upstream, err := url.Parse(api.UpstreamURL)
		if err != nil {
			http.Error(w, `{"error": "invalid_upstream", "message": "Invalid upstream URL"}`, http.StatusInternalServerError)
			return
		}

		var bodyBytes []byte
		if r.Body != nil {
			bodyBytes, _ = io.ReadAll(r.Body)
			r.Body.Close()
		}

		proxy := httputil.NewSingleHostReverseProxy(upstream)

		transport := &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		}
		proxy.Transport = transport

		rw := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)

			if len(bodyBytes) > 0 {
				req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
				req.ContentLength = int64(len(bodyBytes))
				if ct := r.Header.Get("Content-Type"); ct != "" {
					req.Header.Set("Content-Type", ct)
				}
			}

			req.URL.Path = path

			req.Host = upstream.Host

			req.Header.Set("X-Forwarded-Host", r.Host)
			req.Header.Set("X-Forwarded-Proto", "http")
			if clientIP := r.Header.Get("X-Real-Ip"); clientIP != "" {
				req.Header.Set("X-Forwarded-For", clientIP)
			}

			if api.Metadata != nil {
				if region := api.Metadata["region"]; region != "" {
					req.Header.Set("X-API-Region", region)
				}
				if version := api.Metadata["version"]; version != "" {
					req.Header.Set("X-API-Version", version)
				}
			}

			req.Header.Set("X-API-ID", api.ID)
			req.Header.Set("X-Endpoint-ID", rule.EndpointID)
		}

		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			logInfo("Proxy error", map[string]interface{}{
				"upstream": api.UpstreamURL,
				"path":     path,
				"error":    err.Error(),
			})
			http.Error(w, `{"error": "bad_gateway", "message": "Failed to connect to upstream"}`, http.StatusBadGateway)
		}

		logInfo("Proxying request", map[string]interface{}{
			"path":        path,
			"method":      requestMethod,
			"upstream":    api.UpstreamURL,
			"api_id":      api.ID,
			"endpoint_id": rule.EndpointID,
		})

		proxy.ServeHTTP(rw, r)

		responseTime := time.Since(startTime)
		ip := extractor.Extract(r)
		ip = helper.NormalizeIP(ip, cfg.EnableIPv6SubnetBlock, cfg.IPv6SubnetSize)

		allowed := rw.statusCode != http.StatusTooManyRequests && rw.statusCode != http.StatusForbidden
		rateLimited := rw.statusCode == http.StatusTooManyRequests
		blocked := rw.statusCode == http.StatusForbidden

		metricsCollector.Record(metrics.RecordRequest{
			APIID:        rule.APIID,
			EndpointID:   rule.EndpointID,
			Path:         path,
			Method:       r.Method,
			IP:           ip,
			StatusCode:   rw.statusCode,
			Allowed:      allowed,
			Blocked:      blocked,
			RateLimited:  rateLimited,
			ResponseTime: responseTime,
			Timestamp:    time.Now(),
		})
	})

	port := helper.GetEnv("PORT", cfg.Server.Port)
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  envCfg.Server.ReadTimeout,
		WriteTimeout: envCfg.Server.WriteTimeout,
		IdleTimeout:  envCfg.Server.IdleTimeout,
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		logInfo("Shutting down gracefully...", nil)

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(),
			envCfg.Shutdown.Timeout)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logInfo("Shutdown error", map[string]interface{}{"error": err})
		}

		if redisClient != nil {
			if err := redisClient.Close(); err != nil {
				logInfo("Redis close error", map[string]interface{}{"error": err})
			}
		}

		if closer, ok := registry.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				logInfo("Registry close error", map[string]interface{}{"error": err})
			}
		}

		if err := metricsCollector.Close(); err != nil {
			logInfo("Metrics close error", map[string]interface{}{"error": err})
		}

		if err := eventStore.Close(); err != nil {
			logInfo("Event store close error", map[string]interface{}{"error": err})
		}

		cancel()
	}()

	logInfo("Rate limiter service started (Reverse Proxy Mode)", map[string]interface{}{
		"port":             port,
		"environment":      envCfg.Environment,
		"admin_api":        "/admin/apis",
		"metrics_api":      "/admin/metrics",
		"events_api":       "/admin/apis/events",
		"config_endpoint":  "/config",
		"proxy_mode":       "simplified",
		"registry_enabled": true,
		"registry_file":    registryFile,
		"metrics_dir":      metricsDir,
		"events_dir":       eventsDir,
	})

	fmt.Println("=== Server ready, waiting for requests ===")
	fmt.Println("=== Register API: POST /admin/apis ===")
	fmt.Println("=== Events History: GET /admin/apis/events/history ===")
	fmt.Println("=== Events Stream: GET /admin/apis/events ===")
	fmt.Println("=== Metrics available at: /admin/metrics/{api_id} ===")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}

type RegistryAdapter struct {
	registry base.RuleRegistry
}

func (a *RegistryAdapter) GetAPI(ctx context.Context, apiID string) (*metrics.RegistryAPIInfo, error) {
	api, err := a.registry.GetAPI(ctx, apiID)
	if err != nil {
		return nil, err
	}

	endpoints := make([]metrics.EndpointInfo, len(api.Endpoints))
	for i, ep := range api.Endpoints {
		endpoints[i] = metrics.EndpointInfo{
			ID:     ep.ID,
			Path:   ep.Path,
			Method: ep.Method,
		}
	}

	return &metrics.RegistryAPIInfo{
		ID:          api.ID,
		BasePath:    api.BasePath,
		UpstreamURL: api.UpstreamURL,
		Endpoints:   endpoints,
	}, nil
}

func (a *RegistryAdapter) ExportAll(ctx context.Context) ([]metrics.RegistryAPIInfo, error) {
	apis, err := a.registry.ExportAll(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]metrics.RegistryAPIInfo, len(apis))
	for i, api := range apis {
		endpoints := make([]metrics.EndpointInfo, len(api.Endpoints))
		for j, ep := range api.Endpoints {
			endpoints[j] = metrics.EndpointInfo{
				ID:     ep.ID,
				Path:   ep.Path,
				Method: ep.Method,
			}
		}

		result[i] = metrics.RegistryAPIInfo{
			ID:          api.ID,
			BasePath:    api.BasePath,
			UpstreamURL: api.UpstreamURL,
			Endpoints:   endpoints,
		}
	}

	return result, nil
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func splitEnv(env string) []string {
	for i := 0; i < len(env); i++ {
		if env[i] == '=' {
			return []string{env[:i], env[i+1:]}
		}
	}
	return []string{env}
}

func containsSensitive(key string) bool {
	sensitive := []string{"PASSWORD", "SECRET", "TOKEN", "KEY", "AUTH"}
	upperKey := strings.ToUpper(key)
	for _, s := range sensitive {
		if strings.Contains(upperKey, s) {
			return true
		}
	}
	return false
}

func maskIfSet(value string) string {
	if value == "" {
		return "(not set)"
	}
	return "******** (set)"
}

func getEnvStatus(key string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return "(not set)"
}

func maskEnvIfSet(key string) string {
	if val := os.Getenv(key); val != "" {
		return "******** (from env)"
	}
	return "(not set)"
}
