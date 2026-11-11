package usecase

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// ContextStrategy determines how conversation history is managed between turns.
type ContextStrategy string

const (
	StrategyNone          ContextStrategy = ""
	StrategySlidingWindow ContextStrategy = "sliding-window"
	StrategyStickyFacts   ContextStrategy = "sticky-facts"
	StrategyBranching     ContextStrategy = "branching"
)

// ChatConfig holds all per-call parameters for the chat use case.
type ChatConfig struct {
	Model          string
	FullQuery      string // user query with format instruction already appended
	SystemMessage  string
	MaxTokens      int
	Stop           []string
	Temperature    float64 // negative = provider default
	HistoryLimit   int
	SummaryEnabled bool
	Strategy       ContextStrategy
	WindowSize     int
	MemoryUpdate   bool
	Debug          bool
	ShowTokens     bool
	// OnResponse, when set, sees every LLM response of the turn (including
	// tool-call rounds). A non-nil error aborts the turn with that error.
	OnResponse func(port.LLMResponse) error
	// MaxToolRounds caps the tool-calling loop: positive — that many rounds,
	// 0 — the built-in default, negative — no cap. When the cap is hit the
	// turn ends with an explanatory note (not a model answer) so the caller
	// sees the real reason instead of a silently empty reply.
	MaxToolRounds int
}

// ChatResult is the value returned by ChatUseCase.Execute.
type ChatResult struct {
	Content          string
	Usage            domain.Usage
	Stats            domain.SessionStats
	PrevPromptTokens int
	// GuardBlocked is true when the guard proxy replaced the model's answer
	// (X-Guard-Output: blocked / finish_reason content_filter). Content is
	// then the proxy's refusal text, NOT the model's answer: callers must
	// treat the turn as failed.
	GuardBlocked bool
	// GuardReason carries the proxy's output findings for a blocked answer.
	GuardReason string
	// ToolCalls counts executed tool calls; RedactedToolCalls counts calls
	// that were refused because the proxy had rewritten their arguments.
	ToolCalls         int
	RedactedToolCalls int
	// CostUSD is this turn's estimated cost: the proxy-reported cost when the
	// guard sends one (see OnResponse), otherwise the pricing-table estimate
	// for the model.
	CostUSD float64
}

// RedactedToolCallResult is the tool result returned to the model instead of
// executing a call whose arguments the guard proxy rewrote. Running it would
// execute arguments the model never produced (e.g. a file with "[REDACTED]"
// where code was).
const RedactedToolCallResult = "error: аргументы этого вызова изменены guard'ом прокси (найдены секреты или опасные фрагменты), поэтому вызов НЕ выполнен. Повтори вызов без секретов, ключей и URL — значения бери из переменных окружения."

// ChatUseCase orchestrates a single LLM exchange with full memory management.
type ChatUseCase struct {
	llm     port.LLMClient
	history port.HistoryRepository
	stats   port.StatsRepository
	summary port.SummaryRepository
	facts   port.FactsRepository
	wm      port.WorkingMemoryRepository
	ltm     port.LongTermMemoryRepository
	profile port.UserProfileRepository
}

// NewChatUseCase creates a ChatUseCase with all required dependencies injected.
func NewChatUseCase(
	llm port.LLMClient,
	history port.HistoryRepository,
	stats port.StatsRepository,
	summary port.SummaryRepository,
	facts port.FactsRepository,
	wm port.WorkingMemoryRepository,
	ltm port.LongTermMemoryRepository,
	profile port.UserProfileRepository,
) *ChatUseCase {
	return &ChatUseCase{
		llm: llm, history: history, stats: stats,
		summary: summary, facts: facts, wm: wm, ltm: ltm, profile: profile,
	}
}

// Execute runs one full chat turn: load state → build prompt → call LLM → update memory → persist.
func (uc *ChatUseCase) Execute(ctx context.Context, cfg ChatConfig) (ChatResult, error) {
	// A4: a top-level query carrying invisible characters is cleaned and
	// annotated before anything else sees it (the proxy would strip the
	// characters silently, revealing a ready-made instruction).
	cfg = sanitizeInvisibleQuery(cfg)

	// ---- Load all state ----
	history, _ := uc.history.Load()
	stats, _ := uc.stats.Load()
	wmData, _ := uc.wm.Load()
	ltmData, _ := uc.ltm.Load()
	profileData, _ := uc.profile.Load()

	var factsData domain.FactsStore
	if cfg.Strategy == StrategyStickyFacts {
		factsData, _ = uc.facts.Load()
	}

	var existingSummary string
	if cfg.SummaryEnabled && (cfg.Strategy == StrategyNone || cfg.Strategy == StrategyBranching) {
		existingSummary, _ = uc.summary.Load()
	}

	// ---- Build system content: Profile → LTM → WM → [sticky-facts] → [user system] → [summary] ----
	systemContent := cfg.SystemMessage

	if cfg.Strategy == StrategyStickyFacts {
		if block := FactsSystemBlock(factsData); block != "" {
			systemContent = prepend(block, systemContent)
		}
	}
	if block := WMSystemBlock(wmData); block != "" {
		systemContent = prepend(block, systemContent)
	}
	if block := LTMSystemBlock(ltmData); block != "" {
		systemContent = prepend(block, systemContent)
	}
	if block := ProfileSystemBlock(profileData); block != "" {
		systemContent = prepend(block, systemContent)
	}
	if existingSummary != "" {
		appendBlock := "Summary of earlier conversation:\n" + existingSummary
		if systemContent != "" {
			systemContent += "\n\n---\n" + appendBlock
		} else {
			systemContent = appendBlock
		}
	}

	// ---- Select history slice ----
	historyForPrompt := history
	if cfg.Strategy == StrategySlidingWindow || cfg.Strategy == StrategyStickyFacts {
		historyForPrompt = SlidingWindow(history, cfg.WindowSize)
	}

	// ---- Build messages ----
	var messages []domain.Message
	if systemContent != "" {
		messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: systemContent})
	}
	messages = append(messages, historyForPrompt...)
	messages = append(messages, domain.Message{Role: domain.RoleUser, Content: cfg.FullQuery})

	// ---- Pre-call context estimation ----
	if cfg.ShowTokens {
		if p, known := domain.PricingFor(cfg.Model); known && p.ContextWindow > 0 {
			est := domain.EstimateMessagesTokens(messages)
			pct := float64(est) / float64(p.ContextWindow) * 100
			remaining := p.ContextWindow - est
			status := domain.ContextStatus(pct)
			if remaining < 0 {
				fmt.Fprintf(os.Stderr,
					"[pre-call] estimated context: ~%d / %d tokens (%.1f%%) — EXCEEDS LIMIT by %d tokens  %s\n",
					est, p.ContextWindow, pct, -remaining, status)
			} else {
				fmt.Fprintf(os.Stderr,
					"[pre-call] estimated context: ~%d / %d tokens (%.1f%%, ~%d remaining)  %s\n",
					est, p.ContextWindow, pct, remaining, status)
			}
		}
	}

	// ---- Call LLM ----
	resp, err := uc.llm.Chat(ctx, port.LLMRequest{
		Model:       cfg.Model,
		Messages:    messages,
		MaxTokens:   cfg.MaxTokens,
		Stop:        cfg.Stop,
		Temperature: cfg.Temperature,
		Debug:       cfg.Debug,
	})
	if err != nil {
		return ChatResult{}, err
	}
	if cfg.OnResponse != nil {
		if err := cfg.OnResponse(resp); err != nil {
			return ChatResult{}, err
		}
	}

	// ---- Update session stats ----
	prevPromptTokens := stats.LastCallPromptTokens
	price, known := domain.PricingFor(cfg.Model)
	var callCost float64
	if known {
		callCost = float64(resp.Usage.PromptTokens)/1_000_000*price.InputPer1M +
			float64(resp.Usage.CompletionTokens)/1_000_000*price.OutputPer1M
	}
	stats.Calls++
	stats.PromptTokens += resp.Usage.PromptTokens
	stats.CompletionTokens += resp.Usage.CompletionTokens
	stats.TotalTokens += resp.Usage.TotalTokens
	stats.EstimatedCostUSD += callCost
	stats.LastCallPromptTokens = resp.Usage.PromptTokens

	// ---- Update memory layers (Layer 2 + 3) ----
	if cfg.MemoryUpdate {
		fmt.Fprintf(os.Stderr, "[memory-wm] updating working memory...\n")
		if updatedWM, err := UpdateWM(ctx, uc.llm, cfg.Model, wmData, cfg.FullQuery, resp.Content, cfg.Debug); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update working memory: %v\n", err)
		} else if err := uc.wm.Save(updatedWM); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save working memory: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "[memory-wm] %d facts saved\n", len(updatedWM.Facts))
		}

		fmt.Fprintf(os.Stderr, "[memory-ltm] updating long-term memory...\n")
		if updatedLTM, err := UpdateLTM(ctx, uc.llm, cfg.Model, ltmData, cfg.FullQuery, resp.Content, cfg.Debug); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update long-term memory: %v\n", err)
		} else if err := uc.ltm.Save(updatedLTM); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save long-term memory: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "[memory-ltm] %d entries saved\n", len(updatedLTM.Entries))
		}
	}

	// ---- Persist history (Layer 1) by strategy ----
	newHistory := append(history,
		domain.Message{Role: domain.RoleUser, Content: cfg.FullQuery},
		domain.Message{Role: domain.RoleAssistant, Content: resp.Content},
	)

	switch cfg.Strategy {
	case StrategySlidingWindow:
		if err := uc.history.Save(SlidingWindow(newHistory, cfg.WindowSize)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save history: %v\n", err)
		}

	case StrategyStickyFacts:
		fmt.Fprintf(os.Stderr, "[facts] updating facts store...\n")
		if updatedFacts, err := UpdateFacts(ctx, uc.llm, cfg.Model, factsData, cfg.FullQuery, resp.Content, cfg.Debug); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update facts: %v\n", err)
		} else if err := uc.facts.Save(updatedFacts); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save facts: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "[facts] %d facts saved\n", len(updatedFacts.Facts))
			if cfg.Debug {
				keys := make([]string, 0, len(updatedFacts.Facts))
				for k := range updatedFacts.Facts {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Fprintf(os.Stderr, "[facts]   %s: %s\n", k, updatedFacts.Facts[k])
				}
			}
		}
		if err := uc.history.Save(SlidingWindow(newHistory, cfg.WindowSize)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save history: %v\n", err)
		}

	default: // StrategyNone or StrategyBranching
		keep, excess := TrimHistory(newHistory, cfg.HistoryLimit)
		if cfg.SummaryEnabled && len(excess) > 0 {
			fmt.Fprintf(os.Stderr, "[summary] %d messages exceed limit — generating summary...\n", len(excess))
			if newSummary, err := BuildSummary(ctx, uc.llm, cfg.Model, excess, existingSummary, cfg.Debug); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to generate summary: %v\n", err)
			} else if err := uc.summary.Save(newSummary); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save summary: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "[summary] saved\n")
			}
		}
		if err := uc.history.Save(keep); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save history: %v\n", err)
		}
	}

	// ---- Persist stats ----
	if err := uc.stats.Save(stats); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save stats: %v\n", err)
	}

	return ChatResult{
		Content:          resp.Content,
		Usage:            resp.Usage,
		Stats:            stats,
		PrevPromptTokens: prevPromptTokens,
		GuardBlocked:     resp.Guard.Blocked(),
		GuardReason:      guardReason(resp),
		CostUSD:          callCost,
	}, nil
}

const maxToolCallRounds = 20

// maxRepeatedToolFailures caps how many times the same tool may fail the same way
// before the tool loop is abandoned. Observed failure mode: a sub-agent fed file
// paths from a different root kept calling read_text_file with paths the guard
// could never accept, burning every round on one dead end.
const maxRepeatedToolFailures = 4

// stuckDetector spots a tool loop that repeats one identical failure. It keys on
// the tool name plus the error's leading code so retries with a different path
// still count as the same dead end, while a genuinely new error resets it.
type stuckDetector struct {
	key   string
	count int
}

func (s *stuckDetector) record(tool string, err error) {
	k := tool + "|" + failureKey(err)
	if k == s.key {
		s.count++
		return
	}
	s.key, s.count = k, 1
}

func (s *stuckDetector) reset() { s.key, s.count = "", 0 }

func (s *stuckDetector) verdict() (string, bool) {
	if s.count < maxRepeatedToolFailures {
		return "", false
	}
	tool, _, _ := strings.Cut(s.key, "|")
	return fmt.Sprintf(
		"Прерываю: инструмент %s %d раза подряд вернул одну и ту же ошибку, продвижения нет. "+
			"Скорее всего аргументы систематически неверны (например, путь считается не от того корня). "+
			"Последняя ошибка: %s", tool, s.count, strings.SplitN(s.key, "|", 2)[1]), true
}

