// Command devserver runs the site locally with the same router Vercel uses.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/vishnuj16/dear-chaya/app"
)

func main() {
	h, err := app.Bootstrap()
	if err != nil {
		log.Fatal(err)
	}
	addr := ":" + cmp(os.Getenv("PORT"), "8080")
	log.Printf("listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, h))
}

func cmp(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
