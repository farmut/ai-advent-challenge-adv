package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Update — входящее обновление Telegram.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// Message — сообщение из чата.
type Message struct {
	Chat Chat   `json:"chat"`
	Text string `json:"text"`
}

// Chat — чат, из которого пришло сообщение.
type Chat struct {
	ID int64 `json:"id"`
}

// Client — HTTP-клиент Telegram Bot API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient создаёт клиент Bot API. httpClient обязан иметь Timeout больше long-poll timeout.
func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, token: token, httpClient: httpClient}
}

// GetUpdates возвращает новые обновления начиная с offset.
func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}
	u = u.JoinPath("bot"+c.token, "getUpdates")
	q := url.Values{}
	q.Set("offset", fmt.Sprintf("%d", offset))
	q.Set("timeout", "30")
	q.Set("allowed_updates", `["message"]`)
	u.RawQuery = q.Encode()

	var updates []Update
	if err := c.doJSON(ctx, u.String(), &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// Send отправляет текст в чат (plain text, без parse_mode).
func (c *Client) Send(ctx context.Context, chatID int64, text string) error {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("parse base url: %w", err)
	}
	u = u.JoinPath("bot"+c.token, "sendMessage")
	q := url.Values{}
	q.Set("chat_id", fmt.Sprintf("%d", chatID))
	q.Set("text", text)
	u.RawQuery = q.Encode()

	return c.doJSON(ctx, u.String(), nil)
}

func (c *Client) doJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return c.redact(fmt.Errorf("build request: %w", err))
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.redact(fmt.Errorf("telegram request: %w", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return c.redact(fmt.Errorf("read telegram response: %w", err))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return c.redact(&rateLimitError{body: body})
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.redact(fmt.Errorf("telegram status %d: %s", resp.StatusCode, truncate(string(body), 256)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return c.redact(fmt.Errorf("decode telegram response: %w", err))
	}
	return nil
}

// redact убирает токен из ошибки: *url.Error содержит полный URL с токеном в пути.
func (c *Client) redact(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), c.token, "[REDACTED]"))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
