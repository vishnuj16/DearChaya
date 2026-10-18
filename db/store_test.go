package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Every Store implementation must pass the same behaviour checks.
func TestMemoryStore(t *testing.T) { testStore(t, func(*testing.T) Store { return NewMemory() }) }

func TestRedisStore(t *testing.T) {
	testStore(t, func(t *testing.T) Store {
		srv := httptest.NewServer(newFakeUpstash("secret"))
		t.Cleanup(srv.Close)
		return NewRedis(srv.URL+"/", "secret") // trailing slash must be tolerated
	})
}

func testStore(t *testing.T, newStore func(*testing.T) Store) {
	ctx := context.Background()

	t.Run("ping", func(t *testing.T) {
		if err := newStore(t).Ping(ctx); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("story is seeded once", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.GetStory(ctx); !errors.Is(err, ErrNoStory) {
			t.Fatalf("empty store: err = %v, want ErrNoStory", err)
		}
		first := Story{Title: "Dear Chaya", Content: "Dear Diary,\n\n“quotes” & <tags>"}
		if err := s.SeedStory(ctx, first); err != nil {
			t.Fatal(err)
		}
		if err := s.SeedStory(ctx, Story{Title: "other", Content: "other"}); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetStory(ctx)
		if err != nil || got != first {
			t.Fatalf("got %+v, %v; want the first seed %+v", got, err, first)
		}
	})

	t.Run("comments list newest first and round-trip exactly", func(t *testing.T) {
		s := newStore(t)
		if cs, err := s.ListComments(ctx); err != nil || cs == nil || len(cs) != 0 {
			t.Fatalf("empty list = %#v, %v; want non-nil empty slice", cs, err)
		}
		at := time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)
		var want []Comment
		for i := range 5 { // same timestamp on purpose: order must follow insertion
			c := Comment{ID: fmt.Sprintf("id-%d", i), AuthorName: "Amma", Content: fmt.Sprintf("note %d\nline two", i), CreatedAt: at}
			if err := s.InsertComment(ctx, c); err != nil {
				t.Fatal(err)
			}
			want = append([]Comment{c}, want...)
		}
		got, err := s.ListComments(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("got %d comments, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i].ID != want[i].ID || got[i].Content != want[i].Content || !got[i].CreatedAt.Equal(want[i].CreatedAt) {
				t.Errorf("[%d] = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("soft-deleted comments are hidden", func(t *testing.T) {
		s := newStore(t)
		for _, id := range []string{"keep", "drop"} {
			if err := s.InsertComment(ctx, Comment{ID: id, AuthorName: "x", Content: id, CreatedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.SoftDeleteComment(ctx, "drop"); err != nil {
			t.Fatal(err)
		}
		if err := s.SoftDeleteComment(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Errorf("deleting a missing id: err = %v, want ErrNotFound", err)
		}
		got, _ := s.ListComments(ctx)
		if len(got) != 1 || got[0].ID != "keep" {
			t.Errorf("got %+v, want only 'keep'", got)
		}
	})

	t.Run("story can be edited after seeding", func(t *testing.T) {
		s := newStore(t)
		s.SeedStory(ctx, Story{Title: "Dear Chaya", Content: "v1"})
		edited := Story{Title: "Dear Chaya", Content: "v2\n\n> callout"}
		if err := s.UpdateStory(ctx, edited); err != nil {
			t.Fatal(err)
		}
		s.SeedStory(ctx, Story{Title: "seed", Content: "must not overwrite the edit"})
		if got, _ := s.GetStory(ctx); got != edited {
			t.Errorf("got %+v, want %+v", got, edited)
		}
	})

	t.Run("rate limit allows up to the limit per key", func(t *testing.T) {
		s := newStore(t)
		for i := 1; i <= 4; i++ {
			ok, err := s.Allow(ctx, "login:1.2.3.4", 3, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if want := i <= 3; ok != want {
				t.Errorf("hit %d: allowed = %v, want %v", i, ok, want)
			}
		}
		if ok, _ := s.Allow(ctx, "login:5.6.7.8", 3, time.Minute); !ok {
			t.Error("a different key must have its own budget")
		}
	})

	t.Run("concurrent inserts are all kept", func(t *testing.T) {
		s := newStore(t)
		var wg sync.WaitGroup
		for i := range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.InsertComment(ctx, Comment{ID: strconv.Itoa(i), AuthorName: "x", Content: "c", CreatedAt: time.Now()})
			}()
		}
		wg.Wait()
		if got, _ := s.ListComments(ctx); len(got) != 20 {
			t.Errorf("kept %d of 20 concurrent comments", len(got))
		}
	})
}

func TestRedisRejectsBadToken(t *testing.T) {
	srv := httptest.NewServer(newFakeUpstash("secret"))
	defer srv.Close()
	err := NewRedis(srv.URL, "wrong").Ping(context.Background())
	if err == nil {
		t.Fatal("want an error for a bad token")
	}
}

func TestFromEnv(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string // "redis", "memory" or "error"
	}{
		{"upstash vars", map[string]string{"UPSTASH_REDIS_REST_URL": "https://x", "UPSTASH_REDIS_REST_TOKEN": "t"}, "redis"},
		{"vercel integration vars", map[string]string{"KV_REST_API_URL": "https://x", "KV_REST_API_TOKEN": "t"}, "redis"},
		{"only url", map[string]string{"UPSTASH_REDIS_REST_URL": "https://x"}, "error"},
		{"nothing, locally", map[string]string{}, "memory"},
		{"nothing, on vercel", map[string]string{"VERCEL": "1"}, "error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"UPSTASH_REDIS_REST_URL", "UPSTASH_REDIS_REST_TOKEN", "KV_REST_API_URL", "KV_REST_API_TOKEN", "VERCEL"} {
				t.Setenv(k, tc.env[k])
			}
			s, err := FromEnv()
			got := "error"
			switch s.(type) {
			case *Redis:
				got = "redis"
			case *Memory:
				got = "memory"
			}
			if got != tc.want || (got == "error") != (err != nil) {
				t.Errorf("got %s (err %v), want %s", got, err, tc.want)
			}
		})
	}
}

// fakeUpstash implements the slice of Upstash's REST API this package uses:
// POST / with a JSON command array, and POST /multi-exec with an array of them.
type fakeUpstash struct {
	token string
	mu    sync.Mutex
	strs  map[string]string
	zsets map[string]map[string]float64
}

func newFakeUpstash(token string) *fakeUpstash {
	return &fakeUpstash{token: token, strs: map[string]string{}, zsets: map[string]map[string]float64{}}
}

func (f *fakeUpstash) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+f.token {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}
	f.mu.Lock() // one lock per request: a multi-exec batch is atomic
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/":
		var cmd []string
		json.NewDecoder(r.Body).Decode(&cmd)
		json.NewEncoder(w).Encode(f.exec(cmd))
	case "/multi-exec":
		var cmds [][]string
		json.NewDecoder(r.Body).Decode(&cmds)
		out := make([]map[string]any, len(cmds))
		for i, c := range cmds {
			out[i] = f.exec(c)
		}
		json.NewEncoder(w).Encode(out)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeUpstash) exec(c []string) map[string]any {
	ok := func(v any) map[string]any { return map[string]any{"result": v} }
	switch c[0] {
	case "PING":
		return ok("PONG")
	case "SET":
		if slices.Contains(c[3:], "NX") {
			if _, exists := f.strs[c[1]]; exists {
				return ok(nil)
			}
		}
		f.strs[c[1]] = c[2]
		return ok("OK")
	case "GET":
		if v, exists := f.strs[c[1]]; exists {
			return ok(v)
		}
		return ok(nil)
	case "MGET":
		vals := make([]any, len(c)-1)
		for i, k := range c[1:] {
			if v, exists := f.strs[k]; exists {
				vals[i] = v
			}
		}
		return ok(vals)
	case "INCR":
		n, _ := strconv.Atoi(f.strs[c[1]])
		n++
		f.strs[c[1]] = strconv.Itoa(n)
		return ok(n)
	case "ZADD":
		score, _ := strconv.ParseFloat(c[2], 64)
		if f.zsets[c[1]] == nil {
			f.zsets[c[1]] = map[string]float64{}
		}
		f.zsets[c[1]][c[3]] = score
		return ok(1)
	case "ZREM":
		delete(f.zsets[c[1]], c[2])
		return ok(1)
	case "ZREVRANGE": // only "0 -1" is used
		z := f.zsets[c[1]]
		members := make([]string, 0, len(z))
		for m := range z {
			members = append(members, m)
		}
		sort.Slice(members, func(i, j int) bool {
			if z[members[i]] != z[members[j]] {
				return z[members[i]] > z[members[j]]
			}
			return members[i] > members[j]
		})
		return ok(members)
	}
	return map[string]any{"error": "ERR unknown command " + c[0]}
}
