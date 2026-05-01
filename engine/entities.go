package engine

import "regexp"

// Entity represents an auto-extracted entity from content.
type Entity struct {
	Name string
	Type string // "file" or "entity"
}

var (
	fileRe    = regexp.MustCompile(`(?:^|[\s"'` + "`" + `(])([a-zA-Z0-9_./-]+\.(?:go|ts|tsx|js|jsx|py|rs|java|rb|sql|yaml|yml|toml|json|md|css|html|sh|c|cpp|h))\b`)
	urlRe     = regexp.MustCompile(`https?://[^\s"'` + "`" + `)<>]+`)
	pkgGoRe   = regexp.MustCompile(`github\.com/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+`)
	pkgNpmRe  = regexp.MustCompile(`@[a-z0-9_.-]+/[a-z0-9_.-]+`)
	funcRe    = regexp.MustCompile(`\b([a-z][a-zA-Z0-9]{2,})\(`)
	classRe   = regexp.MustCompile(`\b([A-Z][a-zA-Z0-9]{2,})\b`)
)

// ExtractEntities pulls file paths, packages, functions, and classes from content.
func ExtractEntities(content string) []Entity {
	seen := map[string]bool{}
	var out []Entity
	add := func(name, typ string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, Entity{Name: name, Type: typ})
		}
	}
	for _, m := range fileRe.FindAllStringSubmatch(content, -1) {
		add(m[1], "file")
	}
	for _, m := range pkgGoRe.FindAllString(content, -1) {
		add(m, "entity")
	}
	for _, m := range pkgNpmRe.FindAllString(content, -1) {
		add(m, "entity")
	}
	for _, m := range urlRe.FindAllString(content, -1) {
		add(m, "entity")
	}
	// Only extract functions/classes if fewer than 10 already found (avoid noise)
	if len(out) < 10 {
		for _, m := range funcRe.FindAllStringSubmatch(content, 5) {
			add(m[1], "entity")
		}
		for _, m := range classRe.FindAllStringSubmatch(content, 5) {
			add(m[1], "entity")
		}
	}
	return out
}
