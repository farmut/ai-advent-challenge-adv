package main

import (
	"strings"
	"testing"

	ui "github.com/metaspartan/gotui/v5"
	"github.com/metaspartan/gotui/v5/widgets"
)

// visible renders a markup row the way the List does and returns the glyphs a
// user would actually see (zero-width escape spaces stripped).
func visible(row string) string {
	cells := ui.ParseStyles(row, ui.NewStyle(ui.ColorWhite))
	var b strings.Builder
	for _, c := range cells {
		b.WriteRune(c.Rune)
	}
	return strings.ReplaceAll(b.String(), zwsp, "")
}

func newTestState() *tuiState {
	// The widgets are plain structs — no ui.Init needed to exercise the
	// input-history, scroll and header logic.
	return &tuiState{
		header: widgets.NewParagraph(),
		log:    widgets.NewList(),
		input:  widgets.NewTextArea(),
		follow: true,
	}
}

func TestInputHistory_UpDownNavigation(t *testing.T) {
	s := newTestState()
	s.pushHistory("first")
	s.pushHistory("second")
	s.pushHistory("third")

	// Typing a draft, then browsing up must preserve and restore the draft.
	s.input.Text = "draft"
	s.historyPrev() // → third
	if s.input.Text != "third" {
		t.Fatalf("↑ once want %q, got %q", "third", s.input.Text)
	}
	s.historyPrev() // → second
	s.historyPrev() // → first
	if s.input.Text != "first" {
		t.Fatalf("↑ to oldest want %q, got %q", "first", s.input.Text)
	}
	s.historyPrev() // clamp at oldest
	if s.input.Text != "first" {
		t.Fatalf("↑ past oldest must clamp, got %q", s.input.Text)
	}
	s.historyNext() // → second
	s.historyNext() // → third
	s.historyNext() // → back to the live draft
	if s.input.Text != "draft" {
		t.Fatalf("↓ back to draft want %q, got %q", "draft", s.input.Text)
	}
	// Cursor must sit at the end (rune count) of the restored single-line text.
	if s.input.Cursor.X != 5 || s.input.Cursor.Y != 0 {
		t.Errorf("cursor want {5,0}, got %v", s.input.Cursor)
	}
}

// setInput on multi-line text must place the cursor at the end of the LAST line.
func TestSetInput_MultilineCursorAtEnd(t *testing.T) {
	s := newTestState()
	s.setInput("первый абзац\nвторой абзац")
	if s.input.Cursor.Y != 1 {
		t.Errorf("cursor line want 1, got %d", s.input.Cursor.Y)
	}
	if want := len([]rune("второй абзац")); s.input.Cursor.X != want {
		t.Errorf("cursor col want %d, got %d", want, s.input.Cursor.X)
	}
}

func TestInputHistory_SkipsConsecutiveDuplicates(t *testing.T) {
	s := newTestState()
	s.pushHistory("a")
	s.pushHistory("a") // duplicate — must not grow history
	s.pushHistory("b")
	if len(s.history) != 2 {
		t.Fatalf("history want 2 entries, got %d (%v)", len(s.history), s.history)
	}
}

func TestScroll_FollowReleaseAndRearm(t *testing.T) {
	s := newTestState()
	for i := 0; i < 20; i++ {
		s.appendLog("line")
	}
	// Following: newest line selected.
	if s.log.SelectedRow != 19 {
		t.Fatalf("following should pin to last row, got %d", s.log.SelectedRow)
	}
	// Scroll up releases follow.
	s.scroll(-5)
	if s.follow {
		t.Fatalf("scrolling up must release follow")
	}
	if s.log.SelectedRow != 14 {
		t.Fatalf("scroll up want row 14, got %d", s.log.SelectedRow)
	}
	// New output while scrolled up must NOT snap to bottom.
	s.appendLog("streamed")
	if s.log.SelectedRow != 14 {
		t.Fatalf("must stay put while not following, got %d", s.log.SelectedRow)
	}
	// Scrolling back to the bottom re-arms follow.
	s.scroll(100)
	if !s.follow {
		t.Fatalf("scrolling to bottom must re-arm follow")
	}
	s.appendLog("newest")
	if s.log.SelectedRow != len(s.lines)-1 {
		t.Fatalf("following again should pin to last, got %d/%d", s.log.SelectedRow, len(s.lines))
	}
}

