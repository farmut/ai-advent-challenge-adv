package telegram

import "sync"

// lastCities помнит последний успешный город каждого чата. Ёмкость ограничена:
// при переполнении вытесняется чат, добавленный раньше всех (FIFO).
type lastCities struct {
	mu     sync.Mutex
	cities map[int64]string
	order  []int64 // кольцо chatID в порядке вставки
	next   int     // позиция в order для следующей вставки
}

func newLastCities(capacity int) *lastCities {
	return &lastCities{cities: make(map[int64]string, capacity), order: make([]int64, 0, capacity)}
}

// Get возвращает последний город чата.
func (l *lastCities) Get(chatID int64) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	city, ok := l.cities[chatID]
	return city, ok
}

// Set запоминает город чата. Уже известный чат обновляется на месте и не сдвигается в очереди.
func (l *lastCities) Set(chatID int64, city string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.cities[chatID]; ok {
		l.cities[chatID] = city
		return
	}
	switch {
	case cap(l.order) == 0:
		return
	case len(l.order) < cap(l.order):
		l.order = append(l.order, chatID)
	default:
		delete(l.cities, l.order[l.next])
		l.order[l.next] = chatID
		l.next = (l.next + 1) % len(l.order)
	}
	l.cities[chatID] = city
}
