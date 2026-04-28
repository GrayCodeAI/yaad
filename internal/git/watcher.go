package git

import (
	"os/exec"
	"strings"
	"time"

	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// StaleReport describes a stale subgraph.
type StaleReport struct {
	File     string
	ChangedAt time.Time
	NodeIDs  []string
}

// Watcher detects stale memories by checking git history.
type Watcher struct {
	store *storage.Store
	graph *graph.Graph
	dir   string
}

// New creates a git Watcher for the given project directory.
func New(store *storage.Store, g *graph.Graph, dir string) *Watcher {
	return &Watcher{store: store, graph: g, dir: dir}
}

// StalesSince returns stale reports for files changed since the given time.
func (w *Watcher) StalesSince(since time.Time) ([]StaleReport, error) {
	files, err := w.changedFiles(since)
	if err != nil {
		return nil, err
	}

	var reports []StaleReport
	for _, file := range files {
		ids, err := w.graph.Impact(file, 3)
		if err != nil || len(ids) == 0 {
			continue
		}
		reports = append(reports, StaleReport{
			File:    file,
			NodeIDs: ids,
		})
	}
	return reports, nil
}

// changedFiles returns files changed since the given time via git log.
func (w *Watcher) changedFiles(since time.Time) ([]string, error) {
	sinceStr := since.UTC().Format("2006-01-02T15:04:05")
	out, err := exec.Command("git", "-C", w.dir, "log",
		"--since="+sinceStr, "--name-only", "--pretty=format:").Output()
	if err != nil {
		return nil, err // not a git repo or git not installed
	}
	seen := map[string]bool{}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		files = append(files, line)
	}
	return files, nil
}

// WatchFile registers a file→node mapping for staleness tracking.
func (w *Watcher) WatchFile(filePath, nodeID, gitHash string) error {
	return w.store.AddFileWatch(filePath, nodeID, gitHash)
}

// CurrentHash returns the current git HEAD hash.
func CurrentHash(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
