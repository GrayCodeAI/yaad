package hooks

import (
	"strings"
)

// Relevance scores for auto-capture filtering.
// Observations below the threshold are dropped to prevent graph pollution.
const relevanceThreshold = 0.3

// ScoreRelevance determines if a tool observation is worth storing.
// Returns a score 0.0-1.0 where higher = more worth remembering.
// No LLM needed — heuristic-based on content signals.
func ScoreRelevance(toolName, input, output, toolError string) float64 {
	score := 0.0

	// Errors are almost always worth remembering (debugging patterns)
	if toolError != "" {
		return 0.9
	}

	// Tool-specific base scores
	switch toolName {
	case "Write", "Edit", "MultiEdit":
		score = 0.7 // file modifications are high-signal
	case "Bash", "Computer":
		score = bashRelevance(input, output)
	case "Read", "Glob", "Grep":
		score = 0.2 // reads are low-signal unless they reveal something
	default:
		score = 0.4
	}

	// Boost for content signals that indicate decisions/conventions
	content := strings.ToLower(input + " " + output)
	if containsDecisionSignal(content) {
		score += 0.3
	}
	if containsConventionSignal(content) {
		score += 0.2
	}

	// Reduce noise: very short outputs are likely navigation, not knowledge
	if len(output) < 20 && len(input) < 20 {
		score -= 0.3
	}

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}
	if score < 0.0 {
		score = 0.0
	}
	return score
}

// ShouldCapture returns true if the observation passes the relevance threshold.
func ShouldCapture(toolName, input, output, toolError string) bool {
	return ScoreRelevance(toolName, input, output, toolError) >= relevanceThreshold
}

func bashRelevance(input, output string) float64 {
	lower := strings.ToLower(input)

	// High signal: install/config/deploy commands
	if containsAny(lower, "install", "config", "deploy", "migrate", "build", "init") {
		return 0.7
	}
	// Medium signal: test/lint results
	if containsAny(lower, "test", "lint", "check", "fmt") {
		return 0.5
	}
	// Low signal: navigation (ls, cd, pwd, cat, head)
	if containsAny(lower, "ls ", "cd ", "pwd", "cat ", "head ", "tail ", "echo ") {
		return 0.1
	}
	// Medium: git commands (decisions about code)
	if containsAny(lower, "git commit", "git merge", "git rebase", "git checkout -b") {
		return 0.6
	}
	return 0.4
}

func containsDecisionSignal(content string) bool {
	return containsAny(content, "decided", "chose", "switched to", "migrated",
		"instead of", "going with", "replaced", "selected", "picking")
}

func containsConventionSignal(content string) bool {
	return containsAny(content, "always", "never", "convention", "standard",
		"must", "required", "enforce", "pattern", "rule")
}

func containsAny(s string, keywords ...string) bool {
	for _, k := range keywords {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}
