package llm

import (
	"errors"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

func toolPair(id, result string) []domain.Message {
	return []domain.Message{
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCallMsg{{ID: id, Type: "function", Function: domain.ToolCallFunction{Name: "grep"}}}},
		{Role: domain.RoleTool, ToolCallID: id, Content: result},
	}
}

// assertPairsIntact fails when a tool message is not preceded by an assistant
// message whose tool_calls contain its id, or a tool_call has no answer.
func assertPairsIntact(t *testing.T, ms []domain.Message) {
	t.Helper()
	open := map[string]bool{}
	for i, m := range ms {
		switch {
		case m.Role == domain.RoleAssistant && len(m.ToolCalls) > 0:
			for id := range open {
				t.Errorf("tool_call %s has no answer before message %d", id, i)
			}
			open = map[string]bool{}
			for _, tc := range m.ToolCalls {
				open[tc.ID] = true
			}
		case m.Role == domain.RoleTool:
			if !open[m.ToolCallID] {
				t.Errorf("message %d: orphaned tool result %s", i, m.ToolCallID)
			}
			delete(open, m.ToolCallID)
		default:
			for id := range open {
				t.Errorf("tool_call %s unanswered before message %d", id, i)
			}
			open = map[string]bool{}
		}
	}
}

func TestFitBody_NoChangeWhenFits(t *testing.T) {
	ms := []domain.Message{{Role: domain.RoleUser, Content: "hi"}}
	got, err := fitBody(ms, 1000)
	if err != nil || len(got) != 1 || got[0].Content != "hi" {
		t.Fatalf("got (%v, %v)", got, err)
	}
}

func TestFitBody_TruncatesOldToolResultsFirst(t *testing.T) {
	big := strings.Repeat("x", 3000)
	ms := []domain.Message{{Role: domain.RoleSystem, Content: "sys"}}
	ms = append(ms, toolPair("a", big)...)
	ms = append(ms, domain.Message{Role: domain.RoleUser, Content: "next"})
	got, err := fitBody(ms, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(ms) {
		t.Fatalf("truncation should suffice, messages %d -> %d", len(ms), len(got))
	}
	if !strings.Contains(got[2].Content, "обрезано 3000 байт") {
		t.Errorf("tool result not stubbed: %q", got[2].Content)
	}
	if ms[2].Content != big {
		t.Error("input slice must not be modified")
	}
	assertPairsIntact(t, got)
}

func TestFitBody_DropsWholePairsAndKeepsSystemAndLast(t *testing.T) {
	var ms []domain.Message
	ms = append(ms, domain.Message{Role: domain.RoleSystem, Content: strings.Repeat("s", 200)})
	for _, id := range []string{"a", "b", "c", "d"} {
		ms = append(ms, toolPair(id, "short")...)
	}
	last := domain.Message{Role: domain.RoleUser, Content: strings.Repeat("q", 200)}
	ms = append(ms, last)

	got, err := fitBody(ms, messagesSize(ms)-150)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) >= len(ms) {
		t.Fatal("expected some units to be dropped")
	}
	if got[0].Role != domain.RoleSystem || got[len(got)-1].Content != last.Content {
		t.Error("system and last message must survive")
	}
	if (len(got)-2)%2 != 0 {
		t.Errorf("pairs must be dropped whole, got %d middle messages", len(got)-2)
	}
	assertPairsIntact(t, got)
}

func TestFitBody_LastUnitIsNeverSplit(t *testing.T) {
	// The current step: assistant tool_calls + its results is the tail.
	ms := []domain.Message{{Role: domain.RoleUser, Content: strings.Repeat("u", 500)}}
	ms = append(ms, toolPair("z", "res")...)
	got, err := fitBody(ms, messagesSize(ms)-100)
	if err != nil {
		t.Fatal(err)
	}
	assertPairsIntact(t, got)
	if got[len(got)-1].ToolCallID != "z" {
		t.Error("the trailing tool result was lost")
	}
}

func TestFitBody_ImpossibleIsFatal(t *testing.T) {
	ms := []domain.Message{
		{Role: domain.RoleSystem, Content: strings.Repeat("s", 500)},
		{Role: domain.RoleUser, Content: "q"},
	}
	_, err := fitBody(ms, 100)
	var le *port.LLMError
	if !errors.As(err, &le) || le.Code != port.CodeRequestTooLargeClient || le.Retryable() {
		t.Fatalf("want non-retryable request_too_large_client, got %v", err)
	}
}

func TestCapContextWindow_KeepsPairsTogether(t *testing.T) {
	var ms []domain.Message
	ms = append(ms, domain.Message{Role: domain.RoleSystem, Content: "sys"})
	ms = append(ms, toolPair("a", strings.Repeat("word ", 400))...)
	ms = append(ms, domain.Message{Role: domain.RoleUser, Content: "q"})
	got := capContextWindow(ms, 50, 0)
	assertPairsIntact(t, got)
	if got[len(got)-1].Content != "q" {
		t.Error("last message lost")
	}
}
