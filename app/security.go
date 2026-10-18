package app

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/a-h/templ"
)

// contentSecurityPolicy only lets the page load code from this site, plus
// Google Analytics. Inline scripts must carry the per-request nonce.
// 'unsafe-eval' is required by Alpine.js, which evaluates x-* expressions;
// 'unsafe-inline' for styles covers Alpine's x-show / x-bind:style.
func contentSecurityPolicy(nonce string) string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'nonce-" + nonce + "' 'unsafe-eval' https://www.googletagmanager.com",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: https://www.googletagmanager.com https://*.google-analytics.com",
		"font-src 'self'",
		"connect-src 'self' https://*.google-analytics.com https://*.analytics.google.com https://www.googletagmanager.com",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"object-src 'none'",
	}, "; ")
}

// secure wraps the router with security headers and panic recovery.
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic serving %s %s: %v\n%s", r.Method, r.URL.Path, v, debug.Stack())
				http.Error(w, "Something went wrong.", http.StatusInternalServerError)
			}
		}()

		nonce := newNonce()
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy(nonce))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), browsing-topics=()")
		if isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r.WithContext(templ.WithNonce(r.Context(), nonce)))
	})
}

func newNonce() string {
	var b [16]byte
	rand.Read(b[:])
	return base64.RawStdEncoding.EncodeToString(b[:])
}

// robotsTxt lets search engines index the story but asks AI-training
// crawlers to stay out. (Polite bots honour this; it is not enforcement.)
const robotsTxt = `User-agent: GPTBot
User-agent: ChatGPT-User
User-agent: OAI-SearchBot
User-agent: ClaudeBot
User-agent: Claude-Web
User-agent: anthropic-ai
User-agent: CCBot
User-agent: Google-Extended
User-agent: Applebot-Extended
User-agent: PerplexityBot
User-agent: Bytespider
User-agent: meta-externalagent
User-agent: Diffbot
User-agent: cohere-ai
Disallow: /

User-agent: *
Allow: /
Disallow: /api/
`

func serveRobots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write([]byte(robotsTxt))
}
