package privacy

import "regexp"

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),
	regexp.MustCompile(`AKIA[A-Z0-9]{16}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]*`),
	regexp.MustCompile(`Bearer\s+[A-Za-z0-9_.\-]+`),
	regexp.MustCompile(`(?i)(PASSWORD|SECRET|KEY|TOKEN|API_KEY)\s*=\s*\S+`),
	regexp.MustCompile(`-----BEGIN\s+\w+\s+PRIVATE\s+KEY-----[\s\S]*?-----END\s+\w+\s+PRIVATE\s+KEY-----`),
}

// Filter replaces secrets in content with [REDACTED].
func Filter(content string) string {
	for _, p := range patterns {
		content = p.ReplaceAllString(content, "[REDACTED]")
	}
	return content
}
