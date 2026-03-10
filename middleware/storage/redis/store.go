// storage/redis/store.go
package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/seriouscodehere/open-source/middleware/base"
)

// Backend implements base.Backend interface using Redis
type Backend struct {
	client             *redis.Client
	allowScript        *redis.Script
	allowSlidingScript *redis.Script
	blockScript        *redis.Script
	decayScript        *redis.Script
	circuitBreaker     *base.CircuitBreaker
	keyPrefix          string
	defaultWindowSecs  int64
}

// NewBackend creates a new Redis-backed rate limiter backend
func NewBackend(addr, password string, db int, cb *base.CircuitBreaker, keyPrefix string, defaultWindow time.Duration) (*Backend, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     100,
		MinIdleConns: 10,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return initBackend(client, cb, keyPrefix, defaultWindow)
}

// NewBackendWithClient creates backend using existing Redis client
func NewBackendWithClient(client *redis.Client, cb *base.CircuitBreaker, keyPrefix string, defaultWindow time.Duration) (*Backend, error) {
	return initBackend(client, cb, keyPrefix, defaultWindow)
}

// initBackend initializes backend with scripts
func initBackend(client *redis.Client, cb *base.CircuitBreaker, keyPrefix string, defaultWindow time.Duration) (*Backend, error) {
	allowScript := redis.NewScript(`
		local key = KEYS[1]
		local rps = tonumber(ARGV[1])
		local burst = tonumber(ARGV[2])
		local now = tonumber(ARGV[3])
		local window = tonumber(ARGV[4])
		
		if not rps or not burst or not now or not window then
			return redis.error_reply("invalid arguments")
		end
		
		local data = redis.call('HMGET', key, 'tokens', 'last_update', 'blocked_until')
		local tokens = tonumber(data[1])
		local lastUpdate = tonumber(data[2])
		local blockedUntil = tonumber(data[3])
		
		if blockedUntil and now < blockedUntil then
			return {-1, blockedUntil - now}
		end
		
		if not tokens then
			tokens = burst
			lastUpdate = now
		end
		
		local elapsed = now - lastUpdate
		tokens = math.min(burst, tokens + elapsed * rps)
		
		if tokens >= 1 then
			tokens = tokens - 1
			redis.call('HMSET', key, 'tokens', tokens, 'last_update', now)
			redis.call('EXPIRE', key, window)
			return {1, math.floor(tokens)}
		else
			redis.call('HMSET', key, 'tokens', tokens, 'last_update', now)
			redis.call('EXPIRE', key, window)
			return {0, 0}
		end
	`)

	allowSlidingScript := redis.NewScript(`
		local key = KEYS[1]
		local limit = tonumber(ARGV[1])
		local window = tonumber(ARGV[2])
		local now = tonumber(ARGV[3])
		local windowKey = key .. ":sw"
		
		if not limit or not window or not now then
			return redis.error_reply("invalid arguments")
		end
		
		local blockedUntil = tonumber(redis.call('HGET', key, 'blocked_until'))
		if blockedUntil and now < blockedUntil then
			return {-1, blockedUntil - now}
		end
		
		local cutoff = now - window
		redis.call('ZREMRANGEBYSCORE', windowKey, 0, cutoff)
		
		local current = redis.call('ZCARD', windowKey)
		
		if current < limit then
			redis.call('ZADD', windowKey, now, now .. ":" .. redis.call('INCR', key .. ":seq"))
			redis.call('EXPIRE', windowKey, window)
			return {1, limit - current - 1}
		else
			local oldest = redis.call('ZRANGE', windowKey, 0, 0, 'WITHSCORES')
			local retryAfter = math.ceil((oldest[2] + window - now) / 1000)
			return {0, retryAfter}
		end
	`)

	blockScript := redis.NewScript(`
		local key = KEYS[1]
		local duration = tonumber(ARGV[1])
		local now = tonumber(ARGV[2])
		local window = tonumber(ARGV[3])
		
		if not duration or not now or not window then
			return redis.error_reply("invalid arguments")
		end
		
		if duration < 0 then
			redis.call('HDEL', key, 'blocked_until', 'violations', 'last_violation')
			redis.call('DEL', key .. ":sw")
			return {1}
		else
			local blockedUntil = now + duration
			redis.call('HMSET', key, 'blocked_until', blockedUntil)
			redis.call('EXPIRE', key, math.max(duration, window))
			return {blockedUntil}
		end
	`)

	decayScript := redis.NewScript(`
		local key = KEYS[1]
		local decayInterval = tonumber(ARGV[1])
		local now = tonumber(ARGV[2])
		
		if not decayInterval or not now then
			return 0
		end
		
		local lastVio = tonumber(redis.call('HGET', key, 'last_violation'))
		if not lastVio then
			return 0
		end
		
		local hoursSince = math.floor((now - lastVio) / decayInterval)
		if hoursSince > 0 then
			local current = tonumber(redis.call('HGET', key, 'violations') or 0)
			local decay = math.floor(hoursSince / 2)
			local newCount = math.max(0, current - decay)
			redis.call('HSET', key, 'violations', newCount)
			redis.call('HSET', key, 'last_violation', now)
			return newCount
		end
		return tonumber(redis.call('HGET', key, 'violations') or 0)
	`)

	return &Backend{
		client:             client,
		allowScript:        allowScript,
		allowSlidingScript: allowSlidingScript,
		blockScript:        blockScript,
		decayScript:        decayScript,
		circuitBreaker:     cb,
		keyPrefix:          keyPrefix,
		defaultWindowSecs:  int64(defaultWindow.Seconds()),
	}, nil
}

