package ui

import (
	"agile-consultant/internal/ports"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Consultant interface {
	Ask(ctx context.Context, question string) (ports.Response, error)
	CurrentModel() string
}

type Switcher interface {
	Switch(ctx context.Context, modelName string) (string, error)
}

type Model struct {
	width      int
	height     int
	input      textinput.Model
	viewport   viewport.Model
	history    []string
	consultant Consultant
	switcher   Switcher
	loading    bool
	switching  bool
	status     string
	lastResp   ports.Response
	err        error
}

type responseMsg struct {
	resp ports.Response
	err  error
}

type switchMsg struct {
	name string
	err  error
}

func New(consultant Consultant, switcher Switcher) *Model {
	ti := textinput.New()
	ti.Placeholder = "Задайте вопрос по Agile..."
	ti.Prompt = "> "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
	ti.Focus()
	ti.CharLimit = 512
	ti.Width = 80

	welcome := "Добро пожаловать в Agile-консультант. Введите вопрос и нажмите Enter."
	vp := viewport.New(80, 20)
	vp.SetContent(wrap(welcome, 78))

	return &Model{
		width:      80,
		input:      ti,
		viewport:   vp,
		consultant: consultant,
		switcher:   switcher,
		status:     fmt.Sprintf("Модель: %s", consultant.CurrentModel()),
		history:    []string{welcome},
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 4
		m.viewport.Width = msg.Width
		if msg.Height > 7 {
			m.viewport.Height = msg.Height - 7
		} else {
			m.viewport.Height = 3
		}
		m.refreshViewport()
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			if m.loading || m.switching {
				return m, nil
			}
			q := strings.TrimSpace(m.input.Value())
			if q == "" {
				return m, nil
			}
			m.loading = true
			m.err = nil
			m.addWrapped(fmt.Sprintf("> %s", q))
			m.input.SetValue("")
			return m, askCmd(m.consultant, q)
		case tea.KeyTab, tea.KeyF5:
			if m.loading || m.switching {
				return m, nil
			}
			m.switching = true
			return m, switchCmd(m.switcher)
		case tea.KeyCtrlL:
			m.history = []string{}
			m.refreshViewport()
			return m, nil
		case tea.KeyPgUp:
			m.viewport.HalfViewUp()
			return m, nil
		case tea.KeyPgDown:
			m.viewport.HalfViewDown()
			return m, nil
		case tea.KeyUp:
			m.viewport.LineUp(1)
			return m, nil
		case tea.KeyDown:
			m.viewport.LineDown(1)
			return m, nil
		}
	case responseMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.addWrapped(fmt.Sprintf("Ошибка: %v", msg.err))
		} else {
			m.lastResp = msg.resp
			m.addResponse(msg.resp)
			m.status = fmt.Sprintf("Модель: %s | TTFT: %s | %.1f tok/s",
				m.consultant.CurrentModel(), msg.resp.Metrics.TTFT.Round(timeMs), msg.resp.Metrics.TokensPerSec)
		}
	case switchMsg:
		m.switching = false
		if msg.err != nil {
			m.err = msg.err
			m.addToHistory(fmt.Sprintf("Ошибка переключения: %v", msg.err))
		} else {
			m.status = fmt.Sprintf("Модель: %s", msg.name)
			line := fmt.Sprintf("Переключено на модель: %s", msg.name)
			if len(m.history) == 0 || m.history[len(m.history)-1] != line {
				m.addWrapped(line)
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *Model) addToHistory(line string) {
	m.history = append(m.history, line)
	m.refreshViewport()
}

func (m *Model) addWrapped(line string) {
	m.addToHistory(wrap(line, m.width-2))
}

func (m *Model) refreshViewport() {
	m.viewport.SetContent(strings.Join(m.history, "\n"))
	m.viewport.GotoBottom()
}

func (m *Model) addResponse(resp ports.Response) {
	w := m.width - 4
	if w < 20 {
		w = m.width - 2
	}

	var blocks []string
	if resp.Verdict != "" {
		blocks = append(blocks, formatField("Вердикт", resp.Verdict, verdictStyle, w))
	}
	if resp.Principle != "" {
		blocks = append(blocks, formatField("Принцип", resp.Principle, principleStyle, w))
	}
	if resp.Reasoning != "" {
		blocks = append(blocks, formatField("Обоснование", resp.Reasoning, reasoningStyle, w))
	}
	if resp.Suggestion != "" {
		blocks = append(blocks, formatField("Рекомендация", resp.Suggestion, suggestStyle, w))
	}
	if resp.Raw != "" && resp.Verdict == "" {
		blocks = append(blocks, wrapPreserveNewlines(resp.Raw, w))
	}

	for i, block := range blocks {
		m.addToHistory(block)
		if i < len(blocks)-1 {
			m.addToHistory("")
		}
	}
	if len(blocks) > 0 {
		m.addToHistory(dividerStyle.Render(strings.Repeat("─", max(0, m.width-2))))
	}
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Agile-консультант"))
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	if m.loading {
		b.WriteString(spinnerStyle.Render("Генерация..."))
		b.WriteString("\n")
	}
	b.WriteString(statusStyle.Render(m.status))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Enter — отправить | Tab — модель | Ctrl+L — очистить | PgUp/PgDown — прокрутка | Ctrl+C — выход"))
	b.WriteString("\n")
	b.WriteString(dividerStyle.Render(strings.Repeat("─", max(0, m.width))))
	b.WriteString("\n")
	b.WriteString(m.input.View())
	return b.String()
}

func askCmd(c Consultant, question string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		resp, err := c.Ask(ctx, question)
		return responseMsg{resp: resp, err: err}
	}
}

func switchCmd(s Switcher) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		name, err := s.Switch(ctx, "")
		return switchMsg{name: name, err: err}
	}
}

const defaultTimeout = 120 * time.Second

var (
	timeMs         = 1 * time.Millisecond
	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4")).PaddingTop(1).PaddingBottom(1)
	spinnerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#F4D03F"))
	statusStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#2ECC71"))
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	dividerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	verdictStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E74C3C"))
	principleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3498DB"))
	reasoningStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F39C12"))
	suggestStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#2ECC71"))
)

func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var lines []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len()+len(w)+1 > width {
			lines = append(lines, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString(" ")
		}
		cur.WriteString(w)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return strings.Join(lines, "\n")
}

func wrapPreserveNewlines(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrap(line, width))
	}
	return strings.Join(out, "\n")
}

func formatField(label, text string, style lipgloss.Style, width int) string {
	header := style.Render(label + ":")
	body := wrapPreserveNewlines(prettifyText(text), width)
	return header + "\n" + body
}

var (
	numberedListRe = regexp.MustCompile(` (\d+)\. `)
	markdownListRe = regexp.MustCompile(` ([\*\-]) `)
)

func prettifyText(s string) string {
	// "1. ... 2. ... 3. ..." -> "1. ...\n2. ...\n3. ..."
	s = numberedListRe.ReplaceAllString(s, "\n$1. ")
	// "- a - b" -> "- a\n- b"
	s = markdownListRe.ReplaceAllString(s, "\n$1 ")
	// убрать лишние пробелы в начале строк после разбиения
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.Join(lines, "\n")
}
