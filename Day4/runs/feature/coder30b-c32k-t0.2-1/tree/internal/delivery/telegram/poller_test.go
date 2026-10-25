package telegram_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"weatherbot/internal/delivery/telegram"
)

type mockClient struct {
	mock.Mock
}

func (m *mockClient) GetUpdates(ctx context.Context, offset int64) ([]telegram.Update, error) {
	args := m.Called(ctx, offset)
	return args.Get(0).([]telegram.Update), args.Error(1)
}

func (m *mockClient) Send(ctx context.Context, chatID int64, text string) error {
	args := m.Called(ctx, chatID, text)
	return args.Error(0)
}

func TestPoller_Run(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := new(mockClient)
		handler := new(mockHandler)

		// Подготавливаем данные для теста
		update := telegram.Update{
			ID: 123,
			Message: struct {
				ID      int64  `json:"message_id"`
				Chat    struct {
					ID int64 `json:"id"`
				} `json:"chat"`
				Text string `json:"text"`
			}{
				ID:   123,
				Chat: struct {
					ID int64 `json:"id"`
				}{ID: 123456789},
				Text: "Привет!",
			},
		}

		client.On("GetUpdates", mock.Anything, int64(0)).Return([]telegram.Update{update}, nil)
		handler.On("Handle", mock.Anything, int64(123456789), "Привет!").Return()

		poller := telegram.NewPoller(client, handler, nil)
		
		// Запускаем poller на короткое время
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()
		
		err := poller.Run(ctx)
		assert.NoError(t, err)
		client.AssertExpectations(t)
		handler.AssertExpectations(t)
	})

	t.Run("rate limited", func(t *testing.T) {
		client := new(mockClient)
		handler := new(mockHandler)

		// Подготавливаем данные для теста
		update := telegram.Update{
			ID: 123,
			Message: struct {
				ID      int64  `json:"message_id"`
				Chat    struct {
					ID int64 `json:"id"`
				} `json:"chat"`
				Text string `json:"text"`
			}{
				ID:   123,
				Chat: struct {
					ID int64 `json:"id"`
				}{ID: 123456789},
				Text: "Привет!",
			},
		}

		client.On("GetUpdates", mock.Anything, int64(0)).Return([]telegram.Update{update}, nil)
		handler.On("Handle", mock.Anything, int64(123456789), "Привет!").Return()

		poller := telegram.NewPoller(client, handler, nil)
		
		// Запускаем poller на короткое время
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()
		
		err := poller.Run(ctx)
		assert.NoError(t, err)
		client.AssertExpectations(t)
		handler.AssertExpectations(t)
	})
}