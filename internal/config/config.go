package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds all Yaad configuration.
type Config struct {
	Server     ServerConfig     `toml:"server"`
	Memory     MemoryConfig     `toml:"memory"`
	Search     SearchConfig     `toml:"search"`
	Embeddings EmbeddingsConfig `toml:"embeddings"`
	Decay      DecayConfig      `toml:"decay"`
	Git        GitConfig        `toml:"git"`
	LLM        LLMConfig        `toml:"llm"`
}

type ServerConfig struct {
	Port     int    `toml:"port"`
	Host     string `toml:"host"`
	TLS      bool   `toml:"tls"`
	CertFile string `toml:"cert_file"`
	KeyFile  string `toml:"key_file"`
}

type MemoryConfig struct {
	HotTokenBudget  int `toml:"hot_token_budget"`
	WarmTokenBudget int `toml:"warm_token_budget"`
	MaxMemories     int `toml:"max_memories"`
}

type SearchConfig struct {
	BM25Weight   float64 `toml:"bm25_weight"`
	VectorWeight float64 `toml:"vector_weight"`
	DefaultLimit int     `toml:"default_limit"`
}

type EmbeddingsConfig struct {
	Enabled  bool   `toml:"enabled"`
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
}

type DecayConfig struct {
	Enabled       bool    `toml:"enabled"`
	HalfLifeDays  int     `toml:"half_life_days"`
	MinConfidence float64 `toml:"min_confidence"`
	BoostOnAccess float64 `toml:"boost_on_access"`
}

type GitConfig struct {
	Watch     bool `toml:"watch"`
	AutoStale bool `toml:"auto_stale"`
}

// LLMConfig is optional. Yaad is a memory layer — it does NOT call LLMs directly.
// This config is reserved for future summarization hooks.
type LLMConfig struct {
	Enabled   bool   `toml:"enabled"`
	Provider  string `toml:"provider"`
	Model     string `toml:"model"`
	APIKeyEnv string `toml:"api_key_env"`
}

func Default() *Config {
	return &Config{
		Server:     ServerConfig{Port: 3456, Host: "127.0.0.1"},
		Memory:     MemoryConfig{HotTokenBudget: 800, WarmTokenBudget: 800, MaxMemories: 10000},
		Search:     SearchConfig{BM25Weight: 0.5, VectorWeight: 0.5, DefaultLimit: 10},
		Embeddings: EmbeddingsConfig{Provider: "local", Model: "all-MiniLM-L6-v2"},
		Decay:      DecayConfig{Enabled: true, HalfLifeDays: 30, MinConfidence: 0.1, BoostOnAccess: 0.2},
		Git:        GitConfig{Watch: true, AutoStale: true},
		LLM:        LLMConfig{Provider: "openai", Model: "gpt-4.1-mini", APIKeyEnv: "OPENAI_API_KEY"},
	}
}

func Load(projectDir string) (*Config, error) {
	cfg := Default()

	// Global config
	home, err := os.UserHomeDir()
	if err == nil {
		if err := loadFile(filepath.Join(home, ".yaad", "config.toml"), cfg); err != nil {
			return nil, err
		}
	}

	// Project config (overrides global)
	if projectDir != "" {
		if err := loadFile(filepath.Join(projectDir, ".yaad", "config.toml"), cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func loadFile(path string, cfg *Config) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil // no file to load, not an error
		}
		return err
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return fmt.Errorf("invalid config %s: %w", path, err)
	}
	return nil
}
