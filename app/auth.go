package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	adminCookie     = "admin_token"
	sessionLifetime = 7 * 24 * time.Hour
	loginLimit      = 5 // attempts per IP per loginWindow
	loginWindow     = 15 * time.Minute
	minSecretBytes  = 32
)

// auth holds the author credentials. When either env var is missing or
// invalid, admin is disabled: logins fail and isAdmin is always false.
type auth struct {
	hash   []byte
	secret []byte
}

func authFromEnv() auth {
	hash := []byte(strings.TrimSpace(os.Getenv("ADMIN_PASSWORD_HASH")))
	secret := []byte(os.Getenv("JWT_SECRET"))
	switch {
	case len(hash) == 0 && len(secret) == 0:
		return auth{} // not configured; silent, this is the normal local default
	case len(hash) == 0 || len(secret) == 0:
		log.Print("auth: set both ADMIN_PASSWORD_HASH and JWT_SECRET; admin disabled")
		return auth{}
	case len(secret) < minSecretBytes:
		log.Printf("auth: JWT_SECRET must be at least %d characters; admin disabled", minSecretBytes)
		return auth{}
	}
	if _, err := bcrypt.Cost(hash); err != nil {
		log.Printf("auth: ADMIN_PASSWORD_HASH is not a bcrypt hash (%v); admin disabled", err)
		return auth{}
	}
	return auth{hash: hash, secret: secret}
}

func (au auth) enabled() bool { return len(au.hash) > 0 && len(au.secret) > 0 }

// The only header we issue. Verification compares against it byte-for-byte,
// so tokens claiming another algorithm ("none", RS256, …) are rejected.
var jwtHeader = b64(mustJSON(map[string]string{"alg": "HS256", "typ": "JWT"}))

type claims struct {
	Sub string `json:"sub"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

func (au auth) issue(now time.Time) string {
	payload := b64(mustJSON(claims{Sub: "author", Iat: now.Unix(), Exp: now.Add(sessionLifetime).Unix()}))
	unsigned := jwtHeader + "." + payload
	return unsigned + "." + au.sign(unsigned)
}

func (au auth) verify(token string, now time.Time) bool {
	if !au.enabled() {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != jwtHeader {
		return false
	}
	if !hmac.Equal([]byte(parts[2]), []byte(au.sign(parts[0]+"."+parts[1]))) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var c claims
	if json.Unmarshal(raw, &c) != nil {
		return false
	}
	return c.Sub == "author" && now.Unix() < c.Exp
}

func (au auth) sign(s string) string {
	m := hmac.New(sha256.New, au.secret)
	m.Write([]byte(s))
	return b64(m.Sum(nil))
}

// isAdmin reports whether the request carries a valid author session cookie.
func (a *App) isAdmin(r *http.Request) bool {
	c, err := r.Cookie(adminCookie)
	return err == nil && a.auth.verify(c.Value, time.Now())
}

// requireAdmin guards author-only endpoints: same-origin, signed in.
func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeJSONError(w, http.StatusForbidden, "Cross-site request refused.")
			return
		}
		if !a.isAdmin(r) {
			writeJSONError(w, http.StatusUnauthorized, "Please sign in again.")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next(w, r)
	}
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "Cross-site request refused.")
		return
	}
	if !a.auth.enabled() {
		writeJSONError(w, http.StatusServiceUnavailable, "Author sign-in is not set up on this server.")
		return
	}
	ok, err := a.store.Allow(r.Context(), "login:"+clientIP(r), loginLimit, loginWindow)
	if err != nil {
		log.Printf("login: rate limit: %v", err)
		writeJSONError(w, http.StatusServiceUnavailable, "Sign-in is unavailable right now.")
		return
	}
	if !ok {
		writeJSONError(w, http.StatusTooManyRequests, "Too many attempts. Try again in 15 minutes.")
		return
	}

	var in struct {
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if bcrypt.CompareHashAndPassword(a.auth.hash, []byte(in.Password)) != nil {
		writeJSONError(w, http.StatusUnauthorized, "That isn't the author's password.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookie,
		Value:    a.auth.issue(time.Now()),
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// logout clears the cookie. GET (a plain link) redirects home; POST returns 204.
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sameOrigin rejects browser requests sent from another site. SameSite=Lax
// already keeps the cookie off cross-site POSTs; this is a second layer.
// Requests without an Origin header (curl, older browsers) are allowed.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host || u.Host == r.Header.Get("X-Forwarded-Host")
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// clientIP uses the first X-Forwarded-For hop, which Vercel's edge sets to
// the real client address, and falls back to the TCP peer locally.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(ip)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(errors.Join(errors.New("auth: marshal"), err))
	}
	return b
}
