package bandwidthlimiter_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hhftechnology/bandwidthlimiter"
)

// TestBandwidthLimiter tests the basic bandwidth limiting functionality
func TestBandwidthLimiter(t *testing.T) {
	// Create plugin configuration
	cfg := bandwidthlimiter.CreateConfig()
	cfg.DefaultLimit = 1024 * 100 // 100 KB/s
	cfg.BurstSize = 1024 * 50     // 50 KB burst
	
	// Create context
	ctx := context.Background()
	
	// Create a test handler that sends a large response
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Send 200 KB of data (should take ~2 seconds at 100 KB/s)
		data := make([]byte, 200*1024)
		for i := range data {
			data[i] = byte(i % 256)
		}
		rw.Write(data)
	})
	
	// Create the bandwidth limiter middleware
	handler, err := bandwidthlimiter.New(ctx, next, cfg, "test-limiter")
	if err != nil {
		t.Fatal(err)
	}
	
	// Create a recorder to capture the response
	recorder := httptest.NewRecorder()
	
	// Create a test request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	// Measure the time it takes to get the response
	start := time.Now()
	
	// Execute the request
	handler.ServeHTTP(recorder, req)
	
	elapsed := time.Since(start)
	
	// Verify that the response was throttled
	// With 100 KB/s limit and 200 KB data, it should take at least 1.5 seconds
	// (accounting for burst allowance)
	if elapsed < time.Second {
		t.Errorf("Response was not properly throttled. Expected >1s, got %v", elapsed)
	}
	
	// Verify the response size
	body := recorder.Body.Bytes()
	if len(body) != 200*1024 {
		t.Errorf("Unexpected response size. Expected %d, got %d", 200*1024, len(body))
	}
}

// TestPerBackendLimits tests that different backends get different limits
func TestPerBackendLimits(t *testing.T) {
	cfg := bandwidthlimiter.CreateConfig()
	cfg.DefaultLimit = 1024 * 50  // 50 KB/s default
	cfg.BackendLimits = map[string]int64{
		"fast-api.local": 1024 * 200, // 200 KB/s for fast API
	}
	cfg.BurstSize = 1024 * 10 // 10 KB burst
	
	ctx := context.Background()
	
	// Create handler that sends 100 KB
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		data := make([]byte, 100*1024)
		rw.Write(data)
	})
	
	handler, err := bandwidthlimiter.New(ctx, next, cfg, "test-limiter")
	if err != nil {
		t.Fatal(err)
	}
	
	// Test default backend (should be slower)
	t.Run("DefaultBackend", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://default-api.local", nil)
		
		start := time.Now()
		handler.ServeHTTP(recorder, req)
		elapsed := time.Since(start)
		
		// With 50 KB/s, 100 KB should take ~1.5-2 seconds (accounting for burst)
		if elapsed < time.Second {
			t.Errorf("Default backend was too fast. Expected >1s, got %v", elapsed)
		}
	})
	
	// Test fast backend (should be faster)
	t.Run("FastBackend", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://fast-api.local", nil)
		
		start := time.Now()
		handler.ServeHTTP(recorder, req)
		elapsed := time.Since(start)
		
		// With 200 KB/s, 100 KB should take ~0.5 seconds (with burst)
		if elapsed > time.Second {
			t.Errorf("Fast backend was too slow. Expected <1s, got %v", elapsed)
		}
	})
}

// TestPerClientLimits tests that different client IPs get different limits
func TestPerClientLimits(t *testing.T) {
	cfg := bandwidthlimiter.CreateConfig()
	cfg.DefaultLimit = 1024 * 50  // 50 KB/s default
	cfg.ClientLimits = map[string]int64{
		"10.0.0.100": 1024 * 150, // 150 KB/s for premium client
	}
	
	ctx := context.Background()
	
	// Create handler that sends 75 KB
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		data := make([]byte, 75*1024)
		rw.Write(data)
	})
	
	handler, err := bandwidthlimiter.New(ctx, next, cfg, "test-limiter")
	if err != nil {
		t.Fatal(err)
	}
	
	// Test regular client
	t.Run("RegularClient", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost", nil)
		req.RemoteAddr = "192.168.1.100:12345" // Regular client IP
		
		start := time.Now()
		handler.ServeHTTP(recorder, req)
		elapsed := time.Since(start)
		
		// Should take at least 1 second with 50 KB/s limit
		if elapsed < time.Millisecond*800 {
			t.Errorf("Regular client was too fast. Expected >800ms, got %v", elapsed)
		}
	})
	
	// Test premium client
	t.Run("PremiumClient", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost", nil)
		req.RemoteAddr = "10.0.0.100:12345" // Premium client IP
		
		start := time.Now()
		handler.ServeHTTP(recorder, req)
		elapsed := time.Since(start)
		
		// Should be faster with 150 KB/s limit
		if elapsed > time.Millisecond*600 {
			t.Errorf("Premium client was too slow. Expected <600ms, got %v", elapsed)
		}
	})
}

// TestTokenBucket tests the token bucket implementation directly
func TestTokenBucket(t *testing.T) {
	bucket := bandwidthlimiter.NewTokenBucket(1000, 2000) // 1000 tokens/second, 2000 burst
	
	// Should be able to consume burst amount initially
	if !bucket.Consume(2000) {
		t.Error("Should be able to consume burst amount initially")
	}
	
	// Should not be able to consume more than burst
	if bucket.Consume(100) {
		t.Error("Should not be able to consume more than burst")
	}
	
	// Wait for refill
	time.Sleep(100 * time.Millisecond)
	
	// Should be able to consume some tokens after waiting
	if !bucket.Consume(50) {
		t.Error("Should be able to consume tokens after waiting")
	}
}