package embeddings

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestEmbeddingMemo_GetPut(t *testing.T) {
	m := NewEmbeddingMemo(10)
	if _, ok := m.Get("hello"); ok {
		t.Fatal("expected miss on empty cache")
	}
	m.Put("hello", []float32{1, 2, 3})
	vec, ok := m.Get("hello")
	if !ok || len(vec) != 3 || vec[0] != 1 {
		t.Fatalf("expected hit, got ok=%v vec=%v", ok, vec)
	}
}

func TestEmbeddingMemo_LRUEviction(t *testing.T) {
	m := NewEmbeddingMemo(3)
	m.Put("a", []float32{1})
	m.Put("b", []float32{2})
	m.Put("c", []float32{3})
	// Access "a" to promote it
	m.Get("a")
	// Adding "d" should evict "b" (oldest unused)
	m.Put("d", []float32{4})
	if _, ok := m.Get("b"); ok {
		t.Fatal("expected 'b' to be evicted")
	}
	if _, ok := m.Get("a"); !ok {
		t.Fatal("expected 'a' to survive (was promoted)")
	}
}

// countingProvider tracks how many Embed/EmbedBatch calls hit the inner provider.
type countingProvider struct {
	localStub
	embedCalls int64
	batchCalls int64
}

func (c *countingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	atomic.AddInt64(&c.embedCalls, 1)
	return c.localStub.Embed(ctx, text)
}

func (c *countingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	atomic.AddInt64(&c.batchCalls, 1)
	return c.localStub.EmbedBatch(ctx, texts)
}

func TestMemoizedProvider_Embed(t *testing.T) {
	inner := &countingProvider{}
	p := NewMemoizedProvider(inner, 100)
	ctx := context.Background()

	v1, err := p.Embed(ctx, "foo")
	if err != nil || v1 == nil {
		t.Fatalf("first embed failed: %v", err)
	}
	v2, err := p.Embed(ctx, "foo")
	if err != nil || v2 == nil {
		t.Fatalf("second embed failed: %v", err)
	}
	if inner.embedCalls != 1 {
		t.Fatalf("expected 1 inner call, got %d", inner.embedCalls)
	}
}

func TestMemoizedProvider_EmbedBatch(t *testing.T) {
	inner := &countingProvider{}
	p := NewMemoizedProvider(inner, 100)
	ctx := context.Background()

	// Pre-cache one
	p.Embed(ctx, "a")

	// Batch with one cached, one new
	vecs, err := p.EmbedBatch(ctx, []string{"a", "b"})
	if err != nil || len(vecs) != 2 {
		t.Fatalf("batch failed: %v", err)
	}
	// Inner batch should only have been called for "b"
	if inner.batchCalls != 1 {
		t.Fatalf("expected 1 batch call, got %d", inner.batchCalls)
	}

	// Now both cached — no more inner calls
	vecs2, err := p.EmbedBatch(ctx, []string{"a", "b"})
	if err != nil || len(vecs2) != 2 {
		t.Fatalf("second batch failed: %v", err)
	}
	if inner.batchCalls != 1 {
		t.Fatalf("expected still 1 batch call, got %d", inner.batchCalls)
	}
}
