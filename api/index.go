// Package handler is the Vercel Go function entry point. vercel.json rewrites
// every non-static request here; routing happens in internal/app.
package handler

import (
	"log"
	"net/http"
	"sync"

	"github.com/vishnuj16/dear-chaya/internal/app"
)

var (
	once    sync.Once
	router  http.Handler
	initErr error
)

// Handler is invoked by Vercel per request. Setup runs once per warm instance.
func Handler(w http.ResponseWriter, r *http.Request) {
	once.Do(func() {
		router, initErr = app.Bootstrap()
		if initErr != nil {
			log.Printf("bootstrap failed: %v", initErr)
		}
	})
	if initErr != nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	router.ServeHTTP(w, r)
}
