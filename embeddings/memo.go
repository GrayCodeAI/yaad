package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// EmbeddingMemo caches embeddings by content hash to skip re-embedding unchanged content.
type EmbeddingMemo struct {
	mu    sync.RWMutex
	cache map[string][]float32 // sha256(content) -> embedding
	order []string             // insertion order for LRU eviction
	max   int
}

// NewEmbeddingMemo creates a memo cache with the given max entry count.
func NewEmbeddingMemo(maxEntries int) *EmbeddingMemo {
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	return &EmbeddingMemo{
		cache: make(map[string][]float32, maxEntries),
		order: make([]string, 0, maxEntries),
		max:   maxEntries,
	}
}

// Get returns a cached embedding for the content, if present.
func (m *EmbeddingMemo) Get(content string) ([]float32, bool) {
	key := contentHash(content)
	m.mu.RLock()
	vec, ok := m.cache[key]
	m.mu.RUnlock()
	if ok {
		// Promote to end (most recently used)
		m.mu.Lock()
		m.promote(key)
		m.mu.Unlock()
	}
	return vec, ok
}

// Put stores an embedding for the given content, evicting the oldest entry if at capacity.
func (m *EmbeddingMemo) Put(content string, embedding []float32) {
	key := contentHash(content)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.cache[key]; exists {
		m.cache[key] = embedding
		m.promote(key)
		return
	}
	for len(m.order) >= m.max {
		oldest := m.order[0]
		m.order = m.order[1:]
		delete(m.cache, oldest)
	}
	m.cache[key] = embedding
	m.order = append(m.order, key)
}

// Len returns the number of cached entries.
func (m *EmbeddingMemo) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.cache)
}

func (m *EmbeddingMemo) promote(key string) {
	for i, k := range m.order {
		if k == key {
			m.order = append(m.order[:i], m.order[i+1:]...)
			m.order = append(m.order, key)
			return
		}
	}
}

func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// MemoizedProvider wraps a Provider with content-addressed memoization.
type MemoizedProvider struct {
	inner Provider
	memo  *EmbeddingMemo
}

// NewMemoizedProvider wraps an existing Provider with a memo cache.
func NewMemoizedProvider(inner Provider, maxEntries int) *MemoizedProvider {
	return &MemoizedProvider{
		inner: inner,
		memo:  NewEmbeddingMemo(maxEntries),
	}
}

func (p *MemoizedProvider) Name() string { return p.inner.Name() }
func (p *MemoizedProvider) Dims() int    { return p.inner.Dims() }

func (p *MemoizedProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if vec, ok := p.memo.Get(text); ok {
		return vec, nil
	}
	vec, err := p.inner.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	p.memo.Put(text, vec)
	return vec, nil
}

func (p *MemoizedProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	var uncached []string
	var uncachedIdx []int
	for i, t := range texts {
		if vec, ok := p.memo.Get(t); ok {
			results[i] = vec
		} else {
			uncached = append(uncached, t)
			uncachedIdx = append(uncachedIdx, i)
		}
	}
	if len(uncached) == 0 {
		return results, nil
	}
	vecs, err := p.inner.EmbedBatch(ctx, uncached)
	if err != nil {
		return nil, err
	}
	for j, vec := range vecs {
		idx := uncachedIdx[j]
		results[idx] = vec
		p.memo.Put(texts[idx], vec)
	}
	return results, nil
}

func (p *MemoizedProvider) EmbedWithMode(ctx context.Context, text string, mode EmbedMode) ([]float32, error) {
	// Mode-aware calls bypass memo since same content may produce different vectors per mode.
	return p.inner.EmbedWithMode(ctx, text, mode)
}

// Memo returns the underlying cache for inspection/testing.
func (p *MemoizedProvider) Memo() *EmbeddingMemo { return p.memo }
