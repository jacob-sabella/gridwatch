// Package ui embeds templates and static assets into the gridwatch
// binary via embed.FS so the final artifact is a single file.
package ui

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"strings"
	"time"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Templates holds the parsed template set. Created once at startup.
type Templates struct {
	tpl *template.Template
}

// Load parses all templates with a shared function map.
func Load() (*Templates, error) {
	funcs := template.FuncMap{
		"formatTime": func(t time.Time, layout string) string {
			return t.Format(layout)
		},
		"formatTimeIn": func(t time.Time, tz string) string {
			loc, err := time.LoadLocation(tz)
			if err != nil {
				loc = time.UTC
			}
			return t.In(loc).Format("3:04 PM")
		},
		"formatDateIn": func(t time.Time, tz string) string {
			loc, err := time.LoadLocation(tz)
			if err != nil {
				loc = time.UTC
			}
			return t.In(loc).Format("Mon Jan 2")
		},
		"relative": func(t time.Time) string {
			d := time.Until(t)
			switch {
			case d < 0:
				return "started " + absDuration(-d) + " ago"
			case d < time.Minute:
				return "starting"
			case d < time.Hour:
				return fmt.Sprintf("in %dm", int(d.Minutes()))
			case d < 24*time.Hour:
				return fmt.Sprintf("in %dh %dm", int(d.Hours()), int(d.Minutes())%60)
			default:
				return fmt.Sprintf("in %dd", int(d.Hours()/24))
			}
		},
		"joinURL": func(base, path string) string {
			if base == "" {
				return path
			}
			return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
		},
		"streamIcon": func(platform string) string {
			switch platform {
			case "twitch":
				return "📺"
			case "youtube":
				return "▶️"
			default:
				return "🔗"
			}
		},
		"hasKey": func(m map[string]any, key string) bool {
			_, ok := m[key]
			return ok
		},
		"int": func(v any) int {
			switch x := v.(type) {
			case int:
				return x
			case int64:
				return int(x)
			case int32:
				return int(x)
			case float64:
				return int(x)
			}
			return 0
		},
		"seq": func(n int) []int {
			out := make([]int, n)
			for i := range out {
				out[i] = i
			}
			return out
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict requires an even number of arguments")
			}
			m := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict keys must be strings")
				}
				m[key] = values[i+1]
			}
			return m, nil
		},
	}

	t := template.New("gridwatch").Funcs(funcs)

	// Parse all .html files under templates/ and templates/partials/.
	err := fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".html") {
			return nil
		}
		data, err := templateFS.ReadFile(path)
		if err != nil {
			return err
		}
		// Template names are relative to templates/ for cleaner Execute calls.
		name := strings.TrimPrefix(path, "templates/")
		_, err = t.New(name).Parse(string(data))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Templates{tpl: t}, nil
}

// Execute renders the named template to w.
func (t *Templates) Execute(w io.Writer, name string, data any) error {
	tpl := t.tpl.Lookup(name)
	if tpl == nil {
		return fmt.Errorf("template not found: %q", name)
	}
	return tpl.Execute(w, data)
}

// Static returns the embedded static file system rooted at /static.
func (t *Templates) Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		// Should never happen with a correctly-embedded static directory.
		return staticFS
	}
	return sub
}

func absDuration(d time.Duration) string {
	if d < time.Minute {
		return "moments"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
