// Package version exposes build metadata stamped in at release time via -ldflags.
// The defaults apply to `go build`/`go run` (unstamped) developer builds.
package version

import "fmt"

var (
	// Version is the release version (e.g. "1.2.3"); "dev" for unstamped builds.
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "none"
	// Date is the build timestamp.
	Date = "unknown"
)

// String renders a one-line human-readable build identifier.
func String() string {
	return fmt.Sprintf("gophermind %s (commit %s, built %s)", Version, Commit, Date)
}
