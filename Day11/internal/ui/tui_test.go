package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agile-consultant/internal/ports"

	tea "github.com/charmbracelet/bubbletea"
)

type mockConsultant struct {
	current string
	askFunc func(ctx context.Context, question string) (ports.Response, error)
}

func (m *mockConsultant) Ask(ctx context.Context, question string) (ports.Response, error) {
	if m.askFunc != nil {
		return m.askFunc(ctx, question)
	}
	return ports.Response{}, nil
}

func (m *mockConsultant) CurrentModel() string { return m.current }

type mockSwitcher struct {
	switchFunc func(ctx context.Context, modelName string) (string, error)
}

func (m *mockSwitcher) Switch(ctx context.Context, modelName string) (string, error) {
	if m.switchFunc != nil {
		return m.switchFunc(ctx, modelName)
	}
	return "", nil
}

func TestNewModel(t *testing.T) {
	c := &mockConsultant{current: "agile-v1.0-4bit"}
	m := New(c, &mockSwitcher{})

	if m.consultant != c {
		t.Fatal("consultant not set")
	}
	if m.status != "Модель: agile-v1.0-4bit" {
		t.Fatalf("unexpected status: %s", m.status)
	}
	if len(m.history) == 0 {
		t.Fatal("welcome message expected")
	}
	if !m.viewport.AtBottom() {
		t.Fatal("viewport should start at bottom")
	}
}

func TestInitReturnsBlinkCmd(t *testing.T) {
	m := New(&mockConsultant{current: "agile-v1.0-4bit"}, &mockSwitcher{})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init must return a command")
	}
}

func TestEnterEmptyInput(t *testing.T) {
	m := New(&mockConsultant{current: "agile-v1.0-4bit"}, &mockSwitcher{})
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	newModel, cmd := m.Update(msg)
	if cmd != nil {
		t.Fatal("empty input should not trigger a command")
	}
	_ = newModel
}

func TestEnterSendsQuestion(t *testing.T) {
	called := false
	c := &mockConsultant{
		current: "agile-v1.0-4bit",
		askFunc: func(ctx context.Context, question string) (ports.Response, error) {
			called = true
			if question != "Как провести ретроспективу?" {
				t.Fatalf("unexpected question: %s", question)
			}
			return ports.Response{
				Verdict:    "Провести ретроспективу",
				Principle:  "Scrum",
				Reasoning:  "Важно",
				Suggestion: "Используйте Start-Stop-Continue",
				Metrics:    ports.GenerationMetrics{TTFT: 100 * time.Millisecond, TokensPerSec: 50, Tokens: 10},
			}, nil
		},
	}
	m := New(c, &mockSwitcher{})
	m.input.SetValue("Как провести ретроспективу?")

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected ask command")
	}
	model := newModel.(Model)
	if !model.loading {
		t.Fatal("model should be loading")
	}

	msg := cmd()
	rm, ok := msg.(responseMsg)
	if !ok {
		t.Fatalf("expected responseMsg, got %T", msg)
	}
	if rm.err != nil {
		t.Fatalf("unexpected error: %v", rm.err)
	}
	if !called {
		t.Fatal("Ask was not called")
	}

	final, _ := model.Update(rm)
	finalModel := final.(Model)
	if finalModel.loading {
		t.Fatal("loading should be false after response")
	}
	if finalModel.lastResp.Verdict != "Провести ретроспективу" {
		t.Fatalf("unexpected verdict: %s", finalModel.lastResp.Verdict)
	}
	rendered := strings.Join(finalModel.history, "\n")
	if !strings.Contains(rendered, "Провести ретроспективу") {
		t.Fatalf("history should contain the verdict text, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Вердикт") {
		t.Fatal("history should contain the verdict label")
	}
}

func TestResponseError(t *testing.T) {
	c := &mockConsultant{
		current: "agile-v1.0-4bit",
		askFunc: func(ctx context.Context, question string) (ports.Response, error) {
			return ports.Response{}, errors.New("generation failed")
		},
	}
	m := New(c, &mockSwitcher{})
	m.input.SetValue("test")

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := newModel.(Model)
	msg := cmd()
	rm := msg.(responseMsg)

	final, _ := model.Update(rm)
	finalModel := final.(Model)
	if finalModel.err == nil {
		t.Fatal("expected error in model")
	}
	if finalModel.loading {
		t.Fatal("loading should be false after error")
	}
}

