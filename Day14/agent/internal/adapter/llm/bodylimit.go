package llm

import (
	"encoding/json"
	"fmt"
	"net/http"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// msgUnit is a run of messages that must be kept or dropped together: an
// assistant message carrying tool_calls plus every tool answer that follows
// it. Dropping only half of such a pair produces a transcript the OpenAI API
// (and the guard proxy) rejects with 400, so trimming works on units.
type msgUnit struct{ start, end int } // messages[start:end]

// groupUnits splits messages into units. A tool message with no preceding
// assistant tool_calls message forms its own unit (it is already orphaned;
// keeping it alone does not make things worse).
func groupUnits(messages []domain.Message) []msgUnit {
	var units []msgUnit
	for i := 0; i < len(messages); {
		j := i + 1
		if messages[i].Role == domain.RoleAssistant && len(messages[i].ToolCalls) > 0 {
			for j < len(messages) && messages[j].Role == domain.RoleTool {
				j++
			}
		}
		units = append(units, msgUnit{i, j})
		i = j
	}
	return units
}

// messageSize is the JSON size a message contributes to the request body,
// including the separating comma.
func messageSize(m domain.Message) int {
	b, err := json.Marshal(m)
	if err != nil {
		return len(m.Content) + 64
	}
	return len(b) + 1
}

func messagesSize(ms []domain.Message) int {
	n := 2 // [ ]
	for _, m := range ms {
		n += messageSize(m)
	}
	return n
}

// truncationStub replaces a cut message body.
func truncationStub(n int) string {
	return fmt.Sprintf("[…обрезано %d байт…]", n)
}

// fitBody shrinks messages so their JSON encoding fits in budget bytes,
// changing as little as possible and in this order:
//
//  1. the oldest tool results are replaced by a truncation stub;
//  2. then the oldest large user messages;
//  3. then whole units (see msgUnit) are dropped, oldest first.
//
// System messages and the last message are never touched, and an assistant
// tool_calls message is only ever removed together with all its tool answers.
// The input slice is not modified. When nothing more can be removed and the
// body still does not fit, the request is refused with a non-retryable
// LLMError (code request_too_large_client) instead of being sent to fail with
// 413 at the proxy.
func fitBody(messages []domain.Message, budget int) ([]domain.Message, error) {
	size := messagesSize(messages)
	if size <= budget {
		return messages, nil
	}
	out := append([]domain.Message(nil), messages...)
	last := len(out) - 1

	// 1-2. Truncate content in place, oldest first: tool results, then users.
	for _, role := range []domain.Role{domain.RoleTool, domain.RoleUser} {
		for i := 0; i < last && size > budget; i++ {
			if out[i].Role != role {
				continue
			}
			stub := truncationStub(len(out[i].Content))
			if len(out[i].Content) <= len(stub) {
				continue
			}
			before := messageSize(out[i])
			out[i].Content = stub
			size += messageSize(out[i]) - before
		}
	}

	// 3. Drop whole units, oldest first, never a system message and never the
	// unit holding the last message.
	for size > budget {
		dropped := false
		for _, u := range groupUnits(out) {
			if u.end-1 == len(out)-1 || out[u.start].Role == domain.RoleSystem {
				continue
			}
			for _, m := range out[u.start:u.end] {
				size -= messageSize(m)
			}
			out = append(out[:u.start:u.start], out[u.end:]...)
			dropped = true
			break
		}
		if !dropped {
			return nil, &port.LLMError{
				Status: http.StatusRequestEntityTooLarge,
				Code:   port.CodeRequestTooLargeClient,
				Type:   "invalid_request_error",
				Message: fmt.Sprintf("запрос не помещается в llm.request_max_bytes даже после обрезки истории: сообщения %d байт при бюджете %d",
					size, budget),
			}
		}
	}
	return out, nil
}
