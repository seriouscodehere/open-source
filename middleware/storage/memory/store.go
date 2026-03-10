package memory

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryBackend implements base.Backend interface using in-memory storage
type Backend struct {
	cache        *lruCache
	useSliding   bool
	windowSize   time.Duration
	rps          float64
	defaultBurst int
}

// NewBackend creates a new memory-backed rate limiter backend
func NewBackend(maxIPs int, useSliding bool, windowSize time.Duration, rps float64, burst int) *Backend {
	return &Backend{
		cache:        newLRUCache(maxIPs),
		useSliding:   useSliding,
		windowSize:   windowSize,
		rps:          rps,
		defaultBurst: burst,
	}
}

func (m *Backend) Allow(ctx context.Context, key string, rps float64, burst int, windowSize time.Duration) (bool, time.Duration, error) {
	entry, exists := m.cache.Get(key)
	if !exists {
		entry = newIPEntry(rps, burst, false, 0)
		m.cache.Add(key, entry)
	}
	entry.touch()

	if entry.isBlocked() {
		retryAfter := time.Until(entry.blockedUntil)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter, nil
	}

	if !entry.bucket.Allow(rps, burst) {
		return false, 0, nil
	}
	return true, 0, nil
}

func (m *Backend) AllowSlidingWindow(ctx context.Context, key string, limit int, windowSize time.Duration) (bool, time.Duration, error) {
	entry, exists := m.cache.Get(key)
	if !exists {
		entry = newIPEntry(m.rps, m.defaultBurst, true, windowSize)
		m.cache.Add(key, entry)
	}
	entry.touch()

	if entry.isBlocked() {
		retryAfter := time.Until(entry.blockedUntil)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter, nil
	}

	if entry.slidingWindow == nil {
		entry.slidingWindow = NewSlidingWindow(windowSize, limit)
	}

	if !entry.slidingWindow.Allow() {
		return false, windowSize, nil
	}
	return true, 0, nil
}

func (m *Backend) Block(ctx context.Context, key string, duration time.Duration) error {
	entry, exists := m.cache.Get(key)
	if !exists {
		entry = newIPEntry(m.rps, m.defaultBurst, m.useSliding, m.windowSize)
		m.cache.Add(key, entry)
	}
	if duration < 0 {
		entry.blockedUntil = time.Time{}
		atomic.StoreInt32(&entry.violationCount, 0)
	} else {
		entry.blockedUntil = time.Now().Add(duration)
	}
	return nil
}

func (m *Backend) IsBlocked(ctx context.Context, key string) (bool, time.Time, error) {
	entry, exists := m.cache.Get(key)
	if !exists {
		return false, time.Time{}, nil
	}
	return entry.isBlocked(), entry.blockedUntil, nil
}

func (m *Backend) GetTokenBucket(ctx context.Context, key string) (float64, time.Time, error) {
	entry, exists := m.cache.Get(key)
	if !exists {
		return 0, time.Time{}, nil
	}
	return entry.bucket.TokensRemaining(), entry.bucket.LastUpdate, nil
}

func (m *Backend) SetTokenBucket(ctx context.Context, key string, tokens float64, lastUpdate time.Time) error {
	entry, exists := m.cache.Get(key)
	if !exists {
		entry = newIPEntry(m.rps, m.defaultBurst, m.useSliding, m.windowSize)
		m.cache.Add(key, entry)
	}
	entry.bucket.Tokens = tokens
	entry.bucket.LastUpdate = lastUpdate
	return nil
}

func (m *Backend) GetSlidingWindowCount(ctx context.Context, key string, windowSize time.Duration) (int, error) {
	entry, exists := m.cache.Get(key)
	if !exists || entry.slidingWindow == nil {
		return 0, nil
	}
	return entry.slidingWindow.Count(), nil
}

func (m *Backend) GetViolationCount(ctx context.Context, key string) (int32, error) {
	entry, exists := m.cache.Get(key)
	if !exists {
		return 0, nil
	}
	return atomic.LoadInt32(&entry.violationCount), nil
}

func (m *Backend) IncrementViolation(ctx context.Context, key string) (int32, error) {
	entry, exists := m.cache.Get(key)
	if !exists {
		entry = newIPEntry(m.rps, m.defaultBurst, m.useSliding, m.windowSize)
		m.cache.Add(key, entry)
	}
	atomic.StoreInt64(&entry.lastViolation, time.Now().Unix())
	return atomic.AddInt32(&entry.violationCount, 1), nil
}

func (m *Backend) DecayViolations(ctx context.Context, keys []string, decayInterval time.Duration) error {
	now := time.Now().Unix()
	decaySecs := int64(decayInterval.Seconds())

	for _, key := range keys {
		entry, exists := m.cache.Get(key)
		if !exists {
			continue
		}
		lastViolation := atomic.LoadInt64(&entry.lastViolation)
		hoursSince := (now - lastViolation) / decaySecs
		if hoursSince > 0 {
			current := atomic.LoadInt32(&entry.violationCount)
			decay := int32(hoursSince / 2)
			if decay > 0 {
				newCount := current - decay
				if newCount < 0 {
					newCount = 0
				}
				atomic.StoreInt32(&entry.violationCount, newCount)
			}
		}
	}
	return nil
}

func (m *Backend) Close() error {
	return nil
}

func (m *Backend) Cache() *lruCache {
	return m.cache
}

// ipEntry represents a single IP's rate limiting state
type ipEntry struct {
	bucket         *TokenBucket
	slidingWindow  *SlidingWindow
	blockedUntil   time.Time
	violationCount int32
	lastSeen       int64
	firstSeen      time.Time
	requestCount   int64
	lastViolation  int64
	prev, next     *ipEntry
}