// Colourising a line must never change the visible text — the whole point of the
// escaping is that code and brackets survive the markup parser intact.
func TestRenderRow_PreservesVisibleText(t *testing.T) {
	s := newTestState()
	samples := []string{
		"обычный ответ без скобок",
		"› проверь arr[i](y)",
		`[orchestrator] round 1 → spawn "x": s[1:len(s)]`,
		"[subagent researcher] RAG grounded on 3 chunk(s)",
		"[rag] retrieved 5 chunk(s)",
		"[mcp] 2 server(s)",
		"=== Память оркестратора ===",
		"  • researcher [только LLM]",
		"see [docs](http://x) and code []byte(x)",
		"⚠ ошибка: map[k](v) broke",
	}
	for _, raw := range samples {
		if got := visible(s.renderRow(raw)); got != raw {
			t.Errorf("visible text changed:\n raw = %q\n got = %q", raw, got)
		}
	}
}

// Inside a fenced block, code renders verbatim behind a "│ " gutter, and the
// fences flip the state.
func TestRenderRow_CodeBlock(t *testing.T) {
	s := newTestState()
	if got := visible(s.renderRow("```go")); !strings.Contains(got, "код") {
		t.Fatalf("fence should render a separator, got %q", got)
	}
	if !s.inCode {
		t.Fatal("opening fence must enter code mode")
	}
	code := `data := arr[i](y) // s[1:len(s)]`
	if got := visible(s.renderRow(code)); got != "│ "+code {
		t.Fatalf("code row visible = %q, want %q", got, "│ "+code)
	}
	_ = s.renderRow("```")
	if s.inCode {
		t.Fatal("closing fence must leave code mode")
	}
}

func TestEscMarkup_RoundTrip(t *testing.T) {
	for _, s := range []string{
		"x := arr[i](y)", "see [docs](http://x)", "s[1:len(s)]",
		"a[b][c](d)", "[[[weird]]]", "json := []map[string]any{}",
		"no brackets here", "mix ]( )[ ][",
	} {
		if got := visible(escMarkup(s)); got != s {
			t.Errorf("escMarkup round-trip: %q -> %q", s, got)
		}
	}
}

// Markdown in a plan/answer must render as formatted output: hashes stripped
// from headings, "- " unified to "• ", and inline **bold** / `code` markers
// removed — leaving the readable text (never the raw markdown syntax).
func TestRenderRow_MarkdownFormatting(t *testing.T) {
	s := newTestState()
	cases := []struct{ raw, want string }{
		{"## План действий", "План действий"},
		{"### Этап 1", "Этап 1"},
		{"- изучить структуру", "• изучить структуру"},
		{"+ другой пункт", "• другой пункт"},
		{"1. написать модуль", "1. написать модуль"},
		{"2) добавить тесты", "2) добавить тесты"},
		{"обычный **важный** момент", "обычный важный момент"},
		{"вызвать `go build` тут", "вызвать go build тут"},
		{"текст без разметки", "текст без разметки"},
		// Unfenced code must survive verbatim (no bold/bullet mangling).
		{"result = a**b**c", "result = a**b**c"},
		{"power = 2**8 * 3**2", "power = 2**8 * 3**2"},
		{"* ptr = value", "* ptr = value"},
		{"arr := []int{1, 2}", "arr := []int{1, 2}"},
	}
	for _, c := range cases {
		if got := visible(s.renderRow(c.raw)); got != c.want {
			t.Errorf("renderRow(%q) visible = %q, want %q", c.raw, got, c.want)
		}
	}
}

