package embeddings

import "testing"

func TestGetModelDefaults_KnownModel(t *testing.T) {
	d, ok := GetModelDefaults("voyage-code-3")
	if !ok {
		t.Fatal("expected voyage-code-3 to be in defaults table")
	}
	if d.Dimensions != 1024 {
		t.Errorf("expected 1024 dimensions, got %d", d.Dimensions)
	}
	if d.IndexInputType != "document" {
		t.Errorf("expected IndexInputType=document, got %q", d.IndexInputType)
	}
	if d.MaxBatchSize != 128 {
		t.Errorf("expected MaxBatchSize=128, got %d", d.MaxBatchSize)
	}
}

func TestGetModelDefaults_UnknownModel(t *testing.T) {
	_, ok := GetModelDefaults("nonexistent-model-xyz")
	if ok {
		t.Error("expected unknown model to return false")
	}
}

func TestGetInputType_AndBatchSize(t *testing.T) {
	// Test input type for asymmetric model
	idx := GetInputType("text-embedding-004", ModeDocument)
	if idx != "RETRIEVAL_DOCUMENT" {
		t.Errorf("expected RETRIEVAL_DOCUMENT for document mode, got %q", idx)
	}
	qry := GetInputType("text-embedding-004", ModeQuery)
	if qry != "RETRIEVAL_QUERY" {
		t.Errorf("expected RETRIEVAL_QUERY for query mode, got %q", qry)
	}

	// OpenAI has no input types
	empty := GetInputType("text-embedding-3-small", ModeDocument)
	if empty != "" {
		t.Errorf("expected empty input type for OpenAI model, got %q", empty)
	}

	// Unknown model returns empty
	unknown := GetInputType("unknown-model", ModeQuery)
	if unknown != "" {
		t.Errorf("expected empty input type for unknown model, got %q", unknown)
	}

	// Batch sizes
	bs := GetMaxBatchSize("text-embedding-3-small")
	if bs != 2048 {
		t.Errorf("expected 2048, got %d", bs)
	}
	bsDefault := GetMaxBatchSize("unknown-model")
	if bsDefault != 64 {
		t.Errorf("expected default 64, got %d", bsDefault)
	}
}
