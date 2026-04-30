// Package version provides the canonical version string for the yaad binary.
// The Version variable is set at build time via ldflags:
//   go build -ldflags "-X github.com/GrayCodeAI/yaad/internal/version.Version=v1.2.3"
package version

// Version is the current version of yaad. Set at build time.
var Version = "dev"

// String returns the version.
func String() string { return Version }
