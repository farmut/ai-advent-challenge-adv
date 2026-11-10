package profilefs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
)

func TestRepo_Day2Profiles(t *testing.T) {
	var log bytes.Buffer
	r := &Repo{Dir: "testdata/day2", Log: &log}
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("want 3 profiles, got %d", len(list))
	}
	byName := map[string]domain.AgentProfile{}
	for _, p := range list {
		byName[p.Name] = p
	}
	bf := byName["bug-fixer"]
	if !bf.HasTool(domain.PermBash) || !bf.HasTool(domain.PermEdit) {
		t.Errorf("bug-fixer tools: %v", bf.Tools)
	}
	if bf.Env["GOPROXY"] != "off" || bf.Env["GOFLAGS"] != "-mod=readonly" {
		t.Errorf("bug-fixer env: %v", bf.Env)
	}
	if !strings.HasPrefix(bf.Prompt, "# Роль") {
		t.Errorf("prompt must be the body without frontmatter, got %q", bf.Prompt[:30])
	}
	rs := byName["researcher"]
	if rs.HasTool(domain.PermEdit) || len(rs.Allow) != 3 {
		t.Errorf("researcher: tools %v allow %v", rs.Tools, rs.Allow)
	}
	if !strings.Contains(log.String(), "игнорируется") {
		t.Errorf("model hint must be logged as ignored: %q", log.String())
	}
}

func TestRepo_ShippedProfiles(t *testing.T) {
	r := &Repo{Dir: "../../../profiles", Log: &bytes.Buffer{}}
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bug-fixer", "coder", "planner", "researcher", "security-go", "security-vue", "tech-writer"}
	if len(list) != len(want) {
		t.Fatalf("got %d profiles, want %d", len(list), len(want))
	}
	for i, p := range list {
		if p.Name != want[i] {
			t.Errorf("profile %d = %q, want %q", i, p.Name, want[i])
		}
		if !strings.HasPrefix(p.Prompt, "# profile: "+p.Name) {
			t.Errorf("%s: prompt must start with the profile marker", p.Name)
		}
		if !strings.Contains(p.Prompt, "<<<DATA-") {
			t.Errorf("%s: security section must carry the DATA-nonce rule", p.Name)
		}
		for _, marker := range []string{"<|im_start|>", "zero-width", "prompt extraction", "instruction override"} {
			if !strings.Contains(p.Prompt, marker) {
				t.Errorf("%s: security section must carry the Day12 injection rules (%q)", p.Name, marker)
			}
		}
	}
}

func writeProfile(t *testing.T, dir, name, md, settings string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(dir, name+".settings.json"), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

const okMD = "---\nname: x\ndescription: d\ntools: Read, Grep\n---\n\nbody\n"

func TestRepo_Errors(t *testing.T) {
	cases := []struct {
		name, md, settings, want string
	}{
		{"name mismatch", strings.Replace(okMD, "name: x", "name: y", 1), "", "does not match"},
		{"unknown frontmatter field", strings.Replace(okMD, "tools:", "color: red\ntools:", 1), "", "frontmatter"},
		{"unknown tool", strings.Replace(okMD, "Read, Grep", "Read, Shell", 1), "", "unknown tool"},
		{"no frontmatter", "body only", "", "frontmatter"},
		{"unknown section", okMD, `{"permissions":{"allow":["Read"]},"hooks":{}}`, "unknown section"},
		{"unknown permissions field", okMD, `{"permissions":{"allow":[],"ask":[]}}`, "permissions"},
		{"unknown kind", okMD, `{"permissions":{"allow":["Exec(ls)"]}}`, "unknown kind"},
		{"env not allowlisted", okMD, `{"env":{"LD_PRELOAD":"x"}}`, "allowlisted"},
		{"env bad value", okMD, `{"env":{"GOFLAGS":"-toolexec=/tmp/x"}}`, "not allowed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProfile(t, dir, "x", c.md, c.settings)
			_, err := (&Repo{Dir: dir, Log: &bytes.Buffer{}}).Get("x")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestRepo_NoSettingsIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, dir, "x", okMD, "")
	p, err := (&Repo{Dir: dir}).Get("x")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range p.Allow {
		if r.Kind != domain.PermRead && r.Kind != domain.PermGrep && r.Kind != domain.PermGlob {
			t.Errorf("profile without settings must be read-only, got %v", r)
		}
	}
	if len(p.Deny) != 0 || p.Env != nil {
		t.Errorf("unexpected deny/env: %+v", p)
	}
}

func TestRepo_GetRejectsBadNamesAndMissing(t *testing.T) {
	r := &Repo{Dir: t.TempDir()}
	for _, n := range []string{"../x", "X", "", "a/b"} {
		if _, err := r.Get(n); err == nil {
			t.Errorf("name %q must be rejected", n)
		}
	}
	if _, err := r.Get("absent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestRepo_SizeCap(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, dir, "x", okMD+strings.Repeat("a", MaxProfileBytes), "")
	if _, err := (&Repo{Dir: dir}).Get("x"); err == nil || !strings.Contains(err.Error(), "больше") {
		t.Fatalf("oversized profile must be rejected, got %v", err)
	}
}
