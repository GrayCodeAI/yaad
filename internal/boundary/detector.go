// Package boundary implements semantic boundary detection for memory consolidation.
// Based on GAM (arxiv:2604.12285): consolidate only at semantic boundaries,
// not arbitrary session ends, to prevent transient noise contaminating long-term memory.
package boundary

import (
	"math"
	"strings"
	"unicode"
)

// Detector detects semantic topic shifts in a stream of memory content.
type Detector struct {
	buffer    []string // recent content items
	maxBuffer int      // max items before forced consolidation
	threshold float64  // cosine distance threshold for boundary detection
}

// New creates a boundary detector.
// threshold: 0.0-1.0, higher = more sensitive to topic shifts (default 0.3)
func New(maxBuffer int, threshold float64) *Detector {
	if maxBuffer <= 0 {
		maxBuffer = 20
	}
	if threshold <= 0 {
		threshold = 0.3
	}
	return &Detector{maxBuffer: maxBuffer, threshold: threshold}
}

// Add adds content to the buffer and returns true if a semantic boundary is detected.
// A boundary means: consolidate the current buffer into a topic node.
func (d *Detector) Add(content string) bool {
	if len(d.buffer) == 0 {
		d.buffer = append(d.buffer, content)
		return false
	}

	// Check semantic distance between new content and buffer summary
	bufferSummary := strings.Join(d.buffer, " ")
	dist := semanticDistance(bufferSummary, content)

	d.buffer = append(d.buffer, content)

	// Boundary detected if:
	// 1. Semantic distance exceeds threshold (topic shift), OR
	// 2. Buffer is full (forced consolidation)
	if dist > d.threshold || len(d.buffer) >= d.maxBuffer {
		return true
	}
	return false
}

// Flush returns and clears the current buffer.
func (d *Detector) Flush() []string {
	buf := d.buffer
	d.buffer = nil
	return buf
}

// Size returns current buffer size.
func (d *Detector) Size() int { return len(d.buffer) }

// semanticDistance computes a lightweight semantic distance between two texts.
// Uses TF-IDF-inspired bag-of-words cosine distance — no embeddings needed.
// Returns 0.0 (identical) to 1.0 (completely different).
func semanticDistance(a, b string) float64 {
	vecA := termFreq(a)
	vecB := termFreq(b)
	return 1.0 - cosineSim(vecA, vecB)
}

func termFreq(text string) map[string]float64 {
	words := tokenize(text)
	freq := map[string]float64{}
	for _, w := range words {
		if len(w) > 2 && !isStopWord(w) {
			freq[w]++
		}
	}
	// Normalize
	total := 0.0
	for _, v := range freq {
		total += v * v
	}
	if total > 0 {
		norm := math.Sqrt(total)
		for k := range freq {
			freq[k] /= norm
		}
	}
	return freq
}

func cosineSim(a, b map[string]float64) float64 {
	dot := 0.0
	for k, va := range a {
		if vb, ok := b[k]; ok {
			dot += va * vb
		}
	}
	return dot
}

func tokenize(text string) []string {
	text = strings.ToLower(text)
	var words []string
	var word strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
		} else if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}
	return words
}

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true,
	"not": true, "you": true, "all": true, "can": true, "had": true,
	"her": true, "was": true, "one": true, "our": true, "out": true,
	"use": true, "with": true, "this": true, "that": true, "from": true,
	"they": true, "will": true, "have": true, "been": true, "when": true,
}

func isStopWord(w string) bool { return stopWords[w] }
