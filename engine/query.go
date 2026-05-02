package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/GrayCodeAI/yaad/intent"
	"github.com/GrayCodeAI/yaad/storage"
)

// queryConfidenceThreshold is the minimum node confidence required to include
// a node in a query answer. Nodes below this threshold are filtered out.
const queryConfidenceThreshold = 0.3

// queryRetrievalLimit is the number of candidate nodes to retrieve from
// FusedRecall before filtering and synthesis.
const queryRetrievalLimit = 20

// QueryResult holds the answer to a natural language query.
type QueryResult struct {
	Answer     string          // synthesized answer from retrieved memories
	Sources    []*storage.Node // the memories used to form the answer
	Confidence float64         // 0-1, based on relevance scores of retrieved nodes
}

// Query answers a natural language question by retrieving relevant memories
// and synthesizing an answer from them. No LLM required — uses template-based
// synthesis from high-confidence retrieved nodes.
//
// How it works:
//  1. Parse the question — classify intent using the existing intent package
//  2. Retrieve — call FusedRecall with the question as the query
//  3. Rank and filter — keep only nodes with confidence > 0.3
//  4. Synthesize — format retrieved memories into a coherent answer using
//     intent-specific templates
//  5. Calculate confidence — average confidence of the used nodes
func (e *Engine) Query(ctx context.Context, question string, project string) (*QueryResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(question) == "" {
		return nil, fmt.Errorf("question cannot be empty")
	}

	// Step 1: Classify intent
	queryIntent := intent.Classify(question)

	// Step 2: Retrieve candidates via FusedRecall
	recallResult, err := e.FusedRecall(ctx, RecallOpts{
		Query:   question,
		Limit:   queryRetrievalLimit,
		Project: project,
	})
	if err != nil {
		return nil, fmt.Errorf("query retrieval failed: %w", err)
	}

	// Step 3: Filter by confidence threshold
	var filtered []*storage.Node
	for _, n := range recallResult.Nodes {
		if n.Confidence >= queryConfidenceThreshold {
			filtered = append(filtered, n)
		}
	}

	// Empty results
	if len(filtered) == 0 {
		return &QueryResult{
			Answer:     fmt.Sprintf("No relevant memories found for: %q", question),
			Sources:    nil,
			Confidence: 0,
		}, nil
	}

	// Step 4: Synthesize answer based on intent
	answer := synthesizeAnswer(queryIntent, question, filtered, recallResult.Edges)

	// Step 5: Calculate confidence as average of source node confidences
	confidence := averageConfidence(filtered)

	return &QueryResult{
		Answer:     answer,
		Sources:    filtered,
		Confidence: confidence,
	}, nil
}

// synthesizeAnswer formats retrieved memories into a coherent answer using
// intent-specific templates. Each intent type produces a different structure
// optimized for the kind of information requested.
func synthesizeAnswer(qi intent.Intent, question string, nodes []*storage.Node, edges []*storage.Edge) string {
	switch qi {
	case intent.IntentWhat:
		return synthesizeWhat(question, nodes)
	case intent.IntentWhy:
		return synthesizeWhy(question, nodes, edges)
	case intent.IntentWhen:
		return synthesizeWhen(question, nodes)
	case intent.IntentHow:
		return synthesizeHow(question, nodes)
	case intent.IntentWho:
		return synthesizeWho(question, nodes)
	default:
		return synthesizeGeneral(question, nodes)
	}
}

// synthesizeWhat formats an answer for "What" questions by listing relevant
// facts and decisions.
func synthesizeWhat(question string, nodes []*storage.Node) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Regarding %q, here is what was found:\n\n", question))

	// Group by node type for structured output
	grouped := groupByType(nodes)
	order := []string{"decision", "convention", "spec", "preference", "bug", "task", "skill", "entity", "file", "session"}
	for _, typ := range order {
		group, ok := grouped[typ]
		if !ok {
			continue
		}
		b.WriteString(fmt.Sprintf("[%s]\n", strings.Title(typ)))
		for _, n := range group {
			b.WriteString(formatNodeBullet(n))
		}
		b.WriteString("\n")
	}

	// Any types not in the order list
	for typ, group := range grouped {
		if containsStr(order, typ) {
			continue
		}
		if typ == "" {
			b.WriteString("[Other]\n")
		} else {
			b.WriteString(fmt.Sprintf("[%s]\n", strings.Title(typ)))
		}
		for _, n := range group {
			b.WriteString(formatNodeBullet(n))
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// synthesizeWhy formats an answer for "Why" questions by tracing causal chains
// through caused_by and led_to edges.
func synthesizeWhy(question string, nodes []*storage.Node, edges []*storage.Edge) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Regarding %q, here are the reasons found:\n\n", question))

	// Build a node lookup map
	nodeByID := make(map[string]*storage.Node, len(nodes))
	for _, n := range nodes {
		nodeByID[n.ID] = n
	}

	// Find causal edges among the result set
	type causalPair struct {
		from *storage.Node
		to   *storage.Node
		typ  string
	}
	var causalChains []causalPair
	for _, e := range edges {
		if e.Type == "caused_by" || e.Type == "led_to" || e.Type == "supersedes" {
			from, okF := nodeByID[e.FromID]
			to, okT := nodeByID[e.ToID]
			if okF && okT {
				causalChains = append(causalChains, causalPair{from, to, e.Type})
			}
		}
	}

	if len(causalChains) > 0 {
		b.WriteString("Causal chain:\n")
		for _, cp := range causalChains {
			switch cp.typ {
			case "caused_by":
				b.WriteString(fmt.Sprintf("  - %s was caused by: %s\n", nodeSummary(cp.from), nodeSummary(cp.to)))
			case "led_to":
				b.WriteString(fmt.Sprintf("  - %s led to: %s\n", nodeSummary(cp.from), nodeSummary(cp.to)))
			case "supersedes":
				b.WriteString(fmt.Sprintf("  - %s superseded: %s\n", nodeSummary(cp.from), nodeSummary(cp.to)))
			}
		}
		b.WriteString("\n")
	}

	// List all decisions and relevant context
	b.WriteString("Related context:\n")
	for _, n := range nodes {
		b.WriteString(formatNodeBullet(n))
	}

	return strings.TrimRight(b.String(), "\n")
}

// synthesizeWhen formats an answer for "When" questions by sorting memories
// by time and presenting a timeline.
func synthesizeWhen(question string, nodes []*storage.Node) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Timeline for %q:\n\n", question))

	// Sort by creation time (oldest first for timeline)
	sorted := make([]*storage.Node, len(nodes))
	copy(sorted, nodes)
	sort.Slice(sorted, func(i, j int) bool {
		ti := sorted[i].CreatedAt
		if ti.IsZero() {
			ti = sorted[i].UpdatedAt
		}
		tj := sorted[j].CreatedAt
		if tj.IsZero() {
			tj = sorted[j].UpdatedAt
		}
		return ti.Before(tj)
	})

	for _, n := range sorted {
		ts := n.CreatedAt
		if ts.IsZero() {
			ts = n.UpdatedAt
		}
		timeStr := "unknown time"
		if !ts.IsZero() {
			timeStr = ts.Format(time.RFC3339)
		}
		b.WriteString(fmt.Sprintf("  [%s] %s\n", timeStr, nodeSummary(n)))
	}

	return strings.TrimRight(b.String(), "\n")
}

