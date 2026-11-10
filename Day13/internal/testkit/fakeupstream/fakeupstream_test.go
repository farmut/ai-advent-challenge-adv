package fakeupstream

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agile-consultant/internal/ports"
)

// chatRound sends one chat completion request with the given system prompt
// and returns the assistant message and finish reason.
func chatRound(t *testing.T, url, system string) (map[string]any, string) {
	t.Helper()
	body := `{"model":"fake","messages":[{"role":"user","content":"x"}]}`
	if system != "" {
		body = `{"model":"fake","messages":[{"role":"system","content":` + strconvQuote(system) + `},{"role":"user","content":"x"}]}`
	}
	resp, err := http.Post(url+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content   any              `json:"content"`
				ToolCalls []ports.ToolCall `json:"tool_calls"`
			} `json:"message"`
			Finish string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Choices) != 1 {
		t.Fatalf("choices = %d", len(out.Choices))
	}
	msg := map[string]any{"content": out.Choices[0].Message.Content}
	if len(out.Choices[0].Message.ToolCalls) > 0 {
		msg["tool_calls"] = out.Choices[0].Message.ToolCalls
	}
	return msg, out.Choices[0].Finish
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestRoleRouting(t *testing.T) {
	h := NewHandler()
	if err := h.LoadScript([]byte(`{
		"roles": {
			"planner": [
				{"content": "plan-1", "finish_reason": "stop", "usage": {"prompt_tokens": 10, "completion_tokens": 5}},
				{"content": "plan-2"}
			],
			"coder": [
				{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "write_file", "arguments": "{\"path\":\"a.go\"}"}}]}
			]
		},
		"default": {"content": "fallback"}
	}`)); err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// planner queue is served in order...
	if msg, _ := chatRound(t, srv.URL, "# profile: planner\nDo the plan."); msg["content"] != "plan-1" {
		t.Fatalf("planner #1 content = %v", msg["content"])
	}
	if msg, _ := chatRound(t, srv.URL, "# profile: planner\nDo the plan."); msg["content"] != "plan-2" {
		t.Fatalf("planner #2 content = %v", msg["content"])
	}
	// ...and the last reply repeats after the queue is exhausted.
	if msg, _ := chatRound(t, srv.URL, "# profile: planner"); msg["content"] != "plan-2" {
		t.Fatalf("planner #3 (repeat last) content = %v", msg["content"])
	}

	// `# role:` marker works too; other lines before the marker are ignored
	// only within the scanned prefix — here the marker is the first line.
	if msg, _ := chatRound(t, srv.URL, "# role: planner"); msg["content"] != "plan-2" {
		t.Fatalf("role marker content = %v", msg["content"])
	}

	// tool_calls pass through with the derived finish reason.
	msg, finish := chatRound(t, srv.URL, "system\n# profile: coder\nWrite code.")
	if finish != "tool_calls" {
		t.Fatalf("coder finish = %q", finish)
	}
	if msg["content"] != nil {
		t.Fatalf("coder content = %v, want null", msg["content"])
	}
	calls, ok := msg["tool_calls"].([]ports.ToolCall)
	if !ok || len(calls) != 1 || calls[0].Function.Name != "write_file" || calls[0].Function.Arguments != `{"path":"a.go"}` {
		t.Fatalf("coder tool_calls = %v", msg["tool_calls"])
	}

	// Unknown role falls back to the default reply.
	if msg, _ := chatRound(t, srv.URL, "# profile: judge\nVerdict."); msg["content"] != "fallback" {
		t.Fatalf("default content = %v", msg["content"])
	}
	// No marker at all also falls back to the default reply.
	if msg, _ := chatRound(t, srv.URL, ""); msg["content"] != "fallback" {
		t.Fatalf("no-marker content = %v", msg["content"])
	}
}

func TestRoleQueueRepeatsLastNotFirst(t *testing.T) {
	h := NewHandler()
	h.SetRole("coder",
		Reply{Content: "one"},
		Reply{Content: "two"},
	)
	srv := httptest.NewServer(h)
	defer srv.Close()

	want := []string{"one", "two", "two", "two"}
	for i, w := range want {
		msg, _ := chatRound(t, srv.URL, "# profile: coder")
		if msg["content"] != w {
			t.Fatalf("round %d content = %v, want %q", i, msg["content"], w)
		}
	}
}

func TestLoadScriptFailClosed(t *testing.T) {
	h := NewHandler()
	cases := map[string]string{
		"invalid JSON":        `{"roles":`,
		"unknown field":       `{"roles": {}, "default": {"content": "x"}, "extra": 1}`,
		"no roles no default": `{"roles": {}}`,
		"empty role queue":    `{"roles": {"planner": []}, "default": {"content": "x"}}`,
		"bad finish_reason":   `{"default": {"content": "x", "finish_reason": "bogus"}}`,
		"trailing data":       `{"default": {"content": "x"}} {"default": {"content": "y"}}`,
	}
	for name, script := range cases {
		if err := h.LoadScript([]byte(script)); err == nil {
			t.Fatalf("%s: LoadScript succeeded, want error", name)
		}
	}
}

func TestLegacyScriptAndEnqueueUnchanged(t *testing.T) {
	f := NewFakeUpstream()
	defer f.Close()
	f.Script("base", &Usage{Prompt: 40, Completion: 20})
	f.Enqueue(Reply{Content: "queued"})

	msg, _ := chatRound(t, f.URL, "") // no role: legacy queue first
	if msg["content"] != "queued" {
		t.Fatalf("queued content = %v", msg["content"])
	}
	msg, _ = chatRound(t, f.URL, "")
	if msg["content"] != "base" {
		t.Fatalf("default content = %v", msg["content"])
	}
}

func TestDetectRole(t *testing.T) {
	msgs := []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{"system", "You are helpful.\n# profile: security-go\nScan the code."},
	}
	if got := DetectRole(msgs); got != "security-go" {
		t.Fatalf("DetectRole = %q", got)
	}
	msgs[0].Content = "no marker here"
	if got := DetectRole(msgs); got != "" {
		t.Fatalf("DetectRole = %q, want empty", got)
	}
}
