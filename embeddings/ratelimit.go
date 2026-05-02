package embeddings

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var retryAfterRegex = regexp.MustCompile(`(?i)(?:retry|try) (?:again )?in ([0-9.]+)\s*(ms|s|seconds|milliseconds)`)

// ExtractRetryDelay parses rate-limit error messages for the recommended wait
// time. It recognises patterns such as "Please try again in 1.5s",
// "retry in 500ms", and "try again in 2 seconds". Returns 0 if no delay is
// found.
func ExtractRetryDelay(errMsg string) time.Duration {
	m := retryAfterRegex.FindStringSubmatch(errMsg)
	if m == nil {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(m[2])
	switch unit {
	case "ms", "milliseconds":
		return time.Duration(val * float64(time.Millisecond))
	case "s", "seconds":
		return time.Duration(val * float64(time.Second))
	}
	return 0
}

// Pacer enforces minimum intervals between API requests to stay under rate
// limits.
type Pacer struct {
	mu            sync.Mutex
	minInterval   time.Duration
	nextRequestAt time.Time
}

// NewPacer creates a Pacer with the given minimum interval between requests.
func NewPacer(minInterval time.Duration) *Pacer {
	return &Pacer{minInterval: minInterval}
}

// Wait blocks until the next request is allowed according to the pacer's
// minimum interval.
func (p *Pacer) Wait() {
	p.mu.Lock()
	now := time.Now()
	if now.Before(p.nextRequestAt) {
		wait := p.nextRequestAt.Sub(now)
		p.nextRequestAt = p.nextRequestAt.Add(p.minInterval)
		p.mu.Unlock()
		time.Sleep(wait)
		return
	}
	p.nextRequestAt = now.Add(p.minInterval)
	p.mu.Unlock()
}

// SetInterval adjusts the pacer's minimum interval dynamically.
func (p *Pacer) SetInterval(d time.Duration) {
	p.mu.Lock()
	p.minInterval = d
	p.mu.Unlock()
}