// synthesizeHow formats an answer for "How" questions by listing steps and
// procedures found in memory.
func synthesizeHow(question string, nodes []*storage.Node) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Regarding %q, here are the relevant steps and procedures:\n\n", question))

	// Prioritize spec, convention, and skill nodes (most likely to contain procedures)
	procedural := []string{"spec", "convention", "skill", "task"}
	var primary, secondary []*storage.Node
	for _, n := range nodes {
		if containsStr(procedural, n.Type) {
			primary = append(primary, n)
		} else {
			secondary = append(secondary, n)
		}
	}

	step := 1
	for _, n := range primary {
		b.WriteString(fmt.Sprintf("  %d. [%s] %s\n", step, n.Type, nodeSummary(n)))
		step++
	}
	if len(secondary) > 0 && len(primary) > 0 {
		b.WriteString("\nAdditional context:\n")
	}
	for _, n := range secondary {
		b.WriteString(formatNodeBullet(n))
	}

	return strings.TrimRight(b.String(), "\n")
}

// synthesizeWho formats an answer for "Who" questions by listing entities,
// people, and relevant references.
func synthesizeWho(question string, nodes []*storage.Node) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Regarding %q, here are the relevant entities:\n\n", question))

	// Prioritize entity nodes
	var entities, others []*storage.Node
	for _, n := range nodes {
		if n.Type == "entity" || n.Type == "file" {
			entities = append(entities, n)
		} else {
			others = append(others, n)
		}
	}

	if len(entities) > 0 {
		b.WriteString("Entities:\n")
		for _, n := range entities {
			b.WriteString(formatNodeBullet(n))
		}
		b.WriteString("\n")
	}

	if len(others) > 0 {
		b.WriteString("Related memories:\n")
		for _, n := range others {
			b.WriteString(formatNodeBullet(n))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// synthesizeGeneral formats a general answer when no specific intent is detected.
func synthesizeGeneral(question string, nodes []*storage.Node) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Regarding %q, here is what was found:\n\n", question))

	for _, n := range nodes {
		b.WriteString(formatNodeBullet(n))
	}

	return strings.TrimRight(b.String(), "\n")
}

// --- helpers ---

// formatNodeBullet formats a single node as a bullet point for inclusion in
// synthesized answers.
func formatNodeBullet(n *storage.Node) string {
	summary := nodeSummary(n)
	if n.Type != "" {
		return fmt.Sprintf("  - [%s] %s\n", n.Type, summary)
	}
	return fmt.Sprintf("  - %s\n", summary)
}

// nodeSummary returns the best short description of a node: its Summary field
// if set, otherwise a truncated version of Content.
func nodeSummary(n *storage.Node) string {
	if n.Summary != "" {
		return n.Summary
	}
	content := n.Content
	if len(content) > 120 {
		content = content[:117] + "..."
	}
	return content
}

// groupByType groups nodes by their Type field.
func groupByType(nodes []*storage.Node) map[string][]*storage.Node {
	m := make(map[string][]*storage.Node)
	for _, n := range nodes {
		m[n.Type] = append(m[n.Type], n)
	}
	return m
}

// averageConfidence computes the mean confidence across a slice of nodes.
func averageConfidence(nodes []*storage.Node) float64 {
	if len(nodes) == 0 {
		return 0
	}
	total := 0.0
	for _, n := range nodes {
		total += n.Confidence
	}
	return total / float64(len(nodes))
}

// containsStr checks if a string slice contains a given value.
func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
