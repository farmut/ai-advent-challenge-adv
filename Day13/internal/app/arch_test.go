package app

import (
	"os/exec"
	"strings"
	"testing"
)

// TestArchitecture enforces Clean Architecture: use cases and ports must not
// depend on Gin, YAML or other infrastructure; Gin is allowed only in
// internal/delivery/http (and the wiring packages that import it).
func TestArchitecture(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not available")
	}
	forbidden := []string{"github.com/gin-gonic", "gopkg.in/yaml", "github.com/charmbracelet", "agile-consultant/internal/adapters", "agile-consultant/internal/delivery", "agile-consultant/internal/config"}
	pkgs := []string{"agile-consultant/internal/ports", "agile-consultant/internal/usecase/..."}
	out, err := exec.Command("go", append([]string{"list", "-deps", "-f", "{{.ImportPath}}"}, pkgs...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, f := range forbidden {
			if strings.HasPrefix(dep, f) {
				t.Errorf("use case/ports layer depends on %s", dep)
			}
		}
	}

	// Gin must not leak outside delivery/http and the wiring/entry packages.
	all, err := exec.Command("go", "list", "-f", "{{.ImportPath}} {{join .Imports \" \"}}", "agile-consultant/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, all)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(all)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pkg := fields[0]
		if pkg == "agile-consultant/internal/delivery/http" {
			continue
		}
		for _, imp := range fields[1:] {
			if strings.HasPrefix(imp, "github.com/gin-gonic") {
				t.Errorf("%s imports %s; Gin is allowed only in internal/delivery/http", pkg, imp)
			}
		}
	}
}
