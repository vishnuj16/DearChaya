package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Keys are namespaced so this site can share an Upstash database with
// other projects without collisions.
const (
	keyPrefix       = "dearchaya:"
	keyStory        = keyPrefix + "story"    // JSON Story
	keyCommentIndex = keyPrefix + "comments" // sorted set: visible comment ids, scored by insert time
)

func keyComment(id string) string { return keyPrefix + "comment:" + id } // JSON storedComment
func keyRate(key string) string   { return keyPrefix + "rate:" + key }   // counter with TTL

// Redis talks to Upstash over its REST API using only the standard library.
// Each comment is its own key plus an entry in a sorted-set index, written in
// one MULTI/EXEC transaction, so concurrent posts can never overwrite each other.
type Redis struct {
	url    string
	token  string
	client *http.Client
}

func NewRedis(url, token string) *Redis {
	return &Redis{
		url:    strings.TrimRight(url, "/"),
		token:  token,
		client: &http.Client{Timeout: 8 * time.Second},
	}
}

// upstashResult is Upstash's reply shape: {"result": ...} or {"error": "..."}.
type upstashResult struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func (r *Redis) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url+path, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("upstash: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("upstash: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("upstash: read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("upstash: unauthorized, check UPSTASH_REDIS_REST_TOKEN")
	}
	if resp.StatusCode != http.StatusOK {
		var e upstashResult
		json.Unmarshal(data, &e)
		return fmt.Errorf("upstash: %s: %s", resp.Status, e.Error)
	}
	return json.Unmarshal(data, out)
}

// do runs one command, e.g. do(ctx, "GET", "key").
func (r *Redis) do(ctx context.Context, args ...string) (json.RawMessage, error) {
	var res upstashResult
	if err := r.post(ctx, "", args, &res); err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, fmt.Errorf("upstash %s: %s", args[0], res.Error)
	}
	return res.Result, nil
}

// tx runs commands atomically (MULTI/EXEC) and returns each result.
func (r *Redis) tx(ctx context.Context, cmds ...[]string) ([]json.RawMessage, error) {
	var res []upstashResult
	if err := r.post(ctx, "/multi-exec", cmds, &res); err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, len(res))
	for i, x := range res {
		if x.Error != "" {
			return nil, fmt.Errorf("upstash transaction %s: %s", cmds[i][0], x.Error)
		}
		out[i] = x.Result
	}
	return out, nil
}

func (r *Redis) Ping(ctx context.Context) error {
	_, err := r.do(ctx, "PING")
	return err
}

func (r *Redis) SeedStory(ctx context.Context, s Story) error {
	b, _ := json.Marshal(s)
	_, err := r.do(ctx, "SET", keyStory, string(b), "NX")
	return err
}

func (r *Redis) GetStory(ctx context.Context) (Story, error) {
	var s Story
	raw, err := r.do(ctx, "GET", keyStory)
	if err != nil {
		return s, err
	}
	val, ok, err := decodeString(raw)
	if err != nil || !ok {
		return s, errors.Join(ErrNoStory, err)
	}
	return s, json.Unmarshal([]byte(val), &s)
}

// lastScore keeps sort scores strictly increasing within this instance, so
// two posts in the same microsecond still list in the order they arrived.
var lastScore atomic.Int64

func nextScore() int64 {
	for {
		now, last := time.Now().UnixMicro(), lastScore.Load()
		next := max(now, last+1)
		if lastScore.CompareAndSwap(last, next) {
			return next
		}
	}
}

func (r *Redis) UpdateStory(ctx context.Context, s Story) error {
	b, _ := json.Marshal(s)
	_, err := r.do(ctx, "SET", keyStory, string(b))
	return err
}

func (r *Redis) InsertComment(ctx context.Context, c Comment) error {
	b, _ := json.Marshal(storedComment{Comment: c})
	_, err := r.tx(ctx,
		[]string{"SET", keyComment(c.ID), string(b)},
		[]string{"ZADD", keyCommentIndex, strconv.FormatInt(nextScore(), 10), c.ID},
	)
	return err
}

func (r *Redis) ListComments(ctx context.Context) ([]Comment, error) {
	raw, err := r.do(ctx, "ZREVRANGE", keyCommentIndex, "0", "-1")
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, fmt.Errorf("upstash: decode comment ids: %w", err)
	}
	comments := []Comment{}
	if len(ids) == 0 {
		return comments, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = keyComment(id)
	}
	raw, err = r.do(ctx, append([]string{"MGET"}, keys...)...)
	if err != nil {
		return nil, err
	}
	var vals []*string
	if err := json.Unmarshal(raw, &vals); err != nil {
		return nil, fmt.Errorf("upstash: decode comments: %w", err)
	}
	for _, v := range vals {
		if v == nil {
			continue
		}
		var sc storedComment
		if err := json.Unmarshal([]byte(*v), &sc); err != nil || sc.IsDeleted {
			continue
		}
		comments = append(comments, sc.Comment)
	}
	return comments, nil
}

func (r *Redis) SoftDeleteComment(ctx context.Context, id string) error {
	raw, err := r.do(ctx, "GET", keyComment(id))
	if err != nil {
		return err
	}
	val, ok, err := decodeString(raw)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	var sc storedComment
	if err := json.Unmarshal([]byte(val), &sc); err != nil {
		return err
	}
	sc.IsDeleted = true
	b, _ := json.Marshal(sc)
	_, err = r.tx(ctx,
		[]string{"SET", keyComment(id), string(b)},
		[]string{"ZREM", keyCommentIndex, id},
	)
	return err
}

// Allow creates the counter with its TTL only if it is missing (SET NX EX),
// then increments it; INCR keeps the TTL, so the window is fixed from the
// first hit and shared by every function instance.
func (r *Redis) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	k := keyRate(key)
	res, err := r.tx(ctx,
		[]string{"SET", k, "0", "EX", strconv.Itoa(max(1, int(window.Seconds()))), "NX"},
		[]string{"INCR", k},
	)
	if err != nil {
		return false, err
	}
	var n int
	if err := json.Unmarshal(res[1], &n); err != nil {
		return false, fmt.Errorf("upstash: decode counter: %w", err)
	}
	return n <= limit, nil
}

// decodeString reads a GET result: a JSON string, or null when the key is missing.
func decodeString(raw json.RawMessage) (string, bool, error) {
	var v *string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false, fmt.Errorf("upstash: decode value: %w", err)
	}
	if v == nil {
		return "", false, nil
	}
	return *v, true, nil
}
