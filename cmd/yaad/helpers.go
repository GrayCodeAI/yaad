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
	return engine.New(store, graph.New(store, store.DB()))
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
