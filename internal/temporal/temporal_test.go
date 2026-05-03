package temporal

import (
	"math"
	"testing"
	"time"
)

func TestNewWindow(t *testing.T) {
	w := NewWindow(0.9, "abc123")
	if !w.IsValid() {
		t.Fatal("new window should be valid")
	}
	if w.Confidence != 0.9 {
		t.Fatalf("expected confidence 0.9, got %f", w.Confidence)
	}
	if w.GitCommit != "abc123" {
		t.Fatalf("expected git commit abc123, got %s", w.GitCommit)
	}
}

func TestClampConfidence(t *testing.T) {
	w := NewWindow(1.5, "")
	if w.Confidence != 1.0 {
		t.Fatalf("confidence > 1 should be clamped to 1, got %f", w.Confidence)
	}
	w2 := NewWindow(-0.3, "")
	if w2.Confidence != 0.0 {
		t.Fatalf("confidence < 0 should be clamped to 0, got %f", w2.Confidence)
	}
}

func TestIsValidAt(t *testing.T) {
	start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	w := NewWindowAt(start, 0.8, "def456")

	// Before ValidFrom: not valid.
	if w.IsValidAt(start.Add(-time.Hour)) {
		t.Fatal("should not be valid before ValidFrom")
	}
	// At ValidFrom: valid.
	if !w.IsValidAt(start) {
		t.Fatal("should be valid at ValidFrom")
	}
	// After ValidFrom, before invalidation: valid.
	if !w.IsValidAt(start.Add(24 * time.Hour)) {
		t.Fatal("should be valid after ValidFrom when not invalidated")
	}

	// Invalidate at a specific time.
	end := start.Add(48 * time.Hour)
	w.InvalidateAt(end)

	if w.IsValid() {
		t.Fatal("should not be valid after invalidation")
	}
	// Just before invalidation: still valid.
	if !w.IsValidAt(end.Add(-time.Second)) {
		t.Fatal("should be valid just before InvalidAt")
	}
	// At or after invalidation: not valid.
	if w.IsValidAt(end) {
		t.Fatal("should not be valid at InvalidAt")
	}
}

func TestInvalidate(t *testing.T) {
	w := NewWindow(1.0, "")
	if !w.IsValid() {
		t.Fatal("should start valid")
	}
	w.Invalidate()
	if w.IsValid() {
		t.Fatal("should be invalid after Invalidate()")
	}
}

func TestAge(t *testing.T) {
	past := time.Now().Add(-2 * time.Hour)
	w := NewWindowAt(past, 0.9, "")
	age := w.Age()
	// Allow 1 second of slack for test execution time.
	if age < 2*time.Hour-time.Second || age > 2*time.Hour+time.Second {
		t.Fatalf("expected age ~2h, got %v", age)
	}
}

func TestDuration(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	w := NewWindowAt(start, 0.8, "")
	w.InvalidateAt(start.Add(10 * 24 * time.Hour))
	d := w.Duration()
	if d != 10*24*time.Hour {
		t.Fatalf("expected duration 240h, got %v", d)
	}
}

func TestDecayedConfidence(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	w := NewWindowAt(start, 1.0, "")

	// At creation time, confidence should be unchanged.
	c0 := w.DecayedConfidenceAt(start)
	if math.Abs(c0-1.0) > 1e-9 {
		t.Fatalf("at creation, expected 1.0, got %f", c0)
	}

	// After one default half-life (14 days), confidence should be ~0.5.
	c1 := w.DecayedConfidenceAt(start.Add(DefaultHalfLife))
	if math.Abs(c1-0.5) > 1e-6 {
		t.Fatalf("at 1 half-life, expected ~0.5, got %f", c1)
	}

	// After two half-lives, confidence should be ~0.25.
	c2 := w.DecayedConfidenceAt(start.Add(2 * DefaultHalfLife))
	if math.Abs(c2-0.25) > 1e-6 {
		t.Fatalf("at 2 half-lives, expected ~0.25, got %f", c2)
	}
}

func TestCustomHalfLife(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	w := NewWindowAt(start, 1.0, "")
	w.SetHalfLife(7 * 24 * time.Hour) // 7-day half-life

	// After 7 days, should be ~0.5.
	c := w.DecayedConfidenceAt(start.Add(7 * 24 * time.Hour))
	if math.Abs(c-0.5) > 1e-6 {
		t.Fatalf("with 7d half-life at 7d, expected ~0.5, got %f", c)
	}

	// After 14 days (2 half-lives), should be ~0.25.
	c2 := w.DecayedConfidenceAt(start.Add(14 * 24 * time.Hour))
	if math.Abs(c2-0.25) > 1e-6 {
		t.Fatalf("with 7d half-life at 14d, expected ~0.25, got %f", c2)
	}
}

func TestDecayBeforeCreation(t *testing.T) {
	start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	w := NewWindowAt(start, 0.8, "")

	// Querying before creation should return base confidence (no decay).
	c := w.DecayedConfidenceAt(start.Add(-time.Hour))
	if math.Abs(c-0.8) > 1e-9 {
		t.Fatalf("before creation, expected 0.8, got %f", c)
	}
}

func TestConcurrentAccess(t *testing.T) {
	w := NewWindow(0.9, "abc")
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			_ = w.IsValid()
			_ = w.DecayedConfidence()
			_ = w.Age()
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		if i == 500 {
			w.Invalidate()
		}
		_ = w.Duration()
	}
	<-done
}