// Bold/code inner text that contains bracket-markup tokens must still render its
// glyphs verbatim (colour is dropped, correctness kept).
func TestMdInline_PreservesBracketsInSpans(t *testing.T) {
	for _, raw := range []string{
		"call **arr[i](y)** now",
		"see `map[k](v)` here",
		"plain arr[i](y) text",
	} {
		want := strings.NewReplacer("**", "", "`", "").Replace(raw)
		if got := visible(mdInline(raw)); got != want {
			t.Errorf("mdInline(%q) visible = %q, want %q", raw, got, want)
		}
	}
}

func TestToTopBottom(t *testing.T) {
	s := newTestState()
	for i := 0; i < 10; i++ {
		s.appendLog("x")
	}
	s.toTop()
	if s.follow || s.log.SelectedRow != 0 {
		t.Fatalf("toTop want row 0 + follow off, got row %d follow %v", s.log.SelectedRow, s.follow)
	}
	s.toBottom()
	if !s.follow || s.log.SelectedRow != len(s.lines)-1 {
		t.Fatalf("toBottom want last row + follow on, got row %d follow %v", s.log.SelectedRow, s.follow)
	}
}

func TestWrapRunes(t *testing.T) {
	if got := wrapRunes("abcdef", 0); len(got) != 1 || got[0] != "abcdef" {
		t.Fatalf("w<=0 must disable wrapping: %v", got)
	}
	got := wrapRunes("абвгдежз", 3) // Cyrillic: rune-aware, not byte-aware
	want := []string{"абв", "где", "жз"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunk %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestAppendLog_WrapsLongLines(t *testing.T) {
	s := newTestState()
	s.wrapW = 10
	s.appendLog(strings.Repeat("а", 25)) // one logical line → 3 screen rows
	if len(s.lines) != 3 {
		t.Fatalf("want 3 wrapped rows, got %d: %v", len(s.lines), s.lines)
	}
	if len(s.raw) != 1 {
		t.Fatalf("raw must keep the unwrapped line, got %d", len(s.raw))
	}
	// Bottom-follow must point at the LAST screen row.
	if s.log.SelectedRow != 2 {
		t.Fatalf("follow should pin to last wrapped row, got %d", s.log.SelectedRow)
	}
}

func TestAppendLog_WrapKeepsCodeGutterAndFenceState(t *testing.T) {
	s := newTestState()
	s.wrapW = 12
	s.appendLog("```")
	s.appendLog(strings.Repeat("x", 25)) // code line → chunks of wrapW-2
	s.appendLog("```")
	if s.inCode {
		t.Fatal("fence state must close")
	}
	// 1 fence + 3 code chunks (25/10) + 1 fence
	if len(s.lines) != 5 {
		t.Fatalf("want 5 rows, got %d: %v", len(s.lines), s.lines)
	}
	for i := 1; i <= 3; i++ {
		if !strings.HasPrefix(visible(s.lines[i]), "│ ") {
			t.Fatalf("code chunk %d must carry the gutter: %q", i, visible(s.lines[i]))
		}
	}
}

func TestRebuildLog_RewrapsOnNewWidth(t *testing.T) {
	s := newTestState()
	s.wrapW = 10
	s.appendLog(strings.Repeat("б", 30))
	if len(s.lines) != 3 {
		t.Fatalf("precondition: want 3 rows at width 10, got %d", len(s.lines))
	}
	s.wrapW = 30
	s.rebuildLog()
	if len(s.lines) != 1 {
		t.Fatalf("after widening want 1 row, got %d", len(s.lines))
	}
	if !s.follow || s.log.SelectedRow != 0 {
		t.Fatalf("follow position lost: follow=%t row=%d", s.follow, s.log.SelectedRow)
	}
}

// [loop] rows get the magenta bold tag and keep their visible text, even when
// the loop message itself carries markup-looking brackets from the model.
func TestRenderRow_LoopPrefix(t *testing.T) {
	s := newTestState()
	row := s.renderRow("[loop] api-1 → testing: команды пройдены")
	if !strings.Contains(row, "fg:magenta,mod:bold") {
		t.Fatalf("want the loop style, got %q", row)
	}
	if got := visible(row); got != "[loop] api-1 → testing: команды пройдены" {
		t.Fatalf("visible text changed: %q", got)
	}
	row = s.renderRow("[loop] сгенерировано: arr[i](y) = s[1:len(s)]")
	if got := visible(row); got != "[loop] сгенерировано: arr[i](y) = s[1:len(s)]" {
		t.Fatalf("brackets from the model must survive verbatim: %q", got)
	}
}

// [security] rows are red; a line naming Critical/High severities paints the
// whole row red, other lines only the tag — and visible text never changes.
func TestRenderRow_SecurityPrefix(t *testing.T) {
	s := newTestState()
	alarm := "[security] api-1: 3 находок (1 Critical, 2 High)"
	row := s.renderRow(alarm)
	if !strings.Contains(row, "fg:red") {
		t.Fatalf("want the security style, got %q", row)
	}
	if strings.Count(row, "fg:red") < 2 {
		t.Fatalf("a Critical/High line must be red as a whole: %q", row)
	}
	if got := visible(row); got != alarm {
		t.Fatalf("visible text changed: %q", got)
	}

	calm := "[security] api-1: 2 находок (2 Low)"
	row = s.renderRow(calm)
	if strings.Count(row, "fg:red") != 1 {
		t.Fatalf("a Low-only line keeps tag-only colouring: %q", row)
	}
	if got := visible(row); got != calm {
		t.Fatalf("visible text changed: %q", got)
	}
}

// While a loop runs, the header's second line is the live LOOP progress
// instead of the key hints; after it stops, the hints return.
func TestRefreshHeader_LoopLine(t *testing.T) {
	s := newTestState()
	s.status = "model=test"
	s.refreshHeader()
	if strings.Contains(s.header.Text, "LOOP") {
		t.Fatalf("no loop line before a loop starts: %q", s.header.Text)
	}
	s.loopRunning = true
	s.loopHdr = loopHeader{cur: 3, total: 7, profile: "coder", secIter: 2, secMax: 3, tokens: 412_000, cost: 0.31, costMax: 1}
	s.refreshHeader()
	want := "LOOP 3/7 · coder · sec-iter 2/3 · 412k tok · $0.31/1.00"
	if !strings.Contains(s.header.Text, want) {
		t.Fatalf("header missing %q: %q", want, s.header.Text)
	}
	if !strings.HasPrefix(s.header.Text, "model=test\n") {
		t.Fatalf("status line must stay first: %q", s.header.Text)
	}
	s.loopRunning = false
	s.refreshHeader()
	if strings.Contains(s.header.Text, "LOOP 3/7") {
		t.Fatalf("loop line must go away once the loop stops: %q", s.header.Text)
	}
}

// During a busy loop only /loop status|stop, /findings and /copy get through;
// everything else — including starting a new loop — is refused.
func TestLoopAllowedBusy(t *testing.T) {
	cases := []struct {
		kind cmdKind
		sub  string
		want bool
	}{
		{cmdLoop, "status", true},
		{cmdLoop, "stop", true},
		{cmdLoop, "resume", false},
		{cmdLoop, "goal", false},
		{cmdLoop, "", false},
		{cmdFindings, "", true},
		{cmdCopy, "", true},
		{cmdHelp, "", false},
		{cmdInput, "", false},
		{cmdQuit, "", false},
	}
	for _, c := range cases {
		if got := loopAllowedBusy(c.kind, c.sub); got != c.want {
			t.Errorf("loopAllowedBusy(%d, %q) = %t, want %t", c.kind, c.sub, got, c.want)
		}
	}
}
