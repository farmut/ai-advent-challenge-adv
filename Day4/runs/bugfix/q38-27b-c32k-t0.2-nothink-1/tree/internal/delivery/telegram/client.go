// Package telegram — delivery-слой: long polling Telegram Bot API и обработка команд.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxBodyBytes    = 1 << 20 // 1 MiB
	longPollTimeout = 30      // секунд; http.Client.Timeout должен быть больше
)

// ErrUnauthorized — Telegram отверг токен бота; ретраи бессмысленны.
var ErrUnauthorized = errors.New("telegram bot token rejected")

// Update — входящее обновление Bot API (нужны только сообщения).
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// Message — входящее сообщение.
type Message struct {
	Chat Chat   `json:"chat"`
	Text string `json:"text"`
}

// Chat — чат, из которого пришло сообщение.
type Chat struct {
	ID int64 `json:"id"`
}

// retryAfterError — Telegram попросил подождать (HTTP 429).
type retryAfterError struct {
	after time.Duration
}

func (e *retryAfterError) Error() string {
	return "telegram rate limited, retry after " + e.after.String()
}

// Client — минимальный клиент Telegram Bot API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

type sendMessageRequest struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

// NewClient создаёт клиент. Timeout у httpClient должен превышать long-poll timeout (30 с).
func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, token: token, httpClient: httpClient}
}

// GetUpdates забирает новые сообщения через long polling.
func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	q := url.Values{}
	q.Set("offset", strconv.FormatInt(offset, 10))
	q.Set("timeout", strconv.Itoa(longPollTimeout))
	q.Set("allowed_updates", `["message"]`)
	var updates []Update
	if err := c.call(ctx, http.MethodGet, "getUpdates", q, nil, &updates); err != nil {
		return nil, fmt.Errorf("get updates: %w", err)
	}
	return updates, nil
}

// Send отправляет plain-text сообщение (без parse_mode). Ошибка уже очищена от токена.
func (c *Client) Send(ctx context.Context, chatID int64, text string) error {
	body, err := json.Marshal(sendMessageRequest{ChatID: chatID, Text: text})
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	if err := c.call(ctx, http.MethodPost, "sendMessage", nil, body, nil); err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return nil
}

func (c *Client) call(ctx context.Context, method, apiMethod string, q url.Values, body []byte, result any) error {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("parse base url: %w", err)
	}
	u = u.JoinPath("bot"+c.token, apiMethod) // формат токена проверен в config
	u.RawQuery = q.Encode()

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return fmt.Errorf("build request: %s", c.redact(err.Error()))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %s", c.redact(err.Error())) // *url.Error содержит токен в пути
	}
	defer resp.Body.Close()

	var r apiResponse
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		_ = json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&r)
		return &retryAfterError{after: time.Duration(r.Parameters.RetryAfter) * time.Second}
	case http.StatusUnauthorized, http.StatusNotFound:
		return fmt.Errorf("%w: status %d", ErrUnauthorized, resp.StatusCode)
	default:
		_ = json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&r)
		return fmt.Errorf("status %d: %s", resp.StatusCode, c.redact(r.Description))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&r); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if !r.OK {
		return fmt.Errorf("api error: %s", c.redact(r.Description))
	}
	if result != nil {
		if err := json.Unmarshal(r.Result, result); err != nil {
			return fmt.Errorf("decode result: %w", err)
		}
	}
	return nil
}

// redact убирает токен (в сыром и экранированном виде) из текста ошибки.
func (c *Client) redact(s string) string {
	if c.token == "" {
		return s
	}
	for _, v := range []string{c.token, url.PathEscape(c.token), url.QueryEscape(c.token)} {
		s = strings.ReplaceAll(s, v, "[REDACTED]")
	}
	return s
}
