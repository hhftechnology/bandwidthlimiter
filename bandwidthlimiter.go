// Package bandwidthlimiter implements a Traefik middleware plugin for bandwidth limiting
package bandwidthlimiter

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config holds the plugin configuration
type Config struct {
	// Default bandwidth limit in bytes per second
	DefaultLimit int64 `json:"defaultLimit"`
	
	// Backend-specific limits: map[backend-address]limit
	BackendLimits map[string]int64 `json:"backendLimits,omitempty"`
	
	// Client IP-specific limits: map[client-ip]limit
	ClientLimits map[string]int64 `json:"clientLimits,omitempty"`
	
	// Burst size - how many bytes can be sent in a single burst
	BurstSize int64 `json:"burstSize,omitempty"`
	
	// Maximum age of unused buckets before cleanup (in seconds)
	// Default: 3600 (1 hour)
	BucketMaxAge int64 `json:"bucketMaxAge,omitempty"`
	
	// Cleanup interval in seconds
	// Default: 300 (5 minutes)
	CleanupInterval int64 `json:"cleanupInterval,omitempty"`
	
	// File path for persistent bucket storage
	// If empty, no file storage is used
	PersistenceFile string `json:"persistenceFile,omitempty"`
	
	// How often to save buckets to file (in seconds)
	// Default: 60 (1 minute)
	SaveInterval int64 `json:"saveInterval,omitempty"`
}

// CreateConfig creates the default plugin configuration
func CreateConfig() *Config {
	return &Config{
		DefaultLimit:    1024 * 1024, // 1 MB/s default
		BackendLimits:   make(map[string]int64),
		ClientLimits:    make(map[string]int64),
		BurstSize:       10 * 1024 * 1024, // 10 MB burst default
		BucketMaxAge:    3600,  // 1 hour
		CleanupInterval: 300,   // 5 minutes
		SaveInterval:    60,    // 1 minute
	}
}

// BandwidthLimiter implements the middleware
type BandwidthLimiter struct {
	next            http.Handler
	name            string
	config          *Config
	buckets         sync.Map         // map[string]*bucketWrapper
	cleanupTicker   *time.Ticker
	saveTicker      *time.Ticker
	shutdownChan    chan struct{}
	wg              sync.WaitGroup
}

// bucketWrapper wraps a TokenBucket with metadata for cleanup and persistence
type bucketWrapper struct {
	bucket   *TokenBucket
	lastUsed time.Time
	key      string // For easier identification
}

// TokenBucket implements the token bucket algorithm for rate limiting
type TokenBucket struct {
	tokens     int64
	limit      int64
	burstSize  int64
	lastRefill time.Time
	mutex      sync.Mutex
}

// bucketState represents the serializable state of a bucket
type bucketState struct {
	Key        string    `json:"key"`
	Tokens     int64     `json:"tokens"`
	Limit      int64     `json:"limit"`
	BurstSize  int64     `json:"burstSize"`
	LastRefill time.Time `json:"lastRefill"`
	LastUsed   time.Time `json:"lastUsed"`
}

// NewTokenBucket creates a new token bucket
func NewTokenBucket(limit, burstSize int64) *TokenBucket {
	return &TokenBucket{
		tokens:     burstSize,
		limit:      limit,
		burstSize:  burstSize,
		lastRefill: time.Now(),
	}
}

// Consume attempts to consume tokens from the bucket
func (tb *TokenBucket) Consume(tokens int64) bool {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	
	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)
	tokensToAdd := int64(elapsed.Seconds() * float64(tb.limit))
	tb.tokens = min(tb.tokens+tokensToAdd, tb.burstSize)
	tb.lastRefill = now
	
	// Check if we have enough tokens
	if tb.tokens >= tokens {
		tb.tokens -= tokens
		return true
	}
	
	// Not enough tokens, return false
	return false
}

// getState returns the serializable state of the bucket
func (tb *TokenBucket) getState() bucketState {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	
	return bucketState{
		Tokens:     tb.tokens,
		Limit:      tb.limit,
		BurstSize:  tb.burstSize,
		LastRefill: tb.lastRefill,
	}
}

// restoreFromState restores the bucket from a saved state
func (tb *TokenBucket) restoreFromState(state bucketState) {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	
	tb.tokens = state.Tokens
	tb.limit = state.Limit
	tb.burstSize = state.BurstSize
	tb.lastRefill = state.LastRefill
}

