// Package telegram — минимальный клиент Telegram Bot API (long polling).
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"weatherbot/internal/httpx"
)

// DefaultBaseURL — адрес Telegram Bot API.
const DefaultBaseURL = "https://api.telegram.org"

const maxResponseBytes = 10 << 20

// User — отправитель сообщения.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Chat — чат, в котором пришло сообщение.
type Chat struct {
	ID int64 `json:"id"`
}

// Message — входящее сообщение.
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

// Update — событие из getUpdates.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// APIError — ошибка, которую вернул Bot API (ok=false).
type APIError struct {
	Code        int
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram API error %d: %s", e.Code, e.Description)
}

// Client вызывает методы Bot API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient создаёт клиента. Пустой baseURL означает DefaultBaseURL.
// Таймаут httpClient должен быть больше таймаута long polling.
func NewClient(httpClient *http.Client, baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, httpClient: httpClient}
}

// GetUpdates получает новые события, ожидая до timeoutSec секунд.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	params := map[string]any{
		"offset":          offset,
		"timeout":         timeoutSec,
		"allowed_updates": []string{"message"},
	}
	var updates []Update
	if err := c.call(ctx, "getUpdates", params, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// SendMessage отправляет текстовое сообщение в чат.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	params := map[string]any{"chat_id": chatID, "text": text}
	return c.call(ctx, "sendMessage", params, nil)
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
}

func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal %s params: %w", method, err)
	}

	endpoint := fmt.Sprintf("%s/bot%s/%s", c.baseURL, c.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, httpx.RedactURLError(err))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", method, httpx.RedactURLError(err))
	}
	defer resp.Body.Close()

	var body apiResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&body); err != nil {
		return fmt.Errorf("decode %s response (status %d): %w", method, resp.StatusCode, err)
	}
	if !body.OK {
		return &APIError{Code: body.ErrorCode, Description: body.Description}
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(body.Result, result); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}
