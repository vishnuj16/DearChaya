// Package app wires the database, routes and templates. It is shared by the
// Vercel function (api/index.go) and the local dev server (cmd/devserver).
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/vishnuj16/dear-chaya/db"
	"github.com/vishnuj16/dear-chaya/internal/story"
	"github.com/vishnuj16/dear-chaya/static"
	"github.com/vishnuj16/dear-chaya/templates"
)

type App struct {
	store db.Store
	site  templates.Site
	auth  auth
}

var gaIDPattern = regexp.MustCompile(`^G-[A-Z0-9]{4,20}$`)

// siteFromEnv reads deployment settings. A malformed GA ID is dropped rather
// than rendered, so a typo can't break the page or inject markup.
func siteFromEnv() templates.Site {
	var s templates.Site
	if id := strings.TrimSpace(os.Getenv("GA_MEASUREMENT_ID")); id != "" {
		if gaIDPattern.MatchString(id) {
			s.GAMeasurementID = id
		} else {
			log.Printf("GA_MEASUREMENT_ID %q is not a GA4 ID (G-XXXXXXX); analytics disabled", id)
		}
	}
	return s
}

// Bootstrap connects to storage, seeds the story if it is missing and
// returns the router.
func Bootstrap() (http.Handler, error) {
	store, err := db.FromEnv()
	if err != nil {
		return nil, err
	}
	title, body := story.Default()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := store.SeedStory(ctx, db.Story{Title: title, Content: body}); err != nil {
		return nil, err
	}
	return New(store), nil
}

// New builds the router around a ready store.
func New(store db.Store) http.Handler {
	a := &App{store: store, site: siteFromEnv(), auth: authFromEnv()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("GET /api/comments", a.listComments)
	mux.HandleFunc("POST /api/comments", a.createComment)
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("GET /api/logout", a.logout)
	mux.HandleFunc("POST /api/logout", a.logout)
	mux.HandleFunc("POST /api/admin/delete-comment", a.requireAdmin(a.deleteComment))
	mux.HandleFunc("POST /api/admin/edit-story", a.requireAdmin(a.editStory))
	mux.Handle("GET /static/", staticCache(http.StripPrefix("/static/", http.FileServerFS(static.FS))))
	mux.HandleFunc("GET /robots.txt", serveRobots)
	mux.HandleFunc("GET /favicon.ico", serveFavicon)
	mux.HandleFunc("GET /site.webmanifest", serveManifest)
	mux.HandleFunc("/", a.notFound)
	return secure(mux)
}

// serveFavicon answers browsers that request /favicon.ico directly, so
// those requests don't 404 (and don't each wake a function for nothing).
func serveFavicon(w http.ResponseWriter, r *http.Request) {
	b, err := static.FS.ReadFile("img/favicon.ico")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Write(b)
}

// serveManifest lets Android use the Dear Chaya icon when the site is added
// to a home screen.
func serveManifest(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	json.NewEncoder(w).Encode(map[string]any{
		"name":             "Dear Chaya",
		"short_name":       "Dear Chaya",
		"start_url":        "/",
		"display":          "browser",
		"background_color": "#F4EFE6",
		"theme_color":      "#F4EFE6",
		"icons": []map[string]string{
			{"src": static.URL("img/icon-192.png"), "sizes": "192x192", "type": "image/png"},
			{"src": static.URL("img/icon-512.png"), "sizes": "512x512", "type": "image/png"},
		},
	})
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSONError(w, http.StatusNotFound, "Not found.")
		return
	}
	site := a.site
	site.Admin = a.isAdmin(r)
	var buf bytes.Buffer
	if err := templates.NotFound(site).Render(r.Context(), &buf); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	buf.WriteTo(w)
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.GetStory(r.Context())
	if err != nil {
		log.Printf("home: load story: %v", err)
		http.Error(w, "The diary is unavailable right now.", http.StatusInternalServerError)
		return
	}
	comments, err := a.store.ListComments(r.Context())
	if err != nil {
		// The story is what matters; show it with an empty notes page.
		log.Printf("home: load comments: %v", err)
		comments = nil
	}
	site := a.site
	site.Admin = a.isAdmin(r)
	// Render to a buffer so a template error doesn't leave a half-written 200.
	var buf bytes.Buffer
	if err := templates.Page(story.NewView(s.Title, s.Content), comments, site).Render(r.Context(), &buf); err != nil {
		log.Printf("home: render: %v", err)
		http.Error(w, "The diary is unavailable right now.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if site.Admin {
		// The author's view has editing controls; never let a cache keep it.
		w.Header().Set("Cache-Control", "private, no-store")
	}
	buf.WriteTo(w)
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	status, code := map[string]string{"status": "ok", "store": "ok"}, http.StatusOK
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		log.Printf("health: %v", err)
		status["status"], status["store"], code = "degraded", "unreachable", http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(status)
}

// staticCache lets browsers keep versioned asset URLs (?v=<hash>) forever;
// unversioned requests get a short lifetime.
func staticCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Versioned URLs and content-hashed font files never change.
		if r.URL.Query().Get("v") == static.Version || strings.HasPrefix(r.URL.Path, "/static/fonts/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		next.ServeHTTP(w, r)
	})
}
