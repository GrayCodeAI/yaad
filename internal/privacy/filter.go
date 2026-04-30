package privacy

import (
	"math"
	"regexp"
	"strings"
)

var patterns = []*regexp.Regexp{
	// API keys
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),                    // OpenAI
	regexp.MustCompile(`AKIA[A-Z0-9]{16}`),                        // AWS Access Key
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,}`),             // GitHub tokens
	regexp.MustCompile(`glpat-[a-zA-Z0-9_\-]{20,}`),               // GitLab PAT
	regexp.MustCompile(`[sr]g[a-zA-Z0-9]{20,}`),                   // SendGrid
	// JWT / tokens
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]*`),
	// Auth headers
	regexp.MustCompile(`Bearer\s+[A-Za-z0-9_.\-]+`),
	regexp.MustCompile(`Basic\s+[A-Za-z0-9+/=]{20,}`),
	regexp.MustCompile(`(?i)(PASSWORD|SECRET|KEY|TOKEN|API_KEY)\s*=\s*\S+`),
	// Private keys
	regexp.MustCompile(`-----BEGIN\s+\w+\s+PRIVATE\s+KEY-----[\s\S]*?-----END\s+\w+\s+PRIVATE\s+KEY-----`),
	// Connection strings with passwords
	regexp.MustCompile(`(?i)(postgres|mysql|mongodb)://[^\s"]+:[^\s"]+@[^\s"]+`),
	// Generic high-entropy tokens (hex/base64 strings that look like secrets)
	regexp.MustCompile(`[a-f0-9]{64}`), // SHA-256 hex
}

// Filter replaces secrets in content with [REDACTED].
func Filter(content string) string {
	for _, p := range patterns {
		content = p.ReplaceAllString(content, "[REDACTED]")
	}
	return content
}

// hasHighEntropy returns true if a string has Shannon entropy above threshold.
// Useful for detecting random tokens that regexes might miss.
func hasHighEntropy(s string, threshold float64) bool {
	if len(s) < 20 {
		return false
	}
	freq := make(map[rune]int)
	for _, r := range s {
		freq[r]++
	}
	var entropy float64
	length := float64(len(s))
	for _, count := range freq {
		p := float64(count) / length
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	return entropy >= threshold
}



// IsLikelySecret heuristically determines if a token string is a secret.
func IsLikelySecret(token string) bool {
	if len(token) < 16 {
		return false
	}
	// High ratio of non-alphanumeric characters suggests randomness
	special := 0
	for _, r := range token {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			special++
		}
	}
	ratio := float64(special) / float64(len(token))
	// Base64 and hex have low special-char ratios but high entropy
	if ratio > 0.1 && ratio < 0.4 {
		return true
	}
	// All-uppercase or all-lowercase long strings are likely random
	if strings.ToUpper(token) == token || strings.ToLower(token) == token {
		if len(token) >= 32 {
			return true
		}
	}
	return false
}
