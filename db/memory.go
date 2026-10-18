package db

import (
	"context"
	"sync"
	"time"
)

// Memory is an in-process store for local development and tests.
type Memory struct {
	mu       sync.Mutex
	story    *Story
	comments []storedComment // insertion order
	rates    map[string]*rateWindow
}

type rateWindow struct {
	count int
	until time.Time
}

func NewMemory() *Memory { return &Memory{rates: map[string]*rateWindow{}} }

func (m *Memory) Ping(context.Context) error { return nil }

func (m *Memory) SeedStory(_ context.Context, s Story) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.story == nil {
		m.story = &s
	}
	return nil
}

func (m *Memory) GetStory(context.Context) (Story, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.story == nil {
		return Story{}, ErrNoStory
	}
	return *m.story, nil
}

func (m *Memory) UpdateStory(_ context.Context, s Story) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.story = &s
	return nil
}

func (m *Memory) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.rates[key]
	if w == nil || time.Now().After(w.until) {
		w = &rateWindow{until: time.Now().Add(window)}
		m.rates[key] = w
	}
	w.count++
	return w.count <= limit, nil
}

func (m *Memory) ListComments(context.Context) ([]Comment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Comment{}
	for i := len(m.comments) - 1; i >= 0; i-- {
		if !m.comments[i].IsDeleted {
			out = append(out, m.comments[i].Comment)
		}
	}
	return out, nil
}

func (m *Memory) InsertComment(_ context.Context, c Comment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comments = append(m.comments, storedComment{Comment: c})
	return nil
}

func (m *Memory) SoftDeleteComment(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.comments {
		if m.comments[i].ID == id {
			m.comments[i].IsDeleted = true
			return nil
		}
	}
	return ErrNotFound
}
