package domain

import (
	"os/exec"
	"strings"
	"testing"
)

func TestParsePermRule(t *testing.T) {
	cases := []struct {
		in      string
		want    PermRule
		wantErr bool
	}{
		{"Grep", PermRule{Kind: PermGrep}, false},
		{"Edit(./internal/**)", PermRule{Kind: PermEdit, Pattern: "./internal/**"}, false},
		{"Bash(go test:*)", PermRule{Kind: PermBash, Pattern: "go test:*"}, false},
		{"Bash(git * --output*)", PermRule{Kind: PermBash, Pattern: "git * --output*"}, false},
		{"WebFetch", PermRule{Kind: PermWebFetch}, false},
		{"", PermRule{}, true},
		{"Bash()", PermRule{}, true},
		{"Bash(go test", PermRule{}, true},
		{"Exec(ls)", PermRule{}, true},
		{"read", PermRule{}, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParsePermRule(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if !c.wantErr && got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
			if !c.wantErr && got.String() != c.in {
				t.Fatalf("String() = %q, want %q", got.String(), c.in)
			}
		})
	}
}

// TestDomainImportsOnlyStdlib enforces the Clean Architecture invariant: the
// domain package must not depend on anything outside the standard library.
func TestDomainImportsOnlyStdlib(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, imp := range strings.Fields(string(out)) {
		first := strings.SplitN(imp, "/", 2)[0]
		if strings.Contains(first, ".") || strings.HasPrefix(imp, "ai-adv-agent/") {
			t.Errorf("domain imports non-stdlib package %q", imp)
		}
	}
}
