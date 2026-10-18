package app

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vishnuj16/dear-chaya/db"
	"github.com/vishnuj16/dear-chaya/story"
	"golang.org/x/crypto/bcrypt"
)

const (
	testPassword = "correct horse battery"
	testSecret   = "0123456789abcdef0123456789abcdef-test"
)

// newAdminApp returns an app with author sign-in configured, and its store.
func newAdminApp(t *testing.T) (http.Handler, *db.Memory) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	t.Setenv("ADMIN_PASSWORD_HASH", string(hash))
	t.Setenv("JWT_SECRET", testSecret)
	store := db.NewMemory()
	title, body := story.Default()
	store.SeedStory(context.Background(), db.Story{Title: title, Content: body})
	return New(store), store
}

type reqOpt func(*http.Request)

func withCookie(c *http.Cookie) reqOpt { return func(r *http.Request) { r.AddCookie(c) } }
func withHeader(k, v string) reqOpt    { return func(r *http.Request) { r.Header.Set(k, v) } }

func send(h http.Handler, method, path, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(r)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func loginCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	rec := send(h, "POST", "/api/login", `{"password":"`+testPassword+`"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("login: status = %d, body = %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == adminCookie {
			return c
		}
	}
	t.Fatal("login set no admin_token cookie")
	return nil
}

func TestLoginSetsSecureCookie(t *testing.T) {
	h, _ := newAdminApp(t)
	rec := send(h, "POST", "/api/login", `{"password":"`+testPassword+`"}`, withHeader("X-Forwarded-Proto", "https"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	cs := rec.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("cookies = %v", cs)
	}
	c := cs[0]
	if c.Name != "admin_token" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge <= 0 {
		t.Errorf("cookie flags wrong: %+v", c)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	h, _ := newAdminApp(t)
	for _, body := range []string{`{"password":"nope"}`, `{"password":""}`, `{}`} {
		rec := send(h, "POST", "/api/login", body)
		if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
			t.Errorf("%s: status = %d, cookies = %v", body, rec.Code, rec.Result().Cookies())
		}
	}
}

func TestLoginIsRateLimitedPerIP(t *testing.T) {
	h, _ := newAdminApp(t)
	from := withHeader("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	for i := 0; i < loginLimit; i++ {
		send(h, "POST", "/api/login", `{"password":"guess"}`, from)
	}
	// Even the right password is refused once the budget is spent…
	if rec := send(h, "POST", "/api/login", `{"password":"`+testPassword+`"}`, from); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	// …but only for that address.
	if rec := send(h, "POST", "/api/login", `{"password":"`+testPassword+`"}`, withHeader("X-Forwarded-For", "198.51.100.7")); rec.Code != http.StatusNoContent {
		t.Errorf("other IP: status = %d, want 204", rec.Code)
	}
}

func TestLoginDisabledWithoutConfig(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD_HASH", "")
	t.Setenv("JWT_SECRET", "")
	h := New(db.NewMemory())
	if rec := send(h, "POST", "/api/login", `{"password":"anything"}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestAuthConfigValidation(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	cases := map[string][2]string{
		"short secret":   {string(hash), "too-short"},
		"not bcrypt":     {"plaintext-password", testSecret},
		"missing secret": {string(hash), ""},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ADMIN_PASSWORD_HASH", env[0])
			t.Setenv("JWT_SECRET", env[1])
			if authFromEnv().enabled() {
				t.Error("admin should be disabled")
			}
		})
	}
}

