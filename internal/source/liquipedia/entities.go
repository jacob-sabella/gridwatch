package liquipedia

import "html"

// decodeEntities is the minimal HTML entity decoder from the original
// rl-esports-tracker parser (parity for test stability), augmented with
// html.UnescapeString to cover numeric entities like &#8211;.
//
// The parity set: &#39; &quot; &amp; &lt; &gt; &nbsp;
// html.UnescapeString handles all of those plus numeric entities, so we
// could in principle use only that. We apply both as defense-in-depth
// and because html.UnescapeString doesn't touch &nbsp; in some Go versions.
func decodeEntities(s string) string {
	if s == "" {
		return s
	}
	// Minimal pass (parity with original JS parser).
	s = replaceAll(s, "&#39;", "'")
	s = replaceAll(s, "&quot;", "\"")
	s = replaceAll(s, "&amp;", "&")
	s = replaceAll(s, "&lt;", "<")
	s = replaceAll(s, "&gt;", ">")
	s = replaceAll(s, "&nbsp;", " ")
	// Stdlib pass covers numeric entities and anything we missed.
	return html.UnescapeString(s)
}

// replaceAll avoids pulling strings.ReplaceAll for tiny targeted replaces;
// faster in the hot parse path for small inputs.
func replaceAll(s, old, new string) string {
	if old == "" || old == new {
		return s
	}
	var out []byte
	for {
		i := index(s, old)
		if i < 0 {
			break
		}
		out = append(out, s[:i]...)
		out = append(out, new...)
		s = s[i+len(old):]
	}
	if out == nil {
		return s
	}
	out = append(out, s...)
	return string(out)
}

func index(s, sub string) int {
	n := len(sub)
	if n == 0 {
		return 0
	}
	if n > len(s) {
		return -1
	}
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == sub {
			return i
		}
	}
	return -1
}