func TestTabTriggersSwitch(t *testing.T) {
	switchCalled := 0
	s := &mockSwitcher{
		switchFunc: func(ctx context.Context, modelName string) (string, error) {
			switchCalled++
			return "agile-v1.1-4bit", nil
		},
	}
	m := New(&mockConsultant{current: "agile-v1.0-4bit"}, s)

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd == nil {
		t.Fatal("expected switch command")
	}
	model := newModel.(Model)
	if !model.switching {
		t.Fatal("model should be switching")
	}

	// Repeated Tab while switching must be ignored.
	_, cmd2 := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd2 != nil {
		t.Fatal("second Tab during switch should be ignored")
	}

	msg := cmd()
	sm, ok := msg.(switchMsg)
	if !ok {
		t.Fatalf("expected switchMsg, got %T", msg)
	}
	if sm.err != nil {
		t.Fatalf("unexpected switch error: %v", sm.err)
	}
	if sm.name != "agile-v1.1-4bit" {
		t.Fatalf("unexpected model name: %s", sm.name)
	}
	if switchCalled != 1 {
		t.Fatalf("expected 1 switch call, got %d", switchCalled)
	}

	final, _ := model.Update(sm)
	finalModel := final.(Model)
	if finalModel.switching {
		t.Fatal("model should not be switching after switchMsg")
	}
	if finalModel.status != "Модель: agile-v1.1-4bit" {
		t.Fatalf("unexpected status: %s", finalModel.status)
	}

	switchCount := 0
	for _, line := range finalModel.history {
		if line == "Переключено на модель: agile-v1.1-4bit" {
			switchCount++
		}
	}
	if switchCount != 1 {
		t.Fatalf("expected exactly one switch message, got %d", switchCount)
	}
}

func TestSwitchDoesNotDuplicate(t *testing.T) {
	s := &mockSwitcher{
		switchFunc: func(ctx context.Context, modelName string) (string, error) {
			return "agile-v1.1-4bit", nil
		},
	}
	m := New(&mockConsultant{current: "agile-v1.0-4bit"}, s)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	sm1 := cmd().(switchMsg)
	final1, _ := m.Update(sm1)
	model1 := final1.(Model)

	_, cmd2 := model1.Update(tea.KeyMsg{Type: tea.KeyTab})
	sm2 := cmd2().(switchMsg)
	final2, _ := model1.Update(sm2)
	model2 := final2.(Model)

	switchCount := 0
	for _, line := range model2.history {
		if line == "Переключено на модель: agile-v1.1-4bit" {
			switchCount++
		}
	}
	if switchCount != 1 {
		t.Fatalf("expected exactly one switch message after duplicate switches, got %d", switchCount)
	}
}

func TestSwitchError(t *testing.T) {
	s := &mockSwitcher{
		switchFunc: func(ctx context.Context, modelName string) (string, error) {
			return "", errors.New("switch failed")
		},
	}
	m := New(&mockConsultant{current: "agile-v1.0-4bit"}, s)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	msg := cmd()
	sm := msg.(switchMsg)

	final, _ := m.Update(sm)
	finalModel := final.(Model)
	if finalModel.err == nil {
		t.Fatal("expected switch error in model")
	}
	if finalModel.switching {
		t.Fatal("model should not be switching after error")
	}
}

func TestTabDoesNotGoToInput(t *testing.T) {
	s := &mockSwitcher{
		switchFunc: func(ctx context.Context, modelName string) (string, error) {
			return "agile-v1.1-4bit", nil
		},
	}
	m := New(&mockConsultant{current: "agile-v1.0-4bit"}, s)
	m.input.SetValue("hello")

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	model := newModel.(Model)
	if model.input.Value() != "hello" {
		t.Fatal("Tab should not modify input value")
	}
}

func TestCtrlCQuits(t *testing.T) {
	m := New(&mockConsultant{current: "agile-v1.0-4bit"}, &mockSwitcher{})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
}

func TestCtrlLClearsHistory(t *testing.T) {
	m := New(&mockConsultant{current: "agile-v1.0-4bit"}, &mockSwitcher{})
	m.addToHistory("test message")

	final, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	finalModel := final.(Model)
	if len(finalModel.history) != 0 {
		t.Fatalf("expected empty history, got %d entries", len(finalModel.history))
	}
	if strings.TrimSpace(finalModel.viewport.View()) != "" {
		t.Fatal("expected empty viewport after clear")
	}
}

