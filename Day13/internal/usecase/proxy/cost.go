package proxy

import (
	"fmt"
	"unicode/utf8"

	"agile-consultant/internal/ports"
)

// estimateTokens is a coarse estimate: ceil(runes/4).
func estimateTokens(s string) int {
	n := utf8.RuneCountInString(s)
	return (n + 3) / 4
}

func estimateMessages(ms []ports.ChatMessage) int {
	t := 0
	for _, m := range ms {
		t += estimateTokens(m.Content) + estimateToolCalls(m.ToolCalls)
	}
	return t
}

// cost returns ceil((pt*in + ct*out) / 1e6) micro-USD.
func cost(u ports.Usage, p ports.Price) int64 {
	num := int64(u.PromptTokens)*p.InputMicroUSDPer1M + int64(u.CompletionTokens)*p.OutputMicroUSDPer1M
	if num <= 0 {
		return 0
	}
	return (num + 999_999) / 1_000_000
}

// FormatUSD renders micro-USD as dollars with six decimals.
func FormatUSD(micro int64) string {
	sign := ""
	if micro < 0 {
		sign, micro = "-", -micro
	}
	return fmt.Sprintf("%s%d.%06d", sign, micro/1_000_000, micro%1_000_000)
}