// Client returns the underlying Redis client
func (r *Backend) Client() *redis.Client {
	return r.client
}

// Remaining methods unchanged...
func (r *Backend) Allow(ctx context.Context, key string, rps float64, burst int, windowSize time.Duration) (bool, time.Duration, error) {
	if !r.circuitBreaker.Allow() {
		return false, 0, base.ErrBackendUnavailable
	}
	defer r.circuitBreaker.ReleaseProbe()

	now := float64(time.Now().Unix())
	window := windowSize.Seconds()
	if window <= 0 {
		window = float64(r.defaultWindowSecs)
	}

	result, err := r.allowScript.Run(ctx, r.client, []string{r.keyPrefix + key},
		rps, burst, now, window).Result()

	if err != nil {
		r.circuitBreaker.RecordFailure()
		return false, 0, err
	}

	r.circuitBreaker.RecordSuccess()

	values, ok := result.([]interface{})
	if !ok || len(values) < 2 {
		return false, 0, errors.New("invalid script response")
	}

	status := toInt64(values[0])
	retryAfter := toInt64(values[1])

	if status == -1 {
		return false, time.Duration(retryAfter) * time.Second, nil
	}
	return status == 1, 0, nil
}

func (r *Backend) AllowSlidingWindow(ctx context.Context, key string, limit int, windowSize time.Duration) (bool, time.Duration, error) {
	if !r.circuitBreaker.Allow() {
		return false, 0, base.ErrBackendUnavailable
	}
	defer r.circuitBreaker.ReleaseProbe()

	now := float64(time.Now().UnixMilli())
	window := float64(windowSize.Milliseconds())

	result, err := r.allowSlidingScript.Run(ctx, r.client, []string{r.keyPrefix + key},
		limit, window, now).Result()

	if err != nil {
		r.circuitBreaker.RecordFailure()
		return false, 0, err
	}

	r.circuitBreaker.RecordSuccess()

	values, ok := result.([]interface{})
	if !ok || len(values) < 2 {
		return false, 0, errors.New("invalid script response")
	}

	status := toInt64(values[0])
	retryAfter := toInt64(values[1])

	if status == -1 {
		return false, time.Duration(retryAfter) * time.Second, nil
	}
	return status == 1, time.Duration(retryAfter) * time.Second, nil
}

func (r *Backend) Block(ctx context.Context, key string, duration time.Duration) error {
	if !r.circuitBreaker.Allow() {
		return base.ErrBackendUnavailable
	}
	defer r.circuitBreaker.ReleaseProbe()

	now := float64(time.Now().Unix())
	window := float64(r.defaultWindowSecs)

	_, err := r.blockScript.Run(ctx, r.client, []string{r.keyPrefix + key},
		duration.Seconds(), now, window).Result()

	if err != nil {
		r.circuitBreaker.RecordFailure()
		return err
	}
	r.circuitBreaker.RecordSuccess()
	return nil
}

func (r *Backend) IsBlocked(ctx context.Context, key string) (bool, time.Time, error) {
	if !r.circuitBreaker.Allow() {
		return true, time.Now().Add(time.Minute), base.ErrBackendUnavailable
	}
	defer r.circuitBreaker.ReleaseProbe()

	val, err := r.client.HGet(ctx, r.keyPrefix+key, "blocked_until").Result()
	if err == redis.Nil {
		return false, time.Time{}, nil
	}
	if err != nil {
		r.circuitBreaker.RecordFailure()
		return true, time.Time{}, err
	}
	r.circuitBreaker.RecordSuccess()

	blockedUnix, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return true, time.Time{}, fmt.Errorf("invalid blocked timestamp: %w", err)
	}
	blockedUntil := time.Unix(blockedUnix, 0)
	return time.Now().Before(blockedUntil), blockedUntil, nil
}