// min helper function
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// New creates a new BandwidthLimiter plugin
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config.DefaultLimit <= 0 {
		return nil, fmt.Errorf("defaultLimit must be greater than 0")
	}
	
	if config.BurstSize == 0 {
		config.BurstSize = config.DefaultLimit * 10 // Default burst is 10x the rate
	}
	
	if config.BucketMaxAge == 0 {
		config.BucketMaxAge = 3600 // 1 hour default
	}
	
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 300 // 5 minutes default
	}
	
	if config.SaveInterval == 0 {
		config.SaveInterval = 60 // 1 minute default
	}
	
	bl := &BandwidthLimiter{
		next:         next,
		name:         name,
		config:       config,
		shutdownChan: make(chan struct{}),
	}
	
	// Load persisted buckets if persistence is enabled
	if config.PersistenceFile != "" {
		if err := bl.loadBuckets(); err != nil {
			// Log the error but don't fail startup
			fmt.Printf("Warning: Failed to load persisted buckets: %v\n", err)
		}
	}
	
	// Start cleanup routine
	bl.cleanupTicker = time.NewTicker(time.Duration(config.CleanupInterval) * time.Second)
	bl.wg.Add(1)
	go bl.cleanupRoutine()
	
	// Start save routine if persistence is enabled
	if config.PersistenceFile != "" {
		bl.saveTicker = time.NewTicker(time.Duration(config.SaveInterval) * time.Second)
		bl.wg.Add(1)
		go bl.saveRoutine()
	}
	
	return bl, nil
}

// cleanupRoutine periodically removes unused buckets
func (bl *BandwidthLimiter) cleanupRoutine() {
	defer bl.wg.Done()
	
	for {
		select {
		case <-bl.cleanupTicker.C:
			bl.doCleanup()
		case <-bl.shutdownChan:
			return
		}
	}
}

// doCleanup removes buckets that haven't been used recently
func (bl *BandwidthLimiter) doCleanup() {
	now := time.Now()
	maxAge := time.Duration(bl.config.BucketMaxAge) * time.Second
	
	// Count buckets before cleanup
	beforeCount := 0
	bl.buckets.Range(func(key, value interface{}) bool {
		beforeCount++
		return true
	})
	
	// Remove old buckets
	bl.buckets.Range(func(key, value interface{}) bool {
		wrapper := value.(*bucketWrapper)
		if now.Sub(wrapper.lastUsed) > maxAge {
			bl.buckets.Delete(key)
		}
		return true
	})
	
	// Count buckets after cleanup
	afterCount := 0
	bl.buckets.Range(func(key, value interface{}) bool {
		afterCount++
		return true
	})
	
	removed := beforeCount - afterCount
	if removed > 0 {
		fmt.Printf("Cleanup removed %d unused buckets (kept %d active buckets)\n", removed, afterCount)
	}
}

// saveRoutine periodically saves buckets to file
func (bl *BandwidthLimiter) saveRoutine() {
	defer bl.wg.Done()
	
	for {
		select {
		case <-bl.saveTicker.C:
			if err := bl.saveBuckets(); err != nil {
				fmt.Printf("Error saving buckets: %v\n", err)
			}
		case <-bl.shutdownChan:
			// Save one final time on shutdown
			if err := bl.saveBuckets(); err != nil {
				fmt.Printf("Error saving buckets on shutdown: %v\n", err)
			}
			return
		}
	}
}

// saveBuckets saves all current buckets to the configured file
func (bl *BandwidthLimiter) saveBuckets() error {
	if bl.config.PersistenceFile == "" {
		return nil // Persistence disabled
	}
	
	var states []bucketState
	
	// Collect all bucket states
	bl.buckets.Range(func(key, value interface{}) bool {
		wrapper := value.(*bucketWrapper)
		state := wrapper.bucket.getState()
		state.Key = key.(string)
		state.LastUsed = wrapper.lastUsed
		states = append(states, state)
		return true
	})
	
	// Create directory if it doesn't exist
	dir := filepath.Dir(bl.config.PersistenceFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	
	// Write to temporary file first (atomic save)
	tempFile := bl.config.PersistenceFile + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer file.Close()
	
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ") // Pretty print for debugging
	if err := encoder.Encode(states); err != nil {
		return fmt.Errorf("failed to encode buckets: %w", err)
	}
	
	file.Close()
	
	// Atomic rename
	if err := os.Rename(tempFile, bl.config.PersistenceFile); err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}
	
	fmt.Printf("Saved %d buckets to %s\n", len(states), bl.config.PersistenceFile)
	return nil
}

// loadBuckets loads saved buckets from the configured file
func (bl *BandwidthLimiter) loadBuckets() error {
	if bl.config.PersistenceFile == "" {
		return nil // Persistence disabled
	}
	
	file, err := os.Open(bl.config.PersistenceFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist yet, that's OK
		}
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()
	
	var states []bucketState
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&states); err != nil {
		return fmt.Errorf("failed to decode buckets: %w", err)
	}
	
	// Restore buckets
	loaded := 0
	for _, state := range states {
		bucket := NewTokenBucket(state.Limit, state.BurstSize)
		bucket.restoreFromState(state)
		
		wrapper := &bucketWrapper{
			bucket:   bucket,
			lastUsed: state.LastUsed,
			key:      state.Key,
		}
		
		bl.buckets.Store(state.Key, wrapper)
		loaded++
	}
	
	fmt.Printf("Loaded %d buckets from %s\n", loaded, bl.config.PersistenceFile)
	return nil
}

