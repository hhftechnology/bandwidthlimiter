package bandwidthlimiter_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"fmt"
	"testing"
	"time"

	"github.com/hhftechnology/bandwidthlimiter"
)

// TestBandwidthLimiter tests the basic bandwidth limiting functionality
func TestBandwidthLimiter(t *testing.T) {
	// Create plugin configuration with more aggressive limits for testing
	cfg := bandwidthlimiter.CreateConfig()
	cfg.DefaultLimit = 1024 * 50  // 50 KB/s (reduced for faster testing)
	cfg.BurstSize = 1024 * 10     // 10 KB burst (smaller burst for clearer testing)
	
	// Create context
	ctx := context.Background()
	
	// Create a test handler that sends a large response
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Send 100 KB of data (should take ~2 seconds at 50 KB/s)
		data := make([]byte, 100*1024)
		for i := range data {
			data[i] = byte(i % 256)
		}
		// Write in chunks to ensure rate limiting is applied properly
		written := 0
		chunkSize := 4096
		for written < len(data) {
			end := written + chunkSize
			if end > len(data) {
				end = len(data)
			}
			rw.Write(data[written:end])
			written = end
			if rw, ok := rw.(http.Flusher); ok {
				rw.Flush()
			}
		}
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
	// With 50 KB/s limit and 100 KB data, it should take at least 1.5-2 seconds
	minExpectedTime := time.Second
	if elapsed < minExpectedTime {
		t.Errorf("Response was not properly throttled. Expected >%v, got %v", minExpectedTime, elapsed)
	}
	
	// Verify the response size
	body := recorder.Body.Bytes()
	if len(body) != 100*1024 {
		t.Errorf("Unexpected response size. Expected %d, got %d", 100*1024, len(body))
	}
}

// TestPerBackendLimits tests that different backends get different limits
func TestPerBackendLimits(t *testing.T) {
	cfg := bandwidthlimiter.CreateConfig()
	cfg.DefaultLimit = 1024 * 25  // 25 KB/s default (slower for testing)
	cfg.BackendLimits = map[string]int64{
		"fast-api.local": 1024 * 100, // 100 KB/s for fast API
	}
	cfg.BurstSize = 1024 * 5 // 5 KB burst (smaller for clearer testing)
	
	ctx := context.Background()
	
	// Create handler that sends 50 KB
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		data := make([]byte, 50*1024)
		// Write in small chunks to ensure rate limiting is applied
		written := 0
		chunkSize := 2048
		for written < len(data) {
			end := written + chunkSize
			if end > len(data) {
				end = len(data)
			}
			rw.Write(data[written:end])
			written = end
			if rw, ok := rw.(http.Flusher); ok {
				rw.Flush()
			}
		}
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
		
		// With 25 KB/s, 50 KB should take ~2 seconds (accounting for burst)
		minExpectedTime := time.Duration(1.5 * float64(time.Second))
		if elapsed < minExpectedTime {
			t.Errorf("Default backend was too fast. Expected >%v, got %v", minExpectedTime, elapsed)
		}
	})
	
	// Test fast backend (should be faster)
	t.Run("FastBackend", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://fast-api.local", nil)
		
		start := time.Now()
		handler.ServeHTTP(recorder, req)
		elapsed := time.Since(start)
		
		// With 100 KB/s, 50 KB should take ~0.5 seconds (with burst)
		maxExpectedTime := time.Second
		if elapsed > maxExpectedTime {
			t.Errorf("Fast backend was too slow. Expected <%v, got %v", maxExpectedTime, elapsed)
		}
	})
}

// TestPerClientLimits tests that different client IPs get different limits
func TestPerClientLimits(t *testing.T) {
	cfg := bandwidthlimiter.CreateConfig()
	cfg.DefaultLimit = 1024 * 25  // 25 KB/s default (slower for testing)
	cfg.ClientLimits = map[string]int64{
		"10.0.0.100": 1024 * 75, // 75 KB/s for premium client
	}
	cfg.BurstSize = 1024 * 5 // 5 KB burst (smaller for clearer testing)
	
	ctx := context.Background()
	
	// Create handler that sends 50 KB
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		data := make([]byte, 50*1024)
		// Write in small chunks to ensure rate limiting is applied
		written := 0
		chunkSize := 2048
		for written < len(data) {
			end := written + chunkSize
			if end > len(data) {
				end = len(data)
			}
			rw.Write(data[written:end])
			written = end
			if rw, ok := rw.(http.Flusher); ok {
				rw.Flush()
			}
		}
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
		
		// With 25 KB/s, 50 KB should take ~2 seconds (accounting for burst)
		minExpectedTime := time.Duration(1.5 * float64(time.Second))
		if elapsed < minExpectedTime {
			t.Errorf("Regular client was too fast. Expected >%v, got %v", minExpectedTime, elapsed)
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
		
		// With 75 KB/s, 50 KB should take ~0.7 seconds (with burst)
		maxExpectedTime := time.Second
		if elapsed > maxExpectedTime {
			t.Errorf("Premium client was too slow. Expected <%v, got %v", maxExpectedTime, elapsed)
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

// TestPersistence tests file-based persistence functionality through the public interface
func TestPersistence(t *testing.T) {
	// Create temporary file for testing
	tempFile := t.TempDir() + "/test-buckets.json"
	
	cfg := bandwidthlimiter.CreateConfig()
	cfg.DefaultLimit = 1048576
	cfg.PersistenceFile = tempFile
	cfg.SaveInterval = 1 // Save every second for testing
	
	ctx := context.Background()
	
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Write([]byte("test"))
	})
	
	// Create first instance and make some requests
	handler1, err := bandwidthlimiter.New(ctx, next, cfg, "test-limiter-1")
	if err != nil {
		t.Fatal(err)
	}
	
	// Create some traffic to generate buckets
	for i := 0; i < 3; i++ {
		recorder := httptest.NewRecorder()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost", nil)
		req.RemoteAddr = fmt.Sprintf("192.168.1.%d:12345", i)
		handler1.ServeHTTP(recorder, req)
	}
	
	// Wait for the save interval to ensure buckets are saved
	time.Sleep(2 * time.Second)
	
	// We need to access the private Shutdown method, so let's cast the handler
	// This is a bit of a hack, but necessary since we can't access private fields
	if handler1, ok := handler1.(*bandwidthlimiter.BandwidthLimiter); ok {
		handler1.Shutdown()
	} else {
		t.Fatal("Handler is not of type *BandwidthLimiter")
	}
	
	// Check that the file exists and has content
	if _, err := os.Stat(tempFile); os.IsNotExist(err) {
		t.Errorf("Persistence file was not created: %s", tempFile)
	}
	
	// Create second instance and verify it loads the saved buckets
	// We can't directly verify the bucket count since buckets is private
	// But we can verify that the instance loads successfully
	handler2, err := bandwidthlimiter.New(ctx, next, cfg, "test-limiter-2")
	if err != nil {
		t.Fatal(err)
	}
	
	// Make a request with one of the previous IPs to verify the bucket was loaded
	recorder := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost", nil)
	req.RemoteAddr = "192.168.1.0:12345" // Use the first IP from the previous test
	handler2.ServeHTTP(recorder, req)
	
	// If no error occurred, the bucket was likely loaded successfully
	if recorder.Code != http.StatusOK {
		t.Errorf("Second instance failed to handle request, possibly due to persistence issues")
	}
	
	// Cleanup
	if handler2, ok := handler2.(*bandwidthlimiter.BandwidthLimiter); ok {
		handler2.Shutdown()
	}
}