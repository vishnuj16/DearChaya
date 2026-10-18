package app

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vishnuj16/dear-chaya/db"
	"github.com/vishnuj16/dear-chaya/templates"
)

const (
	maxNameRunes    = 60
	maxContentRunes = 2000
	maxBodyBytes    = 16 << 10
	commentLimit    = 5 // notes per IP per commentWindow
	commentWindow   = 10 * time.Minute
)

// commentInput is the POST body, accepted as JSON or as a regular form post.
// Website is a honeypot: hidden from people, but naive bots fill it in.
type commentInput struct {
	AuthorName string `json:"author_name"`
	Content    string `json:"content"`
	Website    string `json:"website"`
}

// validate trims the input, applies the "Anonymous" default and returns a
// reader-facing message when something is wrong.
func (in *commentInput) validate() error {
	in.AuthorName = strings.Join(strings.Fields(in.AuthorName), " ")
	in.Content = strings.TrimSpace(strings.ReplaceAll(in.Content, "\r\n", "\n"))
	switch {
	case in.Website != "":
		return errors.New("Your note could not be saved.")
	case in.Content == "":
		return errors.New("Please write a note before saving it.")
	case utf8.RuneCountInString(in.Content) > maxContentRunes:
		return fmt.Errorf("Notes can be at most %d characters.", maxContentRunes)
	case utf8.RuneCountInString(in.AuthorName) > maxNameRunes:
		return fmt.Errorf("Names can be at most %d characters.", maxNameRunes)
	}
	if in.AuthorName == "" {
		in.AuthorName = "Anonymous"
	}
	return nil
}

func (a *App) listComments(w http.ResponseWriter, r *http.Request) {
	comments, err := a.store.ListComments(r.Context())
	if err != nil {
		log.Printf("list comments: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "Could not load notes right now.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, comments)
}

func (a *App) createComment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	isForm := ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data"

	var in commentInput
	if isForm {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		in = commentInput{AuthorName: r.PostForm.Get("author_name"), Content: r.PostForm.Get("content"), Website: r.PostForm.Get("website")}
	} else if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	if err := in.validate(); err != nil {
		if isForm {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Rate limit per IP. If the limiter itself is unavailable, let the note
	// through rather than block a real reader.
	if ok, err := a.store.Allow(r.Context(), "comment:"+clientIP(r), commentLimit, commentWindow); err != nil {
		log.Printf("create comment: rate limit: %v", err)
	} else if !ok {
		const msg = "You've left several notes just now. Please wait a few minutes."
		if isForm {
			http.Error(w, msg, http.StatusTooManyRequests)
			return
		}
		writeJSONError(w, http.StatusTooManyRequests, msg)
		return
	}

	c := db.Comment{ID: newID(), AuthorName: in.AuthorName, Content: in.Content, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	if err := a.store.InsertComment(r.Context(), c); err != nil {
		log.Printf("create comment: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "Couldn't save your note. Please try again.")
		return
	}

	switch {
	case isForm:
		// No-JavaScript fallback: back to the notes page.
		http.Redirect(w, r, "/#notes", http.StatusSeeOther)
	case strings.Contains(r.Header.Get("Accept"), "text/html"):
		// The page's own script asks for the rendered note so the markup
		// lives in one place (templates.CommentItem).
		var buf bytes.Buffer
		if err := templates.CommentItem(c, a.isAdmin(r)).Render(r.Context(), &buf); err != nil {
			log.Printf("render comment: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "Saved! Refresh to see your note.")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		buf.WriteTo(w)
	default:
		writeJSON(w, http.StatusCreated, c)
	}
}

// newID returns a random RFC 4122 version 4 UUID.
func newID() string {
	var b [16]byte
	rand.Read(b[:]) // never returns an error (Go 1.24+ crashes instead)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
