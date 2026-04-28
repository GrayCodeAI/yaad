// Package dedup implements rolling-window deduplication.
// Skips near-duplicate memories within a configurable time window.
// Based on Engram's SHA-256 dedup with 15-minute rolling window
// and agentmemory's 5-minute dedup window.
package dedup

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// Window tracks recent content hashes to skip near-duplicates.
type Window struct {
	duration time.Duration
	seen     map[string]time.Time
	mu       sync.Mutex
}

// New creates a dedup window. Default: 5 minutes.
func New(duration time.Duration) *Window {
	if duration <= 0 {
		duration = 5 * time.Minute
	}
	return &Window{duration: duration, seen: map[string]time.Time{}}
}

// IsDuplicate returns true if content was seen within the rolling window.
// If not a duplicate, records it for future checks.
func (w *Window) IsDuplicate(content string) bool {
	hash := contentHash(content)

	w.mu.Lock()
	defer w.mu.Unlock()

	// Clean expired entries
	now := time.Now()
	for k, t := range w.seen {
		if now.Sub(t) > w.duration {
			delete(w.seen, k)
		}
	}

	// Check if seen recently
	if _, exists := w.seen[hash]; exists {
		return true
	}

	w.seen[hash] = now
	return false
}

// NormalizedHash returns a hash that ignores whitespace differences.
func contentHash(content string) string {
	h := sha256.Sum256([]byte(normalizeWhitespace(content)))
	return fmt.Sprintf("%x", h[:16])
}

func normalizeWhitespace(s string) string {
	var result []byte
	space := false
	for _, b := range []byte(s) {
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			if !space {
				result = append(result, ' ')
				space = true
			}
		} else {
			result = append(result, b)
			space = false
		}
	}
	return string(result)
}
