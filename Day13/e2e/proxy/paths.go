package proxy

import (
	"os"
	"path/filepath"
	"strings"

	"agile-consultant/internal/config"
)

// SanitizePaths replaces the module root with "<root>" and the home
// directory with "~" in report text (HTML, markdown, JSON), so reports do
// not reveal local absolute paths, including those in older audit records.
// Paths contain no HTML-special characters, so this is safe after
// rendering.
func SanitizePaths(s string) string {
	for _, r := range pathReplacements() {
		if r[0] != "" && r[0] != "/" {
			s = strings.ReplaceAll(s, r[0], r[1])
		}
	}
	return s
}

func pathReplacements() [][2]string {
	var out [][2]string
	if wd, err := os.Getwd(); err == nil {
		root := config.ModuleRoot(wd)
		out = append(out, [2]string{root, "<root>"})
		if real, err := filepath.EvalSymlinks(root); err == nil && real != root {
			out = append(out, [2]string{real, "<root>"})
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, [2]string{home, "~"})
		if real, err := filepath.EvalSymlinks(home); err == nil && real != home {
			out = append(out, [2]string{real, "~"})
		}
	}
	return out
}
