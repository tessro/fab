// Package version provides build and version information.
package version

// Build information set via ldflags.
var (
	// Version is the semantic version (set via -ldflags).
	Version = "dev"
	// Commit is the git commit hash (set via -ldflags).
	Commit = "unknown"
	// Date is the build date (set via -ldflags).
	Date = "unknown"
)
