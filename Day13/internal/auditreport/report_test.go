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
	_ = os.WriteFile(p, []byte(`{"v":4,"status":"ok"}`+"\n"), 0o600)
	if _, _, err := Read([]string{p}); err == nil || !strings.Contains(err.Error(), "unsupported audit schema v4") {
		t.Fatalf("err = %v", err)
	}
}

// TestReadV1V2V3 reads every supported schema version; v3 records carry
// masked tool calls.
func TestReadV1V2V3(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	lines := `{"v":1,"status":"ok","model":"m"}` + "\n" +
		`{"v":2,"status":"ok","model":"m","key_id":"k1"}` + "\n" +
		`{"v":3,"status":"ok","model":"m","key_id":"k1","prompt_masked":[{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"write_file","arguments":"{\"k\":\"[REDACTED_API_KEY]\"}"}}]},{"role":"tool","content":"ok","tool_call_id":"call_1"}],"response_tool_calls_masked":[{"id":"call_2","type":"function","function":{"name":"read_text_file","arguments":"{}"}}]}` + "\n"
	_ = os.WriteFile(p, []byte(lines), 0o600)
	recs, _, err := Read([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || recs[2].PromptMasked[0].ToolCalls[0].Function.Name != "write_file" ||
		recs[2].PromptMasked[1].ToolCallID != "call_1" || recs[2].ResponseToolCallsMasked[0].ID != "call_2" {
		t.Fatalf("records %+v", recs)
	}
	r := Build(recs, nil)
	for _, v := range []string{"v1", "v2", "v3"} {
		found := false
		for _, c := range r.ByVersion {
			found = found || (c.Key == v && c.Count == 1)
		}
		if !found {
			t.Errorf("ByVersion lacks %s: %+v", v, r.ByVersion)
		}
	}
}
