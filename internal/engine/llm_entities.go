package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// LLMExtractor uses an LLM to extract entities from content.
// Falls back to regex extraction if LLM is unavailable.
type LLMExtractor struct {
	apiKey  string
	baseURL string
	model   string
}

// NewLLMExtractor creates an LLM-based entity extractor.
// baseURL: "https://api.openai.com" or any OpenAI-compatible endpoint.
func NewLLMExtractor(apiKey, baseURL, model string) *LLMExtractor {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	if model == "" {
		model = "gpt-4.1-mini"
	}
	return &LLMExtractor{apiKey: apiKey, baseURL: baseURL, model: model}
}

// Extract returns entities from content using the LLM.
// Returns regex-extracted entities if LLM call fails.
func (e *LLMExtractor) Extract(ctx context.Context, content string) []Entity {
	entities, err := e.llmExtract(ctx, content)
	if err != nil {
		// Fallback to regex
		return ExtractEntities(content)
	}
	return entities
}

func (e *LLMExtractor) llmExtract(ctx context.Context, content string) ([]Entity, error) {
	prompt := fmt.Sprintf(`Extract technical entities from this text. Return JSON array of {"name": "...", "type": "file|entity"}.
Only extract: file paths, library names, function names, class names, service names.
Text: %s`, content)

	body, _ := json.Marshal(map[string]any{
		"model": e.model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens":  200,
		"temperature": 0,
	})

	req, _ := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	// Parse JSON from response
	raw := result.Choices[0].Message.Content
	// Find JSON array in response
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end < 0 {
		return nil, fmt.Errorf("no JSON array in response")
	}

	var entities []Entity
	if err := json.Unmarshal([]byte(raw[start:end+1]), &entities); err != nil {
		return nil, err
	}
	return entities, nil
}
