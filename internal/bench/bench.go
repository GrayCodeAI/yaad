// Package bench implements a LongMemEval-style evaluation harness for Yaad.
// It measures retrieval accuracy (R@K), MRR, and token efficiency.
package bench

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/storage"
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
func Run(ctx context.Context, eng *engine.Engine, qas []QA, depth, limit int) *Result {
	result := &Result{
		Total:  len(qas),
		HitAtK: map[int]int{1: 0, 3: 0, 5: 0, 10: 0},
	}
	if len(qas) == 0 {
		return result
	}
	start := time.Now()
	totalTokens := 0.0
	mrrSum := 0.0

	for _, qa := range qas {
		nodes, err := eng.Recall(ctx, engine.RecallOpts{
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

// DefaultQAs returns a built-in set of coding-agent memory QA pairs.
// Covers the same categories as LongMemEval: single-hop, multi-hop, temporal, preference.
func DefaultQAs() []QA {
	return []QA{
		// Single-hop: direct fact retrieval
		{Question: "which JWT library should I use", ExpectedContent: "jose"},
		{Question: "how to run tests", ExpectedContent: "test"},
		{Question: "what is the auth middleware bug", ExpectedContent: "auth"},
		{Question: "which event bus did we choose", ExpectedContent: "NATS"},
		{Question: "token refresh issue", ExpectedContent: "refresh"},
		{Question: "what JWT algorithm for compliance", ExpectedContent: "RS256"},
		{Question: "database query performance bug", ExpectedContent: "DataLoader"},
		{Question: "NATS connection issue", ExpectedContent: "keepalive"},
		{Question: "auth subsystem spec", ExpectedContent: "jose"},
		{Question: "rate limiting task", ExpectedContent: "rate"},

		// Multi-hop: requires traversing edges
		{Question: "why did we choose NATS", ExpectedContent: "backpressure"},
		{Question: "what caused the token refresh race", ExpectedContent: "mutex"},
		{Question: "which library is used for JWT compliance", ExpectedContent: "jose"},
		{Question: "what decision led to the jose convention", ExpectedContent: "jose"},

		// Temporal: recency-aware
		{Question: "what was the last architecture decision", ExpectedContent: "NATS"},
		{Question: "recent bug patterns in auth", ExpectedContent: "auth"},

		// Preference: user-specific
		{Question: "what testing framework do we use", ExpectedContent: "pnpm"},
		{Question: "what are the coding conventions", ExpectedContent: "jose"},
	}
}

// CodingBenchQAs returns an extended set of 50 coding-specific QA pairs
// for more rigorous evaluation. Seed your DB with realistic coding memories first.
func CodingBenchQAs() []QA {
	base := DefaultQAs()
	extended := []QA{
		{Question: "TypeScript export style", ExpectedContent: "Named exports"},
		{Question: "integration tests for auth", ExpectedContent: "integration"},
		{Question: "what is the access token expiry", ExpectedContent: "15min"},
		{Question: "refresh token duration", ExpectedContent: "7d"},
		{Question: "N+1 query fix", ExpectedContent: "DataLoader"},
		{Question: "event bus backpressure solution", ExpectedContent: "NATS"},
		{Question: "JWT signing key type", ExpectedContent: "RS256"},
		{Question: "auth middleware file location", ExpectedContent: "auth.ts"},
		{Question: "test coverage command", ExpectedContent: "coverage"},
		{Question: "functional programming preference", ExpectedContent: "functional"},
	}
	return append(base, extended...)
}
