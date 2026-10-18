package app

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/vishnuj16/dear-chaya/db"
)

const (
	maxTitleRunes  = 120
	maxStoryBytes  = 200 << 10
	maxStoryUpload = maxStoryBytes + 8<<10 // JSON overhead
)

// deleteComment soft-deletes a note: hidden from readers, record kept.
func (a *App) deleteComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.ID) == "" {
		writeJSONError(w, http.StatusBadRequest, "Missing comment id.")
		return
	}
	err := a.store.SoftDeleteComment(r.Context(), in.ID)
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "That note no longer exists.")
	case err != nil:
		log.Printf("delete comment %q: %v", in.ID, err)
		writeJSONError(w, http.StatusInternalServerError, "Couldn't delete the note. Please try again.")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": in.ID})
	}
}

// editStory replaces the story's title and markdown body.
func (a *App) editStory(w http.ResponseWriter, r *http.Request) {
	var in db.Story
	r.Body = http.MaxBytesReader(w, r.Body, maxStoryUpload)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body (is the story over 200 KB?).")
		return
	}
	in.Title = strings.Join(strings.Fields(in.Title), " ")
	in.Content = strings.TrimSpace(strings.ReplaceAll(in.Content, "\r\n", "\n"))
	switch {
	case in.Title == "":
		writeJSONError(w, http.StatusBadRequest, "The story needs a title.")
		return
	case utf8.RuneCountInString(in.Title) > maxTitleRunes:
		writeJSONError(w, http.StatusBadRequest, "The title is too long.")
		return
	case in.Content == "":
		writeJSONError(w, http.StatusBadRequest, "The story can't be empty.")
		return
	case len(in.Content) > maxStoryBytes:
		writeJSONError(w, http.StatusBadRequest, "The story is over 200 KB.")
		return
	}
	if err := a.store.UpdateStory(r.Context(), in); err != nil {
		log.Printf("edit story: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "Couldn't save the story. Please try again.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
