// Package proactive implements proactive context preloading for yaad.
// When a trigger fires (file open, git checkout, terminal cd), yaad predicts
// which memory nodes are likely relevant and preloads them before the user
// asks. This reduces perceived latency and keeps the context window warm.
package proactive

import (
	"context"
	"sort"
	"strings"
	"time"
)

// TriggerKind identifies what caused the preload.
type TriggerKind int

const (
	TriggerFileOpen    TriggerKind = iota // editor opened a file
	TriggerGitCheckout                    // switched branches
	TriggerDirChange                      // cd into a directory
	TriggerQuery                          // user started typing a query
	TriggerSessionStart                   // new coding session began
)

// String returns a human-readable label for the trigger kind.
func (tk TriggerKind) String() string {
	switch tk {
	case TriggerFileOpen:
		return "file_open"
	case TriggerGitCheckout:
		return "git_checkout"
	case TriggerDirChange:
		return "dir_change"
	case TriggerQuery:
		return "query"
	case TriggerSessionStart:
		return "session_start"
	default:
		return "unknown"
	}
}

// PreloadTrigger describes the event that should initiate preloading.
type PreloadTrigger struct {
	Kind      TriggerKind // what triggered the preload
	Value     string      // file path, branch name, directory, or query prefix
	Project   string      // the active project
	Timestamp time.Time   // when the trigger fired
}

// PreloadResult holds the set of preloaded memory entries.
type PreloadResult struct {
	NodeIDs  []string      // node IDs predicted to be relevant
	Reason   string        // human-readable explanation of the prediction
	Duration time.Duration // how long the preload took
}

// Searcher is the interface that proactive preloading uses to find
// relevant nodes. Implementations wrap yaad's graph search.
type Searcher interface {
	// Search returns node IDs matching the query, scoped to a project.
	Search(ctx context.Context, query string, project string, limit int) ([]string, error)
}

// Preload predicts and fetches relevant memory nodes based on the trigger.
// It constructs one or more search queries from the trigger, executes them,
// deduplicates the results, and returns the top candidates.
func Preload(ctx context.Context, trigger PreloadTrigger, searcher Searcher, limit int) (*PreloadResult, error) {
	start := time.Now()

	if limit <= 0 {
		limit = 10
	}

	queries := buildQueries(trigger)
	if len(queries) == 0 {
		return &PreloadResult{
			Reason:   "no queries derived from trigger",
			Duration: time.Since(start),
		}, nil
	}

	// Fan out searches and collect results.
	seen := make(map[string]int) // nodeID -> hit count
	for _, q := range queries {
		ids, err := searcher.Search(ctx, q, trigger.Project, limit)
		if err != nil {
			continue // best-effort: skip failed searches
		}
		for _, id := range ids {
			seen[id]++
		}
	}

	// Rank by hit count (nodes found by multiple queries rank higher).
	type scored struct {
		id    string
		score int
	}
	ranked := make([]scored, 0, len(seen))
	for id, count := range seen {
		ranked = append(ranked, scored{id, count})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	// Cap at limit.
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	nodeIDs := make([]string, len(ranked))
	for i, r := range ranked {
		nodeIDs[i] = r.id
	}

	return &PreloadResult{
		NodeIDs:  nodeIDs,
		Reason:   buildReason(trigger, len(queries), len(nodeIDs)),
		Duration: time.Since(start),
	}, nil
}

// buildQueries generates search queries from a trigger.
func buildQueries(trigger PreloadTrigger) []string {
	switch trigger.Kind {
	case TriggerFileOpen:
		return fileOpenQueries(trigger.Value)
	case TriggerGitCheckout:
		return gitCheckoutQueries(trigger.Value)
	case TriggerDirChange:
		return dirChangeQueries(trigger.Value)
	case TriggerQuery:
		return queryPrefixQueries(trigger.Value)
	case TriggerSessionStart:
		return sessionStartQueries(trigger.Project)
	default:
		return nil
	}
}

// fileOpenQueries generates queries for a file-open trigger.
// Example: opening "internal/search/intent.go" produces queries for
// the filename, the directory, and the package.
func fileOpenQueries(path string) []string {
	if path == "" {
		return nil
	}
	queries := []string{path}

	// Add the bare filename.
	parts := strings.Split(path, "/")
	if len(parts) > 1 {
		queries = append(queries, parts[len(parts)-1])
	}

	// Add the parent directory.
	if len(parts) > 2 {
		dir := strings.Join(parts[:len(parts)-1], "/")
		queries = append(queries, dir)
	}

	return queries
}

// gitCheckoutQueries generates queries for a branch-switch trigger.
func gitCheckoutQueries(branch string) []string {
	if branch == "" {
		return nil
	}
	queries := []string{branch}

	// Strip common prefixes like "feature/", "fix/", "chore/".
	if idx := strings.IndexByte(branch, '/'); idx >= 0 {
		queries = append(queries, branch[idx+1:])
	}

	// Turn kebab-case into space-separated words for broader matching.
	words := strings.ReplaceAll(branch, "-", " ")
	words = strings.ReplaceAll(words, "_", " ")
	if words != branch {
		queries = append(queries, words)
	}

	return queries
}

// dirChangeQueries generates queries for a directory-change trigger.
func dirChangeQueries(dir string) []string {
	if dir == "" {
		return nil
	}
	queries := []string{dir}

	// Add the leaf directory name.
	parts := strings.Split(strings.TrimRight(dir, "/"), "/")
	if len(parts) > 1 {
		queries = append(queries, parts[len(parts)-1])
	}

	return queries
}

// queryPrefixQueries generates queries from a partial user query.
func queryPrefixQueries(prefix string) []string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil
	}
	return []string{prefix}
}

// sessionStartQueries generates broad queries for a new session.
func sessionStartQueries(project string) []string {
	if project == "" {
		return nil
	}
	return []string{
		project,
		"recent changes",
		"last session",
	}
}

// buildReason constructs a human-readable explanation of the preload.
func buildReason(trigger PreloadTrigger, queryCount, resultCount int) string {
	var b strings.Builder
	b.WriteString("preloaded ")
	b.WriteString(intToStr(resultCount))
	b.WriteString(" nodes from ")
	b.WriteString(intToStr(queryCount))
	b.WriteString(" queries (trigger: ")
	b.WriteString(trigger.Kind.String())
	if trigger.Value != "" {
		b.WriteString(" ")
		b.WriteString(trigger.Value)
	}
	b.WriteString(")")
	return b.String()
}

// intToStr converts a small int to its string representation without fmt.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + intToStr(-n)
	}
	digits := make([]byte, 0, 4)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	// Reverse.
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}
