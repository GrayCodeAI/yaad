// Package bench implements a LongMemEval-style evaluation harness for Yaad.
// It measures retrieval accuracy (R@K), MRR, and token efficiency.
package bench

import (
	"fmt"
	"strings"
	"time"

	"github.com/yaadmemory/yaad/internal/engine"
	"github.com/yaadmemory/yaad/internal/storage"
)

// QA is a single question-answer pair for evaluation.
type QA struct {
	Question       string
	ExpectedNodeID string // ID of the node that should be retrieved
	ExpectedContent string // or match by content substring
}

// Result holds evaluation metrics.
type Result struct {
	Total     int
	HitAtK    map[int]int // hits at K=1,3,5,10
	MRR       float64
	AvgTokens float64
	Duration  time.Duration
}

// String formats the result as a readable report.
func (r *Result) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Benchmark Results (%d questions)\n", r.Total))
	sb.WriteString(fmt.Sprintf("  R@1:  %.1f%%\n", float64(r.HitAtK[1])/float64(r.Total)*100))
	sb.WriteString(fmt.Sprintf("  R@3:  %.1f%%\n", float64(r.HitAtK[3])/float64(r.Total)*100))
	sb.WriteString(fmt.Sprintf("  R@5:  %.1f%%\n", float64(r.HitAtK[5])/float64(r.Total)*100))
	sb.WriteString(fmt.Sprintf("  R@10: %.1f%%\n", float64(r.HitAtK[10])/float64(r.Total)*100))
	sb.WriteString(fmt.Sprintf("  MRR:  %.3f\n", r.MRR))
	sb.WriteString(fmt.Sprintf("  Avg tokens/query: %.0f\n", r.AvgTokens))
	sb.WriteString(fmt.Sprintf("  Duration: %s\n", r.Duration))
	return sb.String()
}

// Run evaluates retrieval accuracy on a set of QA pairs.
func Run(eng *engine.Engine, qas []QA, depth, limit int) *Result {
	result := &Result{
		Total:  len(qas),
		HitAtK: map[int]int{1: 0, 3: 0, 5: 0, 10: 0},
	}
	start := time.Now()
	totalTokens := 0.0
	mrrSum := 0.0

	for _, qa := range qas {
		nodes, err := eng.Recall(engine.RecallOpts{
			Query: qa.Question,
			Depth: depth,
			Limit: limit,
		})
		if err != nil {
			continue
		}

		// Count tokens (approximate)
		for _, n := range nodes.Nodes {
			totalTokens += float64(len(n.Content)) / 4.0
		}

		// Find rank of expected answer
		rank := findRank(nodes.Nodes, qa)
		if rank > 0 {
			mrrSum += 1.0 / float64(rank)
			for _, k := range []int{1, 3, 5, 10} {
				if rank <= k {
					result.HitAtK[k]++
				}
			}
		}
	}

	result.MRR = mrrSum / float64(len(qas))
	result.AvgTokens = totalTokens / float64(len(qas))
	result.Duration = time.Since(start)
	return result
}

func findRank(nodes []*storage.Node, qa QA) int {
	for i, n := range nodes {
		if qa.ExpectedNodeID != "" && n.ID == qa.ExpectedNodeID {
			return i + 1
		}
		if qa.ExpectedContent != "" && strings.Contains(
			strings.ToLower(n.Content),
			strings.ToLower(qa.ExpectedContent)) {
			return i + 1
		}
	}
	return 0
}

// DefaultQAs returns a built-in set of coding-agent memory QA pairs for smoke testing.
func DefaultQAs() []QA {
	return []QA{
		{Question: "which JWT library should I use", ExpectedContent: "jose"},
		{Question: "how to run tests", ExpectedContent: "test"},
		{Question: "auth middleware bug", ExpectedContent: "auth"},
		{Question: "architecture decision event bus", ExpectedContent: "NATS"},
		{Question: "token refresh issue", ExpectedContent: "refresh"},
	}
}