// Shutdown gracefully shuts down the bandwidth limiter
func (bl *BandwidthLimiter) Shutdown() {
	close(bl.shutdownChan)
	
	if bl.cleanupTicker != nil {
		bl.cleanupTicker.Stop()
	}
	
	if bl.saveTicker != nil {
		bl.saveTicker.Stop()
	}
	
	bl.wg.Wait()
}

// ServeHTTP implements the http.Handler interface
func (bl *BandwidthLimiter) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// Extract client IP
	clientIP := getClientIP(req)
	
	// Get backend address from request
	backend := req.URL.Host
	if backend == "" {
		backend = "default"
	}
	
	// Determine the bandwidth limit to apply
	limit := bl.getLimit(clientIP, backend)
	
	// Create or get the token bucket for this client/backend combination
	key := fmt.Sprintf("%s:%s", clientIP, backend)
	
	// Get or create bucket with automatic update of last used time
	wrapper := bl.getOrCreateBucket(key, limit)
	wrapper.lastUsed = time.Now() // Update last used time
	
	// Wrap the response writer to monitor bandwidth
	lrw := &limitedResponseWriter{
		ResponseWriter: rw,
		bucket:         wrapper.bucket,
	}
	
	// Call the next handler
	bl.next.ServeHTTP(lrw, req)
}

// getOrCreateBucket gets an existing bucket or creates a new one
func (bl *BandwidthLimiter) getOrCreateBucket(key string, limit int64) *bucketWrapper {
	if value, ok := bl.buckets.Load(key); ok {
		return value.(*bucketWrapper)
	}
	
	// Create new bucket
	bucket := NewTokenBucket(limit, bl.config.BurstSize)
	wrapper := &bucketWrapper{
		bucket:   bucket,
		lastUsed: time.Now(),
		key:      key,
	}
	
	// Store it (may overwrite if another goroutine created it first)
	actual, _ := bl.buckets.LoadOrStore(key, wrapper)
	return actual.(*bucketWrapper)
}

// getLimit determines the bandwidth limit for a given client IP and backend
func (bl *BandwidthLimiter) getLimit(clientIP, backend string) int64 {
	// Check for client-specific limit
	if limit, exists := bl.config.ClientLimits[clientIP]; exists {
		return limit
	}
	
	// Check for backend-specific limit
	if limit, exists := bl.config.BackendLimits[backend]; exists {
		return limit
	}
	
	// Return default limit
	return bl.config.DefaultLimit
}

// getClientIP extracts the client IP from the request
func getClientIP(req *http.Request) string {
	// Try to get IP from X-Forwarded-For header
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		ips := parseForwardedFor(xff)
		if len(ips) > 0 {
			return ips[0]
		}
	}
	
	// Try to get IP from X-Real-IP header
	if xri := req.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	
	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

// parseForwardedFor parses the X-Forwarded-For header
func parseForwardedFor(xff string) []string {
	var ips []string
	for _, ip := range strings.Split(xff, ",") {
		ip = strings.TrimSpace(ip)
		if ip != "" {
			ips = append(ips, ip)
		}
	}
	return ips
}

// limitedResponseWriter wraps http.ResponseWriter to apply bandwidth limiting
type limitedResponseWriter struct {
	http.ResponseWriter
	bucket *TokenBucket
}

// Write applies bandwidth limiting when writing response data
func (lrw *limitedResponseWriter) Write(p []byte) (int, error) {
	// Track the total bytes written
	totalWritten := 0
	remaining := p
	
	for len(remaining) > 0 {
		// Determine how many bytes to write in this iteration
		chunkSize := min(int64(len(remaining)), 4096) // 4KB chunks
		
		// Wait until we have tokens available
		for !lrw.bucket.Consume(chunkSize) {
			// No tokens available, wait a bit
			time.Sleep(10 * time.Millisecond)
		}
		
		// Write the chunk
		written, err := lrw.ResponseWriter.Write(remaining[:chunkSize])
		totalWritten += written
		
		if err != nil {
			return totalWritten, err
		}
		
		remaining = remaining[written:]
	}
	
	return totalWritten, nil
}

// Required for interface compliance, but we don't apply limiting here
func (lrw *limitedResponseWriter) WriteHeader(statusCode int) {
	lrw.ResponseWriter.WriteHeader(statusCode)
}