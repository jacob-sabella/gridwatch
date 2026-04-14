// Package buildinfo exposes build-time metadata set via -ldflags -X.
package buildinfo

import "fmt"

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"

	// Homepage is the canonical project URL included in the outbound
	// User-Agent. It's baked at build time rather than configurable
	// because Liquipedia's ToU asks for a **URL identifying the
	// project** (not the deployment), and there's only one gridwatch
	// project.
	Homepage = "https://github.com/jacob-sabella/gridwatch"
)

// UserAgent returns the outbound User-Agent string for upstream HTTP
// calls. It matches the exact format Liquipedia's ToU documents as an
// example: "Name/version (url; email)".
//
// See https://liquipedia.net/api-terms-of-use — the ToU explicitly
// rejects generic agents like "Go-http-client" and requires a custom
// UA with project identifier + contact info.
func UserAgent(contact string) string {
	return fmt.Sprintf("gridwatch/%s (%s; %s)", Version, Homepage, contact)
}
