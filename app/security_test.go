package app

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestSecurityHeadersAndNonce(t *testing.T) {
	h, _ := newAdminApp(t)
	t.Setenv("GA_MEASUREMENT_ID", "G-TEST1234")
	h, _ = newAdminApp(t) // rebuild so the GA tag (an inline script) is rendered

	rec := send(h, "GET", "/", "", withHeader("X-Forwarded-Proto", "https"))
	hdr := rec.Header()
	for k, want := range map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Strict-Transport-Security": "max-age=63072000",
	} {
		if !strings.Contains(hdr.Get(k), want) {
			t.Errorf("%s = %q, want it to contain %q", k, hdr.Get(k), want)
		}
	}

	csp := hdr.Get("Content-Security-Policy")
	m := regexp.MustCompile(`'nonce-([A-Za-z0-9+/]+)'`).FindStringSubmatch(csp)
	if m == nil {
		t.Fatalf("CSP has no nonce: %q", csp)
	}
	for _, d := range []string{"default-src 'self'", "frame-ancestors 'none'", "object-src 'none'", "font-src 'self'"} {
		if !strings.Contains(csp, d) {
			t.Errorf("CSP missing %q", d)
		}
	}

	// Every inline script must carry this request's nonce, or the CSP blocks it.
	body := rec.Body.String()
	inline := regexp.MustCompile(`<script(?: [^>]*)?>`).FindAllString(body, -1)
	for _, tag := range inline {
		if strings.Contains(tag, " src=") {
			continue
		}
		if !strings.Contains(tag, `nonce="`+m[1]+`"`) {
			t.Errorf("inline script without the request nonce: %s", tag)
		}
	}
	if len(inline) == 0 {
		t.Fatal("no scripts found")
	}

	// A second request gets a different nonce.
	if send(h, "GET", "/", "").Header().Get("Content-Security-Policy") == csp {
		t.Error("nonce reused across requests")
	}

	// No third-party CDNs left on the page (only Google Analytics, by design).
	for _, host := range []string{"cdn.jsdelivr.net", "fonts.googleapis.com", "fonts.gstatic.com"} {
		if strings.Contains(body, host) {
			t.Errorf("page still loads from %s", host)
		}
	}
}

func TestHSTSOnlyOverHTTPS(t *testing.T) {
	h, _ := newAdminApp(t)
	if v := send(h, "GET", "/", "").Header().Get("Strict-Transport-Security"); v != "" {
		t.Errorf("HSTS sent over plain HTTP: %q", v)
	}
}

func TestCommentRateLimit(t *testing.T) {
	h, _ := newAdminApp(t)
	from := withHeader("X-Forwarded-For", "203.0.113.50")
	for i := 0; i < commentLimit; i++ {
		if rec := send(h, "POST", "/api/comments", `{"content":"note"}`, from); rec.Code != http.StatusCreated {
			t.Fatalf("note %d: status = %d", i+1, rec.Code)
		}
	}
	if rec := send(h, "POST", "/api/comments", `{"content":"one too many"}`, from); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	if rec := send(h, "POST", "/api/comments", `{"content":"someone else"}`, withHeader("X-Forwarded-For", "198.51.100.1")); rec.Code != http.StatusCreated {
		t.Errorf("other reader: status = %d, want 201", rec.Code)
	}
}

func TestRobotsFaviconAndNotFound(t *testing.T) {
	h, _ := newAdminApp(t)

	robots := send(h, "GET", "/robots.txt", "")
	if robots.Code != http.StatusOK || !strings.Contains(robots.Body.String(), "User-agent: GPTBot") || !strings.Contains(robots.Body.String(), "Disallow: /api/") {
		t.Errorf("robots.txt: %d %q", robots.Code, robots.Body)
	}

	fav := send(h, "GET", "/favicon.ico", "")
	if fav.Code != http.StatusOK || fav.Header().Get("Content-Type") != "image/x-icon" {
		t.Errorf("favicon: %d %s", fav.Code, fav.Header().Get("Content-Type"))
	}

	page := send(h, "GET", "/no-such-page", "")
	if page.Code != http.StatusNotFound || !strings.Contains(page.Body.String(), "isn&#39;t in the diary") {
		t.Errorf("404 page: %d", page.Code)
	}
	api := send(h, "GET", "/api/nope", "")
	if api.Code != http.StatusNotFound || !strings.HasPrefix(api.Header().Get("Content-Type"), "application/json") {
		t.Errorf("api 404: %d %s", api.Code, api.Header().Get("Content-Type"))
	}
}

func TestCopyProtectionOffForAuthor(t *testing.T) {
	h, _ := newAdminApp(t)
	reader := send(h, "GET", "/", "").Body.String()
	if !strings.Contains(reader, "protected") || !strings.Contains(reader, "privacy-shield") {
		t.Error("reader page should be protected and have the privacy shield")
	}
	author := send(h, "GET", "/", "", withCookie(loginCookie(t, h))).Body.String()
	if regexp.MustCompile(`<body class="[^"]*\bprotected\b`).MatchString(author) || strings.Contains(author, `class="privacy-shield"`) {
		t.Error("author page should not be copy-protected")
	}
	if n := strings.Count(author, "$dispatch('story-edit')"); n != 1 {
		t.Errorf("%d buttons open the story editor; want exactly one (the header's)", n)
	}
}

func TestAnalyticsSkippedForAuthor(t *testing.T) {
	t.Setenv("GA_MEASUREMENT_ID", "G-TEST1234")
	h, _ := newAdminApp(t)
	const tag = "googletagmanager.com/gtag/js?id=G-TEST1234"
	if !strings.Contains(send(h, "GET", "/", "").Body.String(), tag) {
		t.Error("readers should get the GA tag")
	}
	if strings.Contains(send(h, "GET", "/", "", withCookie(loginCookie(t, h))).Body.String(), tag) {
		t.Error("the signed-in author's visits should not be tracked")
	}
}