func TestResponseFormatting(t *testing.T) {
	c := &mockConsultant{
		current: "agile-v1.0-4bit",
		askFunc: func(ctx context.Context, question string) (ports.Response, error) {
			return ports.Response{
				Verdict:    "Вердикт текст",
				Principle:  "Принцип текст",
				Reasoning:  "Обоснование текст\nВторая строка",
				Suggestion: "Рекомендация текст",
			}, nil
		},
	}
	m := New(c, &mockSwitcher{})
	m.input.SetValue("test")

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := newModel.(Model)
	rm := cmd().(responseMsg)
	final, _ := model.Update(rm)
	finalModel := final.(Model)

	rendered := strings.Join(finalModel.history, "\n")
	if !strings.Contains(rendered, "Вердикт текст") {
		t.Fatal("rendered output should contain verdict text")
	}
	if !strings.Contains(rendered, "Принцип текст") {
		t.Fatal("rendered output should contain principle text")
	}
	if !strings.Contains(rendered, "Вторая строка") {
		t.Fatal("rendered output should preserve newlines from the model")
	}

	// Fields should be separated by blank lines.
	if !strings.Contains(rendered, "Вердикт текст\n\nПринцип") {
		t.Fatalf("fields should be separated by blank lines, got:\n%s", rendered)
	}
	// Response should end with a visual separator line.
	if !strings.Contains(rendered, "──") {
		t.Fatalf("response should end with a separator line, got:\n%s", rendered)
	}
}

func TestPrettifyText(t *testing.T) {
	input := "1. Разделите проект на эпик и спринты: Разделите проект. 2. Создайте интеграционную среду: Разработайте среду. 3. Используйте Scrum: Scrum — это популярная форма Agile."
	out := prettifyText(input)
	if !strings.Contains(out, "1. Разделите проект на эпик и спринты:") {
		t.Fatal("first item should be preserved")
	}
	if !strings.Contains(out, "\n2. Создайте интеграционную среду:") {
		t.Fatalf("second item should start on a new line, got:\n%s", out)
	}
	if !strings.Contains(out, "\n3. Используйте Scrum:") {
		t.Fatalf("third item should start on a new line, got:\n%s", out)
	}
}

func TestWrapPreserveNewlines(t *testing.T) {
	input := "first line\n\nsecond line with many words that should be wrapped separately\nthird"
	out := wrapPreserveNewlines(input, 20)
	if !strings.Contains(out, "first line") {
		t.Fatal("should preserve first line")
	}
	if !strings.Contains(out, "third") {
		t.Fatal("should preserve third line")
	}
	// Ensure blank line is preserved.
	if !strings.Contains(out, "first line\n\nsecond") {
		t.Fatalf("blank line should be preserved, got:\n%s", out)
	}
}

func TestWindowSizeMsg(t *testing.T) {
	m := New(&mockConsultant{current: "agile-v1.0-4bit"}, &mockSwitcher{})
	final, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	finalModel := final.(Model)
	if finalModel.width != 120 || finalModel.height != 40 {
		t.Fatalf("unexpected dimensions: %dx%d", finalModel.width, finalModel.height)
	}
	if finalModel.viewport.Width != 120 {
		t.Fatalf("unexpected viewport width: %d", finalModel.viewport.Width)
	}
	if finalModel.viewport.Height != 33 {
		t.Fatalf("unexpected viewport height: %d", finalModel.viewport.Height)
	}
}

func TestViewportScrollKeys(t *testing.T) {
	m := New(&mockConsultant{current: "agile-v1.0-4bit"}, &mockSwitcher{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	for i := 0; i < 50; i++ {
		m.addToHistory("line")
	}

	// Scroll up and verify viewport moved.
	before := m.viewport.YOffset
	final, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	finalModel := final.(Model)
	if finalModel.viewport.YOffset >= before {
		t.Fatal("PgUp should scroll viewport up")
	}

	final2, _ := finalModel.Update(tea.KeyMsg{Type: tea.KeyDown})
	finalModel2 := final2.(Model)
	if finalModel2.viewport.YOffset <= finalModel.viewport.YOffset {
		t.Fatal("Down should scroll viewport down")
	}
}
