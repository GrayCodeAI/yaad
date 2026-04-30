package server

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimiter is a simple per-IP token-bucket rate limiter.
type RateLimiter struct {
	mu       sync.RWMutex
	buckets  map[string]*bucket
	rate     float64 // tokens per second
	capacity int     // max burst
	stopCh   chan struct{}
}

type bucket struct {
	tokens    float64
	lastCheck time.Time
}

// NewRateLimiter creates a limiter with the given rate (req/sec) and burst capacity.
// Call Stop() when the limiter is no longer needed.
func NewRateLimiter(rate float64, capacity int) *RateLimiter {
	rl := &RateLimiter{
		buckets:  make(map[string]*bucket),
		rate:     rate,
		capacity: capacity,
		stopCh:   make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.Cleanup(10 * time.Minute)
		case <-rl.stopCh:
			return
		}
	}
}

// Stop halts the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	select {
	case <-rl.stopCh:
	default:
		close(rl.stopCh)
	}
}

// Allow reports whether the request from the given IP is within rate limits.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[ip]
	if !ok {
		rl.buckets[ip] = &bucket{tokens: float64(rl.capacity) - 1, lastCheck: time.Now()}
		return true
	}

	now := time.Now()
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens = min(b.tokens+elapsed*rl.rate, float64(rl.capacity))
	b.lastCheck = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Cleanup removes stale buckets to prevent memory growth.
func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for ip, b := range rl.buckets {
		if b.lastCheck.Before(cutoff) {
			delete(rl.buckets, ip)
		}
	}
}

// clientIP extracts the client IP from the request's RemoteAddr.
// X-Forwarded-For is intentionally ignored to prevent rate-limit bypass via header spoofing.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// withRateLimit wraps the handler with per-IP rate limiting.
func (s *RESTServer) withRateLimit(next http.Handler) http.Handler {
	if s.limiter == nil {
		s.limiter = NewRateLimiter(10, 30)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.Allow(clientIP(r)) {
			httpErr(w, fmt.Errorf("rate limit exceeded"), 429)
			return
		}
		next.ServeHTTP(w, r)
	})
}
