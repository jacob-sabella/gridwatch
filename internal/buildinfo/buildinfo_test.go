package buildinfo

import (
	"regexp"
	"strings"
	"testing"
)

func TestUserAgentFormat(t *testing.T) {
	Version = "0.1.1"
	ua := UserAgent("you@example.com")

	// Liquipedia's ToU example: "LiveScoresBot/1.0 (http://www.example.com/; email@example.com)"
	// Required elements: project name, version, URL, separator, contact.
	pattern := regexp.MustCompile(`^gridwatch/\d[^\s]* \(https?://[^\s;]+; [^\s()]+\)$`)
	if !pattern.MatchString(ua) {
		t.Errorf("User-Agent doesn't match Liquipedia ToU format:\n  got: %q\n  want pattern: %v", ua, pattern)
	}

	// Must not contain generic client identifiers Liquipedia blocks.
	forbidden := []string{"Go-http-client", "Python-requests", "node-fetch", "curl"}
	for _, bad := range forbidden {
		if strings.Contains(ua, bad) {
			t.Errorf("User-Agent contains forbidden substring %q: %s", bad, ua)
		}
	}

	if !strings.Contains(ua, "you@example.com") {
		t.Errorf("UA must include contact: %s", ua)
	}
	if !strings.Contains(ua, "github.com/jacob-sabella/gridwatch") {
		t.Errorf("UA must include project URL: %s", ua)
	}
}
