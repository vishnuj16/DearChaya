package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/vishnuj16/dear-chaya/db"
	"github.com/vishnuj16/dear-chaya/story"
)

func newTestApp(t *testing.T) (http.Handler, func() []db.Comment) {
	t.Helper()
	store := db.NewMemory()
	title, body := story.Default()
	if err := store.SeedStory(context.Background(), db.Story{Title: title, Content: body}); err != nil {
		t.Fatal(err)
	}
	list := func() []db.Comment {
		cs, err := store.ListComments(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}
	return New(store), list
}

func do(h http.Handler, method, path, contentType, accept, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestCreateCommentJSON(t *testing.T) {
	h, list := newTestApp(t)

	rec := do(h, "POST", "/api/comments", "application/json", "",
		`{"author_name":"  Meena   Miss ","content":"  Lovely story.\r\nThank you.  "}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var got db.Comment
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.AuthorName != "Meena Miss" || got.Content != "Lovely story.\nThank you." {
		t.Errorf("not normalised: %+v", got)
	}
	if !uuidV4.MatchString(got.ID) {
		t.Errorf("id %q is not a UUIDv4", got.ID)
	}

	stored := list()
	if len(stored) != 1 || stored[0].ID != got.ID || !stored[0].CreatedAt.Equal(got.CreatedAt) {
		t.Errorf("stored = %+v, want the created comment %+v", stored, got)
	}
}

func TestCreateCommentDefaultsToAnonymous(t *testing.T) {
	h, list := newTestApp(t)
	for _, name := range []string{`""`, `"   "`} {
		rec := do(h, "POST", "/api/comments", "application/json", "", `{"author_name":`+name+`,"content":"hi"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
	}
	rec := do(h, "POST", "/api/comments", "application/json", "", `{"content":"no name field"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, c := range list() {
		if c.AuthorName != "Anonymous" {
			t.Errorf("author = %q, want Anonymous", c.AuthorName)
		}
	}
}

func TestCreateCommentRejectsBadInput(t *testing.T) {
	h, list := newTestApp(t)
	cases := map[string]string{
		"empty content":      `{"author_name":"A","content":""}`,
		"whitespace content": `{"content":" \n\t "}`,
		"too long content":   `{"content":"` + strings.Repeat("a", maxContentRunes+1) + `"}`,
		"too long name":      `{"author_name":"` + strings.Repeat("n", maxNameRunes+1) + `","content":"hi"}`,
		"honeypot filled":    `{"content":"buy now","website":"http://spam.example"}`,
		"malformed json":     `{"content":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := do(h, "POST", "/api/comments", "application/json", "", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			var e map[string]string
			if json.NewDecoder(rec.Body).Decode(&e) != nil || e["error"] == "" {
				t.Errorf("want a JSON {error} body")
			}
		})
	}
	if n := len(list()); n != 0 {
		t.Errorf("%d comments stored, want 0", n)
	}
}

func TestCreateCommentRejectsOversizedBody(t *testing.T) {
	h, _ := newTestApp(t)
	rec := do(h, "POST", "/api/comments", "application/json", "", `{"content":"`+strings.Repeat("a", maxBodyBytes)+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateCommentHTMLFragmentIsEscaped(t *testing.T) {
	h, _ := newTestApp(t)
	rec := do(h, "POST", "/api/comments", "application/json", "text/html",
		`{"author_name":"<b>Kiran</b>","content":"<script>alert(1)</script>"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(body, "<li") {
		t.Fatalf("want an <li> HTML fragment, got %q", body)
	}
	if strings.Contains(body, "<script>") || strings.Contains(body, "<b>") {
		t.Errorf("user input not escaped: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("escaped content missing: %s", body)
	}
}

func TestCreateCommentFormFallbackRedirects(t *testing.T) {
	h, list := newTestApp(t)
	form := url.Values{"author_name": {""}, "content": {"Written without JavaScript"}}
	rec := do(h, "POST", "/api/comments", "application/x-www-form-urlencoded", "", form.Encode())
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/#notes" {
		t.Fatalf("status = %d, location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if cs := list(); len(cs) != 1 || cs[0].AuthorName != "Anonymous" {
		t.Errorf("stored = %+v", cs)
	}
}

func TestListCommentsNewestFirst(t *testing.T) {
	h, _ := newTestApp(t)
	for _, c := range []string{"first", "second", "third"} {
		if rec := do(h, "POST", "/api/comments", "application/json", "", `{"content":"`+c+`"}`); rec.Code != http.StatusCreated {
			t.Fatal(rec.Body)
		}
	}

	rec := do(h, "GET", "/api/comments", "", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got []db.Comment
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	// Same-second inserts fall back to insertion order (rowid DESC).
	if len(got) != 3 || got[0].Content != "third" || got[2].Content != "first" {
		t.Errorf("order = %v", contents(got))
	}
}

func TestListCommentsEmptyIsArray(t *testing.T) {
	h, _ := newTestApp(t)
	rec := do(h, "GET", "/api/comments", "", "", "")
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %q, want []", rec.Body)
	}
}

func TestHomeRendersNotes(t *testing.T) {
	h, _ := newTestApp(t)
	const hiddenEmpty = `style="display: none">No notes yet`
	body := do(h, "GET", "/", "", "", "").Body.String()
	if !strings.Contains(body, "No notes yet") || strings.Contains(body, hiddenEmpty) {
		t.Error("empty state should be visible with no notes")
	}
	do(h, "POST", "/api/comments", "application/json", "", `{"author_name":"Amma","content":"Proud of you"}`)
	body = do(h, "GET", "/", "", "", "").Body.String()
	// The empty state stays in the markup (so it can return if the author
	// deletes the last note) but must start hidden.
	if !strings.Contains(body, "Proud of you") || !strings.Contains(body, hiddenEmpty) {
		t.Error("comment not rendered, or empty state not hidden")
	}
}

func contents(cs []db.Comment) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Content
	}
	return out
}
