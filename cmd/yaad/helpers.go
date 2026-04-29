package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// dbPath returns the path to the project's yaad database.
func dbPath() string {
	dir, _ := os.Getwd()
	return filepath.Join(dir, ".yaad", "yaad.db")
}

// openEngine opens the yaad database and returns an engine.
// Exits on error — CLI commands should not continue without a DB.
func openEngine() *engine.Engine {
	if err := os.MkdirAll(filepath.Dir(dbPath()), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating .yaad/: %v\n", err)
		os.Exit(1)
	}
	store, err := storage.NewStore(dbPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	return engine.New(store, graph.New(store))
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
