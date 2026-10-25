// Package telegram — delivery-слой: взаимодействие с Telegram Bot API.
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"weatherbot/internal/domain"
)

const (
	maxBodyBytes = 1 << 20 // 1 MiB
	defaultTimeout = 40 * time.Second
)

// Client взаимодействует с Telegram Bot API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient создаёт клиента для работы с Telegram Bot API.
func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{baseURL: baseURL, token: token, httpClient: httpClient}
}

// Update представляет собой одно обновление от Telegram.
type Update struct {
	ID      int64 `json:"update_id"`
	Message struct {
		ID      int64  `json:"message_id"`
		Chat    struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

// SendMessage отправляет текст в чат.
func (c *Client) Send(ctx context.Context, chatID int64, text string) error {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("parse base url: %w", err)
	}
	u = u.JoinPath("bot" + c.token, "sendMessage")

	q := url.Values{}
	q.Set("chat_id", fmt.Sprintf("%d", chatID))
	q.Set("text", text)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("send message: status %d", resp.StatusCode)
	}

	return nil
}

// GetUpdates получает обновления от Telegram.
func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}
	u = u.JoinPath("bot" + c.token, "getUpdates")

	q := url.Values{}
	q.Set("offset", fmt.Sprintf("%d", offset+1))
	q.Set("timeout", "30")
	q.Set("allowed_updates", `["message"]`)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		// Обработка rate limiting от Telegram
		var response struct {
			Parameters struct {
				RetryAfter int `json:"retry_after"`
			} `json:"parameters"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&response); err != nil {
			return nil, fmt.Errorf("decode rate limit response: %w", err)
		}
		if response.Parameters.RetryAfter > 0 {
			return nil, fmt.Errorf("rate limited: retry after %d seconds", response.Parameters.RetryAfter)
		}
		return nil, domain.ErrProviderUnavailable
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get updates: status %d", resp.StatusCode)
	}

	var response struct {
		Result []Update `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode updates response: %w", err)
	}

	return response.Result, nil
}