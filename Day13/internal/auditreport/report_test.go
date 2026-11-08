package auditreport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agile-consultant/internal/ports"
)

func TestContextAround(t *testing.T) {
	long := strings.Repeat("а", 40)
	tests := []struct {
		name, text, ph, want string
	}{
		{"short", "ключ [X] тут", "[X]", "ключ [X] тут"},
		{"trim", long + "[X]" + long, "[X]", "…" + strings.Repeat("а", 30) + "[X]" + strings.Repeat("а", 30) + "…"},
		{"missing", "нет", "[X]", ""},
		{"no placeholder", "x", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contextAround(tt.text, tt.ph); got != tt.want {
				t.Fatalf("got %q", got)
			}
		})
	}
	if Cell("a|b\nc") != `a\|b ⏎ c` || Cell("") != "—" {
		t.Fatal("Cell")
	}
}

func TestBuildV2Unauthorized(t *testing.T) {
	recs := []ports.AuditRecord{
		{V: 1, Status: "ok", Model: "m", RequestID: "a"},
		{V: 2, Status: "ok", Model: "m", KeyID: "k1", RequestID: "b"},
		{V: 2, Status: "ok", Model: "m", KeyID: "k2", RequestID: "c"},
		{V: 2, Status: ports.AuditStatusUnauthorized, HTTPStatus: 401, Reason: ports.AuthReasonMissing, RequestID: "d"},
		{V: 2, Status: ports.AuditStatusUnauthorized, HTTPStatus: 401, Reason: ports.AuthReasonMissing, RequestID: "e"},
		{V: 2, Status: ports.AuditStatusRateLimited, HTTPStatus: 429, Reason: ports.AuthReasonFailLimit, RequestID: "f"},
	}
	r := Build(recs, nil)
	got := func(cs []Count, k string) int {
		for _, c := range cs {
			if c.Key == k {
				return c.Count
			}
		}
		return 0
	}
	if got(r.ByVersion, "v1") != 1 || got(r.ByVersion, "v2") != 5 || got(r.ByKeyID, "k1") != 1 || got(r.ByKeyID, "k2") != 1 ||
		got(r.Unauthorized, "unauthorized/missing") != 2 || got(r.Unauthorized, "rate_limited/auth_fail_limit") != 1 ||
		got(r.ByStatus, "unauthorized") != 2 || got(r.ByModel, "m") != 3 || got(r.ByModel, "") != 0 {
		t.Fatalf("report %+v", r)
	}
	md := Markdown(r)
	for _, want := range []string{"| v2 | 5 |", "| `k1` | 1 |", "| unauthorized/missing | 2 |"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
}

func TestReadRejectsUnknownVersion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	_ = os.WriteFile(p, []byte(`{"v":3,"status":"ok"}`+"\n"), 0o600)
	if _, _, err := Read([]string{p}); err == nil || !strings.Contains(err.Error(), "unsupported audit schema v3") {
		t.Fatalf("err = %v", err)
	}
}
