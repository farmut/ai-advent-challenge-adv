package devpipeline

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// fileStack classifies a changed file: "go", "vue" or "" (neither).
func fileStack(rel string) string {
	base := path.Base(rel)
	switch base {
	case "go.mod", "go.sum":
		return StackGo
	case "package.json", "package-lock.json":
		return StackVue
	}
	switch strings.ToLower(path.Ext(base)) {
	case ".go":
		return StackGo
	case ".vue", ".ts", ".tsx", ".js", ".mjs", ".cjs", ".jsx", ".mts", ".cts":
		return StackVue
	}
	return ""
}

// detectStacks returns the stacks touched by the changed (and deleted)
// files, falling back to the hint when no file classifies.
func detectStacks(files []string, hint []string) ([]string, error) {
	set := map[string]bool{}
	for _, f := range files {
		if s := fileStack(f); s != "" {
			set[s] = true
		}
	}
	if len(set) == 0 {
		for _, h := range hint {
			if h != StackGo && h != StackVue {
				return nil, fmt.Errorf("неизвестный стек %q в подсказке", h)
			}
			set[h] = true
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("стек не определён: среди изменённых файлов нет Go/Vue и нет подсказки stack")
	}
	return sortedKeys(set), nil
}

// diff compares two snapshots: changed holds added and modified files,
// deleted the removed ones; both sorted.
func diff(before, after map[string]string) (changed, deleted []string) {
	for p, h := range after {
		if before[p] != h {
			changed = append(changed, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			deleted = append(deleted, p)
		}
	}
	sort.Strings(changed)
	sort.Strings(deleted)
	return changed, deleted
}
