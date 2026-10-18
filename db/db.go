// Package db is the persistence layer. Production uses Upstash Redis (over
// its REST API, so it works from Vercel functions with no disk); local
// development and tests can use an in-memory store.
package db

import (
	"context"
	"errors"
	"log"
	"os"
	"time"
)

// Story is the narrative shown on the page.
type Story struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Comment is a reader's note left under the story.
type Comment struct {
	ID         string    `json:"id"`
	AuthorName string    `json:"author_name"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

var (
	// ErrNoStory is returned when the story has not been seeded.
	ErrNoStory = errors.New("story not found")
	// ErrNotFound is returned when a comment id does not exist.
	ErrNotFound = errors.New("not found")
)

// Store is everything the app needs from storage.
type Store interface {
	Ping(ctx context.Context) error
	// SeedStory stores s only if no story exists yet, so author edits survive redeploys.
	SeedStory(ctx context.Context, s Story) error
	GetStory(ctx context.Context) (Story, error)
	// UpdateStory replaces the story (the author's live edits).
	UpdateStory(ctx context.Context, s Story) error
	// ListComments returns visible (not deleted) comments, newest first.
	ListComments(ctx context.Context) ([]Comment, error)
	InsertComment(ctx context.Context, c Comment) error
	// SoftDeleteComment hides a comment but keeps its record.
	SoftDeleteComment(ctx context.Context, id string) error
	// Allow counts one hit against key and reports whether it is within
	// limit hits per window. Windows are fixed and start at the first hit.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// FromEnv picks the store from environment variables:
//
//   - UPSTASH_REDIS_REST_URL + UPSTASH_REDIS_REST_TOKEN → Upstash Redis.
//     (KV_REST_API_URL / KV_REST_API_TOKEN, the names Vercel's Upstash
//     integration injects, are accepted too.)
//   - Neither, running locally → in-memory store; data is lost on restart.
//   - Neither, running on Vercel → error. An in-memory store there would
//     silently lose readers' comments, so refuse to start instead.
func FromEnv() (Store, error) {
	url := firstEnv("UPSTASH_REDIS_REST_URL", "KV_REST_API_URL")
	token := firstEnv("UPSTASH_REDIS_REST_TOKEN", "KV_REST_API_TOKEN")
	switch {
	case url != "" && token != "":
		return NewRedis(url, token), nil
	case url != "" || token != "":
		return nil, errors.New("set both UPSTASH_REDIS_REST_URL and UPSTASH_REDIS_REST_TOKEN")
	case os.Getenv("VERCEL") != "":
		return nil, errors.New("UPSTASH_REDIS_REST_URL and UPSTASH_REDIS_REST_TOKEN are not set; add them in Vercel → Settings → Environment Variables")
	default:
		log.Print("db: no Upstash credentials found, using an in-memory store (comments are lost on restart)")
		return NewMemory(), nil
	}
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// storedComment is the persisted form; IsDeleted never leaves the store.
type storedComment struct {
	Comment
	IsDeleted bool `json:"is_deleted"`
}