func (r *Backend) GetTokenBucket(ctx context.Context, key string) (float64, time.Time, error) {
	if !r.circuitBreaker.Allow() {
		return 0, time.Time{}, base.ErrBackendUnavailable
	}
	defer r.circuitBreaker.ReleaseProbe()

	data, err := r.client.HMGet(ctx, r.keyPrefix+key, "tokens", "last_update").Result()
	if err != nil {
		r.circuitBreaker.RecordFailure()
		return 0, time.Time{}, err
	}
	r.circuitBreaker.RecordSuccess()

	if data[0] == nil || data[1] == nil {
		return 0, time.Time{}, nil
	}

	tokens := toFloat64(data[0])
	lastUpdateUnix := toInt64(data[1])

	return tokens, time.Unix(lastUpdateUnix, 0), nil
}

func (r *Backend) SetTokenBucket(ctx context.Context, key string, tokens float64, lastUpdate time.Time) error {
	if !r.circuitBreaker.Allow() {
		return base.ErrBackendUnavailable
	}
	defer r.circuitBreaker.ReleaseProbe()

	err := r.client.HMSet(ctx, r.keyPrefix+key, map[string]interface{}{
		"tokens":      tokens,
		"last_update": lastUpdate.Unix(),
	}).Err()

	if err != nil {
		r.circuitBreaker.RecordFailure()
	}
	r.circuitBreaker.RecordSuccess()
	return err
}

func (r *Backend) GetSlidingWindowCount(ctx context.Context, key string, windowSize time.Duration) (int, error) {
	if !r.circuitBreaker.Allow() {
		return 0, base.ErrBackendUnavailable
	}
	defer r.circuitBreaker.ReleaseProbe()

	windowKey := r.keyPrefix + key + ":sw"
	cutoff := time.Now().Add(-windowSize).UnixMilli()

	pipe := r.client.Pipeline()
	pipe.ZRemRangeByScore(ctx, windowKey, "0", strconv.FormatInt(cutoff, 10))
	countCmd := pipe.ZCard(ctx, windowKey)
	_, err := pipe.Exec(ctx)

	if err != nil {
		r.circuitBreaker.RecordFailure()
		return 0, err
	}
	r.circuitBreaker.RecordSuccess()

	return int(countCmd.Val()), nil
}

func (r *Backend) GetViolationCount(ctx context.Context, key string) (int32, error) {
	if !r.circuitBreaker.Allow() {
		return 0, nil
	}
	defer r.circuitBreaker.ReleaseProbe()

	val, err := r.client.HGet(ctx, r.keyPrefix+key, "violations").Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		r.circuitBreaker.RecordFailure()
		return 0, err
	}
	r.circuitBreaker.RecordSuccess()

	return int32(toInt64(val)), nil
}

func (r *Backend) IncrementViolation(ctx context.Context, key string) (int32, error) {
	if !r.circuitBreaker.Allow() {
		return 0, base.ErrBackendUnavailable
	}
	defer r.circuitBreaker.ReleaseProbe()

	pipe := r.client.Pipeline()
	incrCmd := pipe.HIncrBy(ctx, r.keyPrefix+key, "violations", 1)
	pipe.HSet(ctx, r.keyPrefix+key, "last_violation", time.Now().Unix())
	_, err := pipe.Exec(ctx)

	if err != nil {
		r.circuitBreaker.RecordFailure()
		return 0, err
	}
	r.circuitBreaker.RecordSuccess()

	return int32(incrCmd.Val()), nil
}

func (r *Backend) DecayViolations(ctx context.Context, keys []string, decayInterval time.Duration) error {
	if !r.circuitBreaker.Allow() {
		return nil
	}
	defer r.circuitBreaker.ReleaseProbe()

	now := float64(time.Now().Unix())
	decaySecs := decayInterval.Seconds()

	pipe := r.client.Pipeline()
	for _, key := range keys {
		r.decayScript.Run(ctx, r.client, []string{r.keyPrefix + key}, decaySecs, now)
	}
	_, err := pipe.Exec(ctx)

	if err != nil {
		r.circuitBreaker.RecordFailure()
	}
	r.circuitBreaker.RecordSuccess()
	return err
}

func (r *Backend) Close() error {
	return r.client.Close()
}

func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case float64:
		return int64(val)
	case string:
		i, _ := strconv.ParseInt(val, 10, 64)
		return i
	default:
		return 0
	}
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int64:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}
