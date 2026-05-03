// Package temporal provides bi-temporal validity windows for memory nodes.
// Each node tracks two time axes: when the fact was true in the real world
// (valid_from/invalid_at), and when yaad recorded it (transaction time via
// git commit). Confidence decays over time using an Ebbinghaus-inspired
// exponential curve, allowing stale facts to be deprioritised during retrieval.
package temporal

import (
	"math"
	"sync"
	"time"
)

// DefaultHalfLife is the default duration after which confidence drops to 50%.
// 14 days mirrors the Ebbinghaus forgetting curve's first steep drop-off.
const DefaultHalfLife = 14 * 24 * time.Hour

// ValidityWindow represents a bi-temporal validity record for a memory node.
// ValidFrom/InvalidAt track real-world truth; GitCommit tracks transaction time.
type ValidityWindow struct {
	// ValidFrom is the moment the fact became true (real-world axis).
	ValidFrom time.Time `json:"valid_from"`

	// InvalidAt is the moment the fact stopped being true.
	// Zero value means the fact is still considered valid.
	InvalidAt time.Time `json:"invalid_at,omitempty"`

	// GitCommit is the commit hash that recorded this fact (transaction axis).
	GitCommit string `json:"git_commit,omitempty"`

	// Confidence is the initial confidence score in [0,1].
	// It decays over time via DecayedConfidence.
	Confidence float64 `json:"confidence"`

	// HalfLife controls how fast confidence decays.
	// Zero value falls back to DefaultHalfLife.
	HalfLife time.Duration `json:"half_life,omitempty"`

	mu sync.RWMutex
}

// NewWindow creates a validity window that starts now with the given confidence.
func NewWindow(confidence float64, gitCommit string) *ValidityWindow {
	return &ValidityWindow{
		ValidFrom:  time.Now(),
		GitCommit:  gitCommit,
		Confidence: clamp(confidence),
	}
}

// NewWindowAt creates a validity window starting at a specific time.
func NewWindowAt(validFrom time.Time, confidence float64, gitCommit string) *ValidityWindow {
	return &ValidityWindow{
		ValidFrom:  validFrom,
		GitCommit:  gitCommit,
		Confidence: clamp(confidence),
	}
}

// IsValid reports whether the fact is currently considered true.
// A window is valid when InvalidAt is zero (not yet invalidated).
func (w *ValidityWindow) IsValid() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.InvalidAt.IsZero()
}

// IsValidAt reports whether the fact was valid at a specific point in time.
func (w *ValidityWindow) IsValidAt(t time.Time) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if t.Before(w.ValidFrom) {
		return false
	}
	if w.InvalidAt.IsZero() {
		return true
	}
	return t.Before(w.InvalidAt)
}

// Invalidate marks the fact as no longer true, effective now.
func (w *ValidityWindow) Invalidate() {
	w.InvalidateAt(time.Now())
}

// InvalidateAt marks the fact as no longer true, effective at t.
func (w *ValidityWindow) InvalidateAt(t time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.InvalidAt = t
}

// Age returns how long the fact has been recorded (since ValidFrom).
func (w *ValidityWindow) Age() time.Duration {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return time.Since(w.ValidFrom)
}

// Duration returns how long the fact was (or has been) valid.
// For still-valid facts, this returns the time since ValidFrom.
// For invalidated facts, this returns InvalidAt - ValidFrom.
func (w *ValidityWindow) Duration() time.Duration {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.InvalidAt.IsZero() {
		return time.Since(w.ValidFrom)
	}
	return w.InvalidAt.Sub(w.ValidFrom)
}

// DecayedConfidence returns the current confidence after applying
// Ebbinghaus-inspired exponential decay: C(t) = C0 * 2^(-age/half_life).
// This matches the forgetting curve where retention halves every half-life.
func (w *ValidityWindow) DecayedConfidence() float64 {
	return w.DecayedConfidenceAt(time.Now())
}

// DecayedConfidenceAt returns what the confidence would be at time t.
func (w *ValidityWindow) DecayedConfidenceAt(t time.Time) float64 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	age := t.Sub(w.ValidFrom)
	if age <= 0 {
		return w.Confidence
	}

	hl := w.halfLife()
	// Ebbinghaus exponential decay: C(t) = C0 * 2^(-t/half_life)
	decay := math.Pow(2, -float64(age)/float64(hl))
	return w.Confidence * decay
}

// SetHalfLife overrides the decay half-life for this window.
func (w *ValidityWindow) SetHalfLife(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.HalfLife = d
}

// halfLife returns the effective half-life (caller must hold mu).
func (w *ValidityWindow) halfLife() time.Duration {
	if w.HalfLife > 0 {
		return w.HalfLife
	}
	return DefaultHalfLife
}

// clamp restricts confidence to [0, 1].
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