func newIPEntry(rps float64, burst int, useSlidingWindow bool, windowSize time.Duration) *ipEntry {
	now := time.Now()
	entry := &ipEntry{
		bucket: &TokenBucket{
			Tokens:     float64(burst),
			LastUpdate: now,
		},
		lastSeen:      now.Unix(),
		firstSeen:     now,
		lastViolation: now.Unix(),
	}
	if useSlidingWindow {
		limit := int(rps * windowSize.Seconds())
		if limit < 1 {
			limit = 1
		}
		if limit < burst {
			limit = burst
		}
		entry.slidingWindow = NewSlidingWindow(windowSize, limit)
	}
	return entry
}

func (e *ipEntry) isBlocked() bool {
	return time.Now().Before(e.blockedUntil)
}

func (e *ipEntry) touch() {
	atomic.StoreInt64(&e.lastSeen, time.Now().Unix())
	atomic.AddInt64(&e.requestCount, 1)
}

func (e *ipEntry) shouldCleanup(maxAge time.Duration) bool {
	lastSeen := time.Unix(atomic.LoadInt64(&e.lastSeen), 0)
	return time.Since(lastSeen) > maxAge && !e.isBlocked()
}

// TokenBucket implements token bucket algorithm
type TokenBucket struct {
	Tokens     float64
	LastUpdate time.Time
	mu         sync.Mutex
}

func (tb *TokenBucket) Allow(rps float64, burst int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(tb.LastUpdate).Seconds()
	tb.Tokens = math.Min(float64(burst), tb.Tokens+elapsed*rps)
	tb.LastUpdate = now
	if tb.Tokens >= 1.0 {
		tb.Tokens--
		return true
	}
	return false
}

func (tb *TokenBucket) TokensRemaining() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.Tokens
}

// SlidingWindow implements sliding window counter
type SlidingWindow struct {
	WindowSize time.Duration
	Limit      int
	Requests   []time.Time
	mu         sync.Mutex
	lastAlloc  int
}

func NewSlidingWindow(windowSize time.Duration, limit int) *SlidingWindow {
	return &SlidingWindow{
		WindowSize: windowSize,
		Limit:      limit,
		Requests:   make([]time.Time, 0, min(limit*2, 1000)),
		lastAlloc:  limit * 2,
	}
}

func (sw *SlidingWindow) Allow() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-sw.WindowSize)

	validIdx := sw.findFirstValid(cutoff)

	if validIdx > 0 {
		sw.Requests = sw.Requests[validIdx:]
	}

	if len(sw.Requests) >= sw.Limit {
		return false
	}

	sw.Requests = append(sw.Requests, now)

	if cap(sw.Requests) > len(sw.Requests)*4 && cap(sw.Requests) > 1000 {
		newSlice := make([]time.Time, len(sw.Requests), len(sw.Requests)*2)
		copy(newSlice, sw.Requests)
		sw.Requests = newSlice
		sw.lastAlloc = cap(newSlice)
	}

	return true
}

func (sw *SlidingWindow) findFirstValid(cutoff time.Time) int {
	if len(sw.Requests) == 0 || sw.Requests[len(sw.Requests)-1].After(cutoff) {
		if len(sw.Requests) > 0 && sw.Requests[0].After(cutoff) {
			return 0
		}
	}

	left, right := 0, len(sw.Requests)
	for left < right {
		mid := (left + right) / 2
		if sw.Requests[mid].Before(cutoff) || sw.Requests[mid].Equal(cutoff) {
			left = mid + 1
		} else {
			right = mid
		}
	}
	return left
}

func (sw *SlidingWindow) Count() int {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-sw.WindowSize)
	validIdx := sw.findFirstValid(cutoff)
	return len(sw.Requests) - validIdx
}

// lruCache implements LRU eviction for IP entries
type lruCache struct {
	capacity int
	items    map[string]*ipEntry
	head     *ipEntry
	tail     *ipEntry
	mu       sync.Mutex
}

func newLRUCache(capacity int) *lruCache {
	cache := &lruCache{
		capacity: capacity,
		items:    make(map[string]*ipEntry),
	}
	cache.head = &ipEntry{}
	cache.tail = &ipEntry{}
	cache.head.next = cache.tail
	cache.tail.prev = cache.head
	return cache
}

func (c *lruCache) Get(key string) (*ipEntry, bool) {
	c.mu.Lock()
	entry, exists := c.items[key]
	if exists {
		c.moveToFront(entry)
	}
	c.mu.Unlock()
	if exists {
		return entry, true
	}
	return nil, false
}

func (c *lruCache) Add(key string, entry *ipEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, exists := c.items[key]; exists {
		c.removeFromList(existing)
	}
	c.items[key] = entry
	c.addToFront(entry)
	if len(c.items) > c.capacity {
		c.evict()
	}
}

func (c *lruCache) Remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, exists := c.items[key]; exists {
		c.removeFromList(entry)
		delete(c.items, key)
	}
}

func (c *lruCache) addToFront(e *ipEntry) {
	e.next = c.head.next
	e.prev = c.head
	c.head.next.prev = e
	c.head.next = e
}

func (c *lruCache) removeFromList(e *ipEntry) {
	e.prev.next = e.next
	e.next.prev = e.prev
}

func (c *lruCache) moveToFront(e *ipEntry) {
	c.removeFromList(e)
	c.addToFront(e)
}

func (c *lruCache) evict() {
	back := c.tail.prev
	if back == c.head {
		return
	}
	c.removeFromList(back)
	for k, v := range c.items {
		if v == back {
			delete(c.items, k)
			return
		}
	}
}

func (c *lruCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *lruCache) GetAll() map[string]*ipEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]*ipEntry, len(c.items))
	for k, v := range c.items {
		result[k] = v
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