func TestForgedTokensAreRejected(t *testing.T) {
	au := auth{hash: []byte("x"), secret: []byte(testSecret)}
	now := time.Now()
	valid := au.issue(now)
	parts := strings.Split(valid, ".")
	enc := base64.RawURLEncoding.EncodeToString

	cases := map[string]string{
		"garbage":         "not-a-token",
		"alg none":        enc([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + parts[1] + ".",
		"tampered claims": parts[0] + "." + enc([]byte(`{"sub":"author","iat":0,"exp":99999999999}`)) + "." + parts[2],
		"other secret":    auth{hash: []byte("x"), secret: []byte(strings.Repeat("z", 40))}.issue(now),
		"expired":         au.issue(now.Add(-sessionLifetime - time.Minute)),
	}
	if !au.verify(valid, now) {
		t.Fatal("a freshly issued token must verify")
	}
	for name, tok := range cases {
		if au.verify(tok, now) {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDeleteCommentRequiresAdmin(t *testing.T) {
	h, store := newAdminApp(t)
	store.InsertComment(context.Background(), db.Comment{ID: "c1", AuthorName: "x", Content: "spam", CreatedAt: time.Now()})

	if rec := send(h, "POST", "/api/admin/delete-comment", `{"id":"c1"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d, want 401", rec.Code)
	}
	bogus := &http.Cookie{Name: adminCookie, Value: "forged.token.value"}
	if rec := send(h, "POST", "/api/admin/delete-comment", `{"id":"c1"}`, withCookie(bogus)); rec.Code != http.StatusUnauthorized {
		t.Errorf("forged cookie: status = %d, want 401", rec.Code)
	}

	cookie := loginCookie(t, h)
	evil := withHeader("Origin", "https://evil.example")
	if rec := send(h, "POST", "/api/admin/delete-comment", `{"id":"c1"}`, withCookie(cookie), evil); rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin: status = %d, want 403", rec.Code)
	}
	if cs, _ := store.ListComments(context.Background()); len(cs) != 1 {
		t.Fatal("comment deleted by an unauthorised request")
	}

	same := withHeader("Origin", "http://example.com") // httptest's default Host
	if rec := send(h, "POST", "/api/admin/delete-comment", `{"id":"c1"}`, withCookie(cookie), same); rec.Code != http.StatusOK {
		t.Fatalf("author: status = %d, body = %s", rec.Code, rec.Body)
	}
	if cs, _ := store.ListComments(context.Background()); len(cs) != 0 {
		t.Error("comment still visible after delete")
	}
	if rec := send(h, "POST", "/api/admin/delete-comment", `{"id":"nope"}`, withCookie(cookie)); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: status = %d, want 404", rec.Code)
	}
}

func TestEditStory(t *testing.T) {
	h, store := newAdminApp(t)
	body := `{"title":"  Dear   Chaya ","content":"Dear Diary,\r\n\r\n> A brand new *thought*."}`

	if rec := send(h, "POST", "/api/admin/edit-story", body); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d, want 401", rec.Code)
	}
	cookie := loginCookie(t, h)
	for _, bad := range []string{`{"title":"","content":"x"}`, `{"title":"T","content":"   "}`, `{"title":"T","content":"` + strings.Repeat("a", maxStoryBytes+1) + `"}`} {
		if rec := send(h, "POST", "/api/admin/edit-story", bad, withCookie(cookie)); rec.Code != http.StatusBadRequest {
			t.Errorf("invalid edit accepted: status = %d", rec.Code)
		}
	}
	if rec := send(h, "POST", "/api/admin/edit-story", body, withCookie(cookie)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	got, _ := store.GetStory(context.Background())
	if got.Title != "Dear Chaya" || got.Content != "Dear Diary,\n\n> A brand new *thought*." {
		t.Errorf("stored %+v", got)
	}
	page := send(h, "GET", "/", "").Body.String()
	if !strings.Contains(page, "A brand new <em>thought</em>.") {
		t.Error("the page does not show the edited story")
	}
}

func TestAdminControlsOnlyForAuthor(t *testing.T) {
	h, store := newAdminApp(t)
	store.InsertComment(context.Background(), db.Comment{ID: "c1", AuthorName: "x", Content: "hello", CreatedAt: time.Now()})

	reader := send(h, "GET", "/", "")
	for _, s := range []string{"ink-stamp", "Edit Story", "Author mode", "editor-story-body"} {
		if strings.Contains(reader.Body.String(), s) {
			t.Errorf("reader's page contains %q", s)
		}
	}

	author := send(h, "GET", "/", "", withCookie(loginCookie(t, h)))
	for _, s := range []string{"ink-stamp", "Edit Story", "Author mode", "editor-story-body", "Kiran", `"> Story</span>`, `"> read</span>`} {
		if !strings.Contains(author.Body.String(), s) {
			t.Errorf("author's page is missing %q", s)
		}
	}
	if cc := author.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("author page Cache-Control = %q, want no-store", cc)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	h, _ := newAdminApp(t)
	for _, method := range []string{"POST", "GET"} {
		rec := send(h, method, "/api/logout", "")
		cs := rec.Result().Cookies()
		if len(cs) != 1 || cs[0].Name != adminCookie || cs[0].MaxAge >= 0 {
			t.Errorf("%s: cookies = %+v, want admin_token expired", method, cs)
		}
	}
}
