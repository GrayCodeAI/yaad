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
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// EmbedMode specifies whether a text is a document or a query for
// asymmetric embedding models.
type EmbedMode int

const (
	// ModeDocument indicates the text is a document to be stored.
	ModeDocument EmbedMode = iota
	// ModeQuery indicates the text is a search query.
	ModeQuery
)

// Provider generates vector embeddings for text.
type Provider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	EmbedWithMode(ctx context.Context, text string, mode EmbedMode) ([]float32, error)
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
	body, err := json.Marshal(map[string]any{
		"input": text,
		"model": p.model,
	})
	if err != nil {
		return nil, fmt.Errorf("openai: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
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
		errMsg := result.Error.Message
		if d := ExtractRetryDelay(errMsg); d > 0 {
			time.Sleep(d)
			return p.Embed(ctx, text)
		}
		return nil, fmt.Errorf("openai: %s", errMsg)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("openai: empty response")
	}
	return result.Data[0].Embedding, nil
}

func (p *openAI) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{
		"input": texts,
		"model": p.model,
	})
	if err != nil {
		return nil, fmt.Errorf("openai: marshal batch: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Error != nil {
		errMsg := result.Error.Message
		if d := ExtractRetryDelay(errMsg); d > 0 {
			time.Sleep(d)
			return p.EmbedBatch(ctx, texts)
		}
		return nil, fmt.Errorf("openai: %s", errMsg)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("openai: empty batch response")
	}
	// Results may come back out of order; sort by index
	out := make([][]float32, len(texts))
	for _, d := range result.Data {
		if d.Index < len(out) {
			out[d.Index] = d.Embedding
		}
	}
	return out, nil
}

// EmbedWithMode for OpenAI ignores the mode (no asymmetric support).
func (p *openAI) EmbedWithMode(ctx context.Context, text string, _ EmbedMode) ([]float32, error) {
	return p.Embed(ctx, text)
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
	body, err := json.Marshal(map[string]any{
		"input": []string{text},
		"model": p.model,
	})
	if err != nil {
		return nil, fmt.Errorf("voyage: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.voyageai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("voyage: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errBody struct {
			Detail string `json:"detail"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		errMsg := fmt.Sprintf("voyage: API returned status %d: %s", resp.StatusCode, errBody.Detail)
		if d := ExtractRetryDelay(errMsg); d > 0 {
			time.Sleep(d)
			return p.Embed(ctx, text)
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

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

func (p *voyage) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{
		"input": texts,
		"model": p.model,
	})
	if err != nil {
		return nil, fmt.Errorf("voyage: marshal batch: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.voyageai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("voyage: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errBody struct {
			Detail string `json:"detail"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		errMsg := fmt.Sprintf("voyage: API returned status %d: %s", resp.StatusCode, errBody.Detail)
		if d := ExtractRetryDelay(errMsg); d > 0 {
			time.Sleep(d)
			return p.EmbedBatch(ctx, texts)
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	out := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

// EmbedWithMode for Voyage uses input_type for asymmetric embeddings.
func (p *voyage) EmbedWithMode(ctx context.Context, text string, mode EmbedMode) ([]float32, error) {
	inputType := "search_document"
	if mode == ModeQuery {
		inputType = "search_query"
	}
	body, err := json.Marshal(map[string]any{
		"input":      []string{text},
		"model":      p.model,
		"input_type": inputType,
	})
	if err != nil {
		return nil, fmt.Errorf("voyage: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.voyageai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("voyage: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errBody struct {
			Detail string `json:"detail"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		errMsg := fmt.Sprintf("voyage: API returned status %d: %s", resp.StatusCode, errBody.Detail)
		if d := ExtractRetryDelay(errMsg); d > 0 {
			time.Sleep(d)
			return p.EmbedWithMode(ctx, text, mode)
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

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

func (p *localStub) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		vec, err := p.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = vec
	}
	return out, nil
}

// EmbedWithMode for localStub ignores the mode.
func (p *localStub) EmbedWithMode(ctx context.Context, text string, _ EmbedMode) ([]float32, error) {
	return p.Embed(ctx, text)
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
