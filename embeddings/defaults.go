package embeddings

// ModelDefaults maps model names to their optimal embedding parameters.
type ModelDefaults struct {
	IndexInputType string // e.g., "search_document", "RETRIEVAL_DOCUMENT"
	QueryInputType string // e.g., "search_query", "RETRIEVAL_QUERY"
	Dimensions     int    // output dimensions
	MaxBatchSize   int    // max texts per batch request
}

var defaultsTable = map[string]ModelDefaults{
	// Voyage
	"voyage-code-3": {IndexInputType: "document", QueryInputType: "query", Dimensions: 1024, MaxBatchSize: 128},
	"voyage-3":      {IndexInputType: "document", QueryInputType: "query", Dimensions: 1024, MaxBatchSize: 128},
	"voyage-3-lite": {IndexInputType: "document", QueryInputType: "query", Dimensions: 512, MaxBatchSize: 128},
	// Cohere
	"embed-english-v3.0":      {IndexInputType: "search_document", QueryInputType: "search_query", Dimensions: 1024, MaxBatchSize: 96},
	"embed-multilingual-v3.0": {IndexInputType: "search_document", QueryInputType: "search_query", Dimensions: 1024, MaxBatchSize: 96},
	// OpenAI
	"text-embedding-3-small": {Dimensions: 1536, MaxBatchSize: 2048},
	"text-embedding-3-large": {Dimensions: 3072, MaxBatchSize: 2048},
	"text-embedding-ada-002": {Dimensions: 1536, MaxBatchSize: 2048},
	// Google/Gemini
	"text-embedding-004": {IndexInputType: "RETRIEVAL_DOCUMENT", QueryInputType: "RETRIEVAL_QUERY", Dimensions: 768, MaxBatchSize: 100},
	"text-embedding-005": {IndexInputType: "RETRIEVAL_DOCUMENT", QueryInputType: "RETRIEVAL_QUERY", Dimensions: 768, MaxBatchSize: 100},
	// Nvidia
	"NV-Embed-QA": {IndexInputType: "passage", QueryInputType: "query", Dimensions: 4096, MaxBatchSize: 50},
	// Snowflake
	"snowflake-arctic-embed-xs": {Dimensions: 384, MaxBatchSize: 64},
	"snowflake-arctic-embed-m":  {Dimensions: 768, MaxBatchSize: 64},
}

// GetModelDefaults returns the defaults for a known model. The second return
// value is false when the model is not in the table.
func GetModelDefaults(model string) (ModelDefaults, bool) {
	d, ok := defaultsTable[model]
	return d, ok
}

// GetInputType returns the appropriate input_type string for the given model
// and embed mode. If the model has no asymmetric input types, an empty string
// is returned.
func GetInputType(model string, mode EmbedMode) string {
	d, ok := defaultsTable[model]
	if !ok {
		return ""
	}
	if mode == ModeQuery {
		return d.QueryInputType
	}
	return d.IndexInputType
}

// GetMaxBatchSize returns the maximum batch size for a model, or 64 as the
// default when the model is unknown.
func GetMaxBatchSize(model string) int {
	d, ok := defaultsTable[model]
	if !ok {
		return 64
	}
	if d.MaxBatchSize <= 0 {
		return 64
	}
	return d.MaxBatchSize
}
