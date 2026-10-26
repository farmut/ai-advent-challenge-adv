package telegram

import (
	"sync"
	"testing"
)

func TestLastCitiesGetSet(t *testing.T) {
	l := newLastCities(3)
	if _, ok := l.Get(1); ok {
		t.Fatal("Get(1) on empty store: ok = true")
	}
	l.Set(1, "Казань")
	l.Set(2, "Пермь")
	l.Set(1, "Москва") // обновление существующего
	for id, want := range map[int64]string{1: "Москва", 2: "Пермь"} {
		if got, ok := l.Get(id); !ok || got != want {
			t.Errorf("Get(%d) = (%q, %v), want (%q, true)", id, got, ok, want)
		}
	}
}

func TestLastCitiesEvictsOldest(t *testing.T) {
	l := newLastCities(2)
	l.Set(1, "Казань")
	l.Set(2, "Пермь")
	l.Set(1, "Москва") // обновление не сдвигает чат 1 в очереди
	l.Set(3, "Омск")   // вытесняет 1 — самый старый по вставке
	l.Set(4, "Тула")   // вытесняет 2
	want := map[int64]string{3: "Омск", 4: "Тула"}
	for _, id := range []int64{1, 2, 3, 4} {
		got, ok := l.Get(id)
		if w, keep := want[id]; got != w || ok != keep {
			t.Errorf("Get(%d) = (%q, %v), want (%q, %v)", id, got, ok, w, keep)
		}
	}
	if len(l.cities) != 2 {
		t.Errorf("stored chats = %d, want 2", len(l.cities))
	}
}

func TestLastCitiesZeroCapacity(t *testing.T) {
	l := newLastCities(0)
	l.Set(1, "Казань") // не должно паниковать
	if _, ok := l.Get(1); ok {
		t.Error("Get(1) with zero capacity: ok = true")
	}
}

func TestLastCitiesConcurrent(t *testing.T) {
	const capacity = 8
	l := newLastCities(capacity)
	var wg sync.WaitGroup
	for g := range int64(16) {
		wg.Go(func() {
			for i := range int64(100) {
				l.Set(g*100+i, "Казань")
				l.Get(g*100 + i)
			}
		})
	}
	wg.Wait()
	if len(l.cities) != capacity {
		t.Errorf("stored chats = %d, want %d", len(l.cities), capacity)
	}
}