// sanitizeInvisibleQuery cleans a top-level query of zero-width/bidi
// characters and prepends a guard warning, so a hidden instruction cannot
// ride into the model as plain text after the proxy strips the carrier (A4).
func sanitizeInvisibleQuery(cfg ChatConfig) ChatConfig {
	if n := countInvisible(cfg.FullQuery); n > 0 {
		cfg.FullQuery = fmt.Sprintf("%s (%d шт.)\n\n%s", InvisibleWarning, n, stripInvisible(cfg.FullQuery))
	}
	return cfg
}

// failureKey reduces an error to its stable part: the machine-readable code when
// the guard produced one, otherwise a trimmed prefix of the message.
func failureKey(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ":"); i > 0 && i < 40 {
		return msg[:i]
	}
	if len(msg) > 40 {
		return msg[:40]
	}
	return msg
}

// ExecuteWithTools runs one full chat turn with MCP tool-calling support.
// The LLM receives the tool definitions in the request; when it produces tool_calls the
// executor callback is invoked and results are fed back until the LLM produces a final
// text reply.  Falls back to Execute when tools is empty or executor is nil.
func (uc *ChatUseCase) ExecuteWithTools(ctx context.Context, cfg ChatConfig, tools []domain.MCPTool, executor port.ToolExecutor) (ChatResult, error) {
	cfg = sanitizeInvisibleQuery(cfg)
	if len(tools) == 0 || executor == nil {
		return uc.Execute(ctx, cfg)
	}

	history, _ := uc.history.Load()
	stats, _ := uc.stats.Load()
	wmData, _ := uc.wm.Load()
	ltmData, _ := uc.ltm.Load()
	profileData, _ := uc.profile.Load()

	var factsData domain.FactsStore
	if cfg.Strategy == StrategyStickyFacts {
		factsData, _ = uc.facts.Load()
	}

	var existingSummary string
	if cfg.SummaryEnabled && (cfg.Strategy == StrategyNone || cfg.Strategy == StrategyBranching) {
		existingSummary, _ = uc.summary.Load()
	}

	// Build system content (same ordering as Execute)
	systemContent := cfg.SystemMessage
	if cfg.Strategy == StrategyStickyFacts {
		if block := FactsSystemBlock(factsData); block != "" {
			systemContent = prepend(block, systemContent)
		}
	}
	if block := WMSystemBlock(wmData); block != "" {
		systemContent = prepend(block, systemContent)
	}
	if block := LTMSystemBlock(ltmData); block != "" {
		systemContent = prepend(block, systemContent)
	}
	if block := ProfileSystemBlock(profileData); block != "" {
		systemContent = prepend(block, systemContent)
	}
	if existingSummary != "" {
		appendBlock := "Summary of earlier conversation:\n" + existingSummary
		if systemContent != "" {
			systemContent += "\n\n---\n" + appendBlock
		} else {
			systemContent = appendBlock
		}
	}

	// Build base messages (system + history window)
	historyForPrompt := history
	if cfg.Strategy == StrategySlidingWindow || cfg.Strategy == StrategyStickyFacts {
		historyForPrompt = SlidingWindow(history, cfg.WindowSize)
	}
	var baseMessages []domain.Message
	if systemContent != "" {
		baseMessages = append(baseMessages, domain.Message{Role: domain.RoleSystem, Content: systemContent})
	}
	baseMessages = append(baseMessages, historyForPrompt...)

	// turnMsgs accumulates all new messages for this turn (user + tool rounds + final assistant)
	turnMsgs := []domain.Message{
		{Role: domain.RoleUser, Content: cfg.FullQuery},
	}

	prevPromptTokens := stats.LastCallPromptTokens
	var totalUsage domain.Usage
	var finalContent string
	var stuck stuckDetector
	var result ChatResult

	maxRounds := maxToolCallRounds
	switch {
	case cfg.MaxToolRounds > 0:
		maxRounds = cfg.MaxToolRounds
	case cfg.MaxToolRounds < 0:
		maxRounds = int(^uint(0) >> 1)
	}
	finished := false

	for round := 0; round < maxRounds; round++ {
		resp, err := uc.llm.Chat(ctx, port.LLMRequest{
			Model:       cfg.Model,
			Messages:    append(baseMessages, turnMsgs...),
			MaxTokens:   cfg.MaxTokens,
			Stop:        cfg.Stop,
			Temperature: cfg.Temperature,
			Debug:       cfg.Debug,
			Tools:       tools,
		})
		if err != nil {
			return ChatResult{}, err
		}

		totalUsage.PromptTokens += resp.Usage.PromptTokens
		totalUsage.CompletionTokens += resp.Usage.CompletionTokens
		totalUsage.TotalTokens += resp.Usage.TotalTokens
		if cfg.OnResponse != nil {
			if err := cfg.OnResponse(resp); err != nil {
				return ChatResult{}, err
			}
		}

		// A blocked answer is the proxy's refusal, not the model's output.
		// Its tool calls (if any slipped through) are never executed.
		if resp.Guard.Blocked() {
			result.GuardBlocked = true
			result.GuardReason = guardReason(resp)
			finalContent = resp.Content
			turnMsgs = append(turnMsgs, domain.Message{Role: domain.RoleAssistant, Content: finalContent})
			finished = true
			break
		}

		if len(resp.ToolCalls) == 0 {
			// Final text response from the LLM
			finalContent = resp.Content
			turnMsgs = append(turnMsgs, domain.Message{Role: domain.RoleAssistant, Content: finalContent})
			finished = true
			break
		}

		// Build assistant message containing the requested tool calls
		assistantMsg := domain.Message{Role: domain.RoleAssistant}
		for _, tc := range resp.ToolCalls {
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, domain.ToolCallMsg{
				ID:   tc.ID,
				Type: "function",
				Function: domain.ToolCallFunction{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}
		turnMsgs = append(turnMsgs, assistantMsg)

		// Arguments rewritten by the guard are not the model's: refuse them all
		// and tell the model why, instead of executing a mutilated call.
		if resp.Guard.Output == port.GuardRedacted {
			for _, tc := range resp.ToolCalls {
				result.RedactedToolCalls++
				turnMsgs = append(turnMsgs, domain.Message{
					Role:       domain.RoleTool,
					Content:    RedactedToolCallResult,
					ToolCallID: tc.ID,
					Name:       tc.Name,
				})
			}
			continue
		}

		// Execute each tool call and append the result
		for _, tc := range resp.ToolCalls {
			result.ToolCalls++
			if cfg.Debug {
				fmt.Fprintf(os.Stderr, "[tool-call] %s(%s)\n", tc.Name, tc.Arguments)
			}
			result, err := executor(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
				stuck.record(tc.Name, err)
			} else {
				stuck.reset()
			}
			if cfg.Debug {
				fmt.Fprintf(os.Stderr, "[tool-result] %s: %s\n", tc.Name, result)
			}
			turnMsgs = append(turnMsgs, domain.Message{
				Role:       domain.RoleTool,
				Content:    result,
				ToolCallID: tc.ID,
				Name:       tc.Name,
			})
		}

		// A model that keeps hitting the identical failure is not making progress;
		// it will burn every remaining round on the same dead end. Stop and say so,
		// so the caller sees the real reason instead of an exhausted budget.
		if msg, halted := stuck.verdict(); halted {
			fmt.Fprintf(os.Stderr, "[tool-loop] %s\n", msg)
			turnMsgs = append(turnMsgs, domain.Message{Role: domain.RoleAssistant, Content: msg})
			finalContent = msg
			finished = true
			break
		}
	}

	if !finished {
		// The round cap is spent and the model never produced a final answer.
		// Say so instead of returning an empty reply: the caller must see the
		// real reason (a limit), not silence.
		msg := fmt.Sprintf("[tool-loop] достигнут лимит раундов tool calling (%d): модель не завершила ответ. Заверши ответ текстом без вызовов инструментов.", maxRounds)
		fmt.Fprintln(os.Stderr, msg)
		turnMsgs = append(turnMsgs, domain.Message{Role: domain.RoleAssistant, Content: msg})
		finalContent = msg
	}

	// Update session stats
	price, known := domain.PricingFor(cfg.Model)
	var callCost float64
	if known {
		callCost = float64(totalUsage.PromptTokens)/1_000_000*price.InputPer1M +
			float64(totalUsage.CompletionTokens)/1_000_000*price.OutputPer1M
	}
	stats.Calls++
	stats.PromptTokens += totalUsage.PromptTokens
	stats.CompletionTokens += totalUsage.CompletionTokens
	stats.TotalTokens += totalUsage.TotalTokens
	stats.EstimatedCostUSD += callCost
	stats.LastCallPromptTokens = totalUsage.PromptTokens

	// Persist history (all messages from this turn)
	newHistory := append(history, turnMsgs...)
	switch cfg.Strategy {
	case StrategySlidingWindow:
		_ = uc.history.Save(SlidingWindow(newHistory, cfg.WindowSize))
	case StrategyStickyFacts:
		if updatedFacts, err := UpdateFacts(ctx, uc.llm, cfg.Model, factsData, cfg.FullQuery, finalContent, cfg.Debug); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update facts: %v\n", err)
		} else if err := uc.facts.Save(updatedFacts); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save facts: %v\n", err)
		}
		_ = uc.history.Save(SlidingWindow(newHistory, cfg.WindowSize))
	default:
		keep, excess := TrimHistory(newHistory, cfg.HistoryLimit)
		if cfg.SummaryEnabled && len(excess) > 0 {
			fmt.Fprintf(os.Stderr, "[summary] %d messages exceed limit — generating summary...\n", len(excess))
			if newSummary, err := BuildSummary(ctx, uc.llm, cfg.Model, excess, existingSummary, cfg.Debug); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to generate summary: %v\n", err)
			} else if err := uc.summary.Save(newSummary); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save summary: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "[summary] saved\n")
			}
		}
		_ = uc.history.Save(keep)
	}

	_ = uc.stats.Save(stats)

	result.Content = finalContent
	result.Usage = totalUsage
	result.Stats = stats
	result.PrevPromptTokens = prevPromptTokens
	result.CostUSD = callCost
	return result, nil
}

// guardReason summarises why the proxy blocked an answer.
func guardReason(resp port.LLMResponse) string {
	if !resp.Guard.Blocked() {
		return ""
	}
	if r := strings.TrimSpace(resp.Guard.OutputFindings); r != "" {
		return r
	}
	if resp.FinishReason == "content_filter" {
		return "content_filter"
	}
	return "blocked"
}

func prepend(block, existing string) string {
	if existing != "" {
		return block + "\n\n" + existing
	}
	return block
}

// LLM returns the underlying LLMClient for use by sub-agents that need direct API access.
func (uc *ChatUseCase) LLM() port.LLMClient {
	return uc.llm
}
