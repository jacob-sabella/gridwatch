// Package buildinfo exposes build-time metadata set via -ldflags -X.
package buildinfo

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// UserAgent returns the outbound User-Agent string for upstream HTTP calls.
// Liquipedia's ToU requires descriptive contact info, which callers append.
func UserAgent(contact string) string {
	return "gridwatch/" + Version + " (+" + contact + ")"
}
