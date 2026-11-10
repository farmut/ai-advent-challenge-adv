package usecase_test

import (
	"context"
	"errors"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
)

// scriptLLM replays a fixed list of responses and records the requests.
type scriptLLM struct {
	resps []port.LLMResponse
	reqs  []port.LLMRequest
}

func (s *scriptLLM) Chat(_ context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	s.reqs = append(s.reqs, req)
	if len(s.resps) == 0 {
		return port.LLMResponse{Content: "done"}, nil
	}
	r := s.resps[0]
	s.resps = s.resps[1:]
	return r, nil
}

func toolUC(llm port.LLMClient) *usecase.ChatUseCase {
	return usecase.NewChatUseCase(llm, &stubHistory{}, &stubStats{}, &stubSummary{}, newStubFacts(), newStubWM(), newStubLTM(), newStubProfile())
}

var oneTool = []domain.MCPTool{{Name: "write_file"}}

func TestExecuteWithTools_GuardBlockedIsNotSuccess(t *testing.T) {
	llm := &scriptLLM{resps: []port.LLMResponse{{
		Content:   "Ответ модели скрыт политикой безопасности прокси.",
		ToolCalls: []domain.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{}`}},
		Guard:     port.GuardMeta{Output: port.GuardBlocked, OutputFindings: "dangerous_command"},
	}}}
	executed := 0
	exec := func(context.Context, string, string) (string, error) { executed++; return "ok", nil }
	res, err := toolUC(llm).ExecuteWithTools(context.Background(), baseCfg("q"), oneTool, exec)
	if err != nil {
		t.Fatal(err)
	}
	if !res.GuardBlocked || res.GuardReason != "dangerous_command" {
		t.Errorf("blocked answer must be flagged: %+v", res)
	}
	if executed != 0 {
		t.Error("tool calls of a blocked answer must never run")
	}
}

func TestExecuteWithTools_RedactedToolCallsNotExecuted(t *testing.T) {
	llm := &scriptLLM{resps: []port.LLMResponse{
		{
			ToolCalls: []domain.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{"content":"[REDACTED]"}`}},
			Guard:     port.GuardMeta{Output: port.GuardRedacted},
		},
		{Content: "final"},
	}}
	executed := 0
	exec := func(context.Context, string, string) (string, error) { executed++; return "ok", nil }
	res, err := toolUC(llm).ExecuteWithTools(context.Background(), baseCfg("q"), oneTool, exec)
	if err != nil {
		t.Fatal(err)
	}
	if executed != 0 || res.RedactedToolCalls != 1 || res.ToolCalls != 0 {
		t.Fatalf("redacted call executed=%d result=%+v", executed, res)
	}
	if res.Content != "final" || res.GuardBlocked {
		t.Errorf("result: %+v", res)
	}
	// The model was told why, as the tool result of that call.
	last := llm.reqs[1].Messages[len(llm.reqs[1].Messages)-1]
	if last.Role != domain.RoleTool || last.ToolCallID != "c1" || last.Content != usecase.RedactedToolCallResult {
		t.Errorf("model must get the redaction notice, got %+v", last)
	}
}

func TestExecuteWithTools_OnResponseAborts(t *testing.T) {
	llm := &scriptLLM{resps: []port.LLMResponse{{Content: "x"}}}
	stop := errors.New("budget")
	cfg := baseCfg("q")
	seen := 0
	cfg.OnResponse = func(port.LLMResponse) error { seen++; return stop }
	_, err := toolUC(llm).ExecuteWithTools(context.Background(), cfg, oneTool,
		func(context.Context, string, string) (string, error) { return "", nil })
	if !errors.Is(err, stop) || seen != 1 {
		t.Fatalf("err=%v seen=%d", err, seen)
	}
}

func TestExecute_GuardBlockedFlag(t *testing.T) {
	llm := &scriptLLM{resps: []port.LLMResponse{{Content: "refusal", FinishReason: "content_filter",
		Guard: port.GuardMeta{Output: port.GuardBlocked}}}}
	res, err := toolUC(llm).Execute(context.Background(), baseCfg("q"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.GuardBlocked || res.GuardReason != "content_filter" {
		t.Errorf("%+v", res)
	}
}
