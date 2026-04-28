// Package embeddings provides a pluggable embedding provider interface.
// Supported: OpenAI, Voyage AI, and a local stub (returns hash-based pseudo-vectors).
package embeddings

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
)

// Provider generates vector embeddings for text.
type Provider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dims() int
	Name() string
}

// --- OpenAI ---

type openAI struct {
	apiKey string
	model  string
	dims   int
}

// NewOpenAI creates an OpenAI embedding provider.
// model: "text-embedding-3-small" (1536 dims) or "text-embedding-3-large" (3072 dims)
func NewOpenAI(apiKey, model string) Provider {
	dims := 1536
	if model == "text-embedding-3-large" {
		dims = 3072
	}
	return &openAI{apiKey: apiKey, model: model, dims: dims}
}

func (p *openAI) Name() string { return "openai:" + p.model }
func (p *openAI) Dims() int    { return p.dims }

func (p *openAI) Embed(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]any{
		"input": text,
		"model": p.model,
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Error != nil {
		return nil, fmt.Errorf("openai: %s", result.Error.Message)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("openai: empty response")
	}
	return result.Data[0].Embedding, nil
}

// --- Voyage AI ---

type voyage struct {
	apiKey string
	model  string
}

// NewVoyage creates a Voyage AI embedding provider.
// model: "voyage-code-3" (optimized for code)
func NewVoyage(apiKey, model string) Provider {
	if model == "" {
		model = "voyage-code-3"
	}
	return &voyage{apiKey: apiKey, model: model}
}

func (p *voyage) Name() string { return "voyage:" + p.model }
func (p *voyage) Dims() int    { return 1024 }

func (p *voyage) Embed(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]any{
		"input": []string{text},
		"model": p.model,
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.voyageai.com/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("voyage: empty response")
	}
	return result.Data[0].Embedding, nil
}

// --- Local stub (hash-based pseudo-vectors, no API needed) ---

type localStub struct{}

// NewLocal returns a local stub provider that generates deterministic
// pseudo-vectors from SHA-256 hashes. Useful for testing and offline use.
// NOT semantically meaningful — use OpenAI/Voyage for real semantic search.
func NewLocal() Provider { return &localStub{} }

func (p *localStub) Name() string { return "local:stub" }
func (p *localStub) Dims() int    { return 128 }

func (p *localStub) Embed(_ context.Context, text string) ([]float32, error) {
	h := sha256.Sum256([]byte(text))
	vec := make([]float32, 128)
	for i := 0; i < 128; i++ {
		// Use hash bytes cyclically, normalize to [-1, 1]
		vec[i] = float32(int8(h[i%32])) / 128.0
	}
	return normalize(vec), nil
}

// --- Helpers ---

// Cosine returns the cosine similarity between two vectors.
func Cosine(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

func normalize(v []float32) []float32 {
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out
}
