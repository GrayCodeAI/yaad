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
}

type bucket struct {
	tokens    float64
	lastCheck time.Time
}

// NewRateLimiter creates a limiter with the given rate (req/sec) and burst capacity.
func NewRateLimiter(rate float64, capacity int) *RateLimiter {
	return &RateLimiter{
		buckets:  make(map[string]*bucket),
		rate:     rate,
		capacity: capacity,
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

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// clientIP extracts the client IP from a request, preferring X-Forwarded-For.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if host, _, err := net.SplitHostPort(fwd); err == nil {
			return host
		}
		return fwd
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// withRateLimit wraps the handler with per-IP rate limiting.
func (s *RESTServer) withRateLimit(next http.Handler) http.Handler {
	// Default: 30 req/sec burst, 10 req/sec sustained
	rl := NewRateLimiter(10, 30)
	cleanupTicker := time.NewTicker(5 * time.Minute)
	go func() {
		for range cleanupTicker.C {
			rl.Cleanup(10 * time.Minute)
		}
	}()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.Allow(clientIP(r)) {
			httpErr(w, fmt.Errorf("rate limit exceeded"), 429)
			return
		}
		next.ServeHTTP(w, r)
	})
}
