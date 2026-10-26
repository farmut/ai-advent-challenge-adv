package telegram

import (
	"sync"
	"time"
)

const perChatInterval = 2 * time.Second

// chatLimiter — per-chat token bucket ёмкостью 1: не больше одного запроса за interval на чат.
// Карта ограничена maxChats: при переполнении из неё выкидываются устаревшие записи.
type chatLimiter struct {
	interval time.Duration
	maxChats int
	now      func() time.Time // внедряемые часы для тестов
	mu       sync.Mutex
	last     map[int64]time.Time // время последнего разрешённого запроса чата
}

// newChatLimiter создаёт лимитер. now == nil означает time.Now.
func newChatLimiter(interval time.Duration, maxChats int, now func() time.Time) *chatLimiter {
	if now == nil {
		now = time.Now
	}
	return &chatLimiter{interval: interval, maxChats: maxChats, now: now, last: make(map[int64]time.Time)}
}

// Allow сообщает, можно ли чату сделать запрос сейчас, и при успехе тратит токен.
func (l *chatLimiter) Allow(chatID int64) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if last, ok := l.last[chatID]; ok && now.Sub(last) < l.interval {
		return false
	}
	if len(l.last) >= l.maxChats {
		for id, last := range l.last {
			if now.Sub(last) >= l.interval {
				delete(l.last, id)
			}
		}
	}
	l.last[chatID] = now
	return true
}
