package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/yaad/config"
	"github.com/GrayCodeAI/yaad/engine"
	"github.com/GrayCodeAI/yaad/graph"
	"github.com/GrayCodeAI/yaad/storage"
)

// projectDir returns the current working directory.
func projectDir() string {
	dir, _ := os.Getwd()
	return dir
}

// dbPath returns the path to the project's yaad database.
func dbPath() string {
	return filepath.Join(projectDir(), ".yaad", "yaad.db")
}

// loadConfig loads config from .yaad/config.toml (falls back to defaults).
func loadConfig() *config.Config {
	cfg, err := config.Load(projectDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "yaad: warning: config load failed, using defaults: %v\n", err)
		return config.Default()
	}
	return cfg
}

// openEngine opens the yaad database and returns an engine.
// Exits on error — CLI commands should not continue without a DB.
func openEngine() *engine.Engine {
	path := dbPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "error: no yaad project found in %s\n", filepath.Dir(path))
		fmt.Fprintf(os.Stderr, "Run 'yaad init' to initialize a project.\n")
		os.Exit(1)
	}
	store, err := storage.NewStore(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	cfg := loadConfig()
	eng := engine.New(store, graph.New(store, store.DB()))
	eng.DecayConfig = engine.DecayConfig{
		HalfLifeDays:  float64(cfg.Decay.HalfLifeDays),
		MinConfidence: cfg.Decay.MinConfidence,
		BoostOnAccess: cfg.Decay.BoostOnAccess,
	}
	return eng
}

// printJSON prints a value as indented JSON to stdout.
func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

// truncate shortens a string for display, replacing newlines with spaces.
func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
