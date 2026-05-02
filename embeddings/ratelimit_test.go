package embeddings

import (
	"testing"
	"time"
)

func TestExtractRetryDelay_Seconds(t *testing.T) {
	d := ExtractRetryDelay("Rate limit exceeded. Please try again in 1.5s")
	want := time.Duration(1500 * time.Millisecond)
	if d != want {
		t.Errorf("expected %v, got %v", want, d)
	}
}

func TestExtractRetryDelay_Milliseconds(t *testing.T) {
	d := ExtractRetryDelay("retry in 500ms before next request")
	want := 500 * time.Millisecond
	if d != want {
		t.Errorf("expected %v, got %v", want, d)
	}
}

func TestExtractRetryDelay_FullWord(t *testing.T) {
	d := ExtractRetryDelay("Please try again in 2 seconds.")
	want := 2 * time.Second
	if d != want {
		t.Errorf("expected %v, got %v", want, d)
	}
}

func TestExtractRetryDelay_NoMatch(t *testing.T) {
	d := ExtractRetryDelay("generic error with no timing info")
	if d != 0 {
		t.Errorf("expected 0 for non-matching message, got %v", d)
	}
}

func TestPacer_Wait(t *testing.T) {
	p := NewPacer(50 * time.Millisecond)
	start := time.Now()
	p.Wait() // first call should not block
	p.Wait() // second call should block ~50ms
	elapsed := time.Since(start)
	if elapsed < 40*time.Millisecond {
		t.Errorf("expected pacer to enforce at least ~50ms interval, got %v", elapsed)
	}
}

func TestPacer_SetInterval(t *testing.T) {
	p := NewPacer(1 * time.Hour)
	// Shrink the interval so the next Wait does not block for an hour
	p.SetInterval(10 * time.Millisecond)
	// Reset nextRequestAt to now so the shrunk interval takes effect
	p.mu.Lock()
	p.nextRequestAt = time.Now()
	p.mu.Unlock()

	start := time.Now()
	p.Wait()
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected pacer with 10ms interval to return quickly, got %v", elapsed)
	}
}
