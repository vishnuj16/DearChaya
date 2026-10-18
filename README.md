# Dear Chaya

A single-story reading site for **"Dear Chaya"** by Vishnu Vyas: the story laid out as a spiral-bound notebook, with a "Notes & Thoughts" page for readers' comments, a hidden author mode for editing the story and moderating notes, and Google Analytics for reading engagement.

Built with Go, [templ](https://templ.guide), Tailwind CSS v4 and Alpine.js, deployed as a Vercel Go function, with data in Upstash Redis.

> **Keep this repository private.** The full story text lives in `internal/story/story.md`.

---

## Quick start (local)

Requirements: Go 1.24+, Node.js 20+ (only for building the CSS).

```bash
npm install          # Tailwind CLI, once
cp .env.example .env # then fill it in (see below)
make dev             # builds templates + CSS, runs http://localhost:8080
```

`make dev` loads `.env` automatically. Without Upstash credentials it falls back to an **in-memory store** (comments vanish on restart), which is handy for quick experiments.

| Command | What it does |
|---|---|
| `make build` | `templ generate`, Tailwind build, `go vet`, `go build`, `go test`. **Run before every commit.** |
| `make dev` | Local server on `:8080` (set `PORT` to change). |
| `make watch-css` | Rebuild CSS on change while editing templates. |
| `make admin-secrets` | Prompts for an author password; prints `ADMIN_PASSWORD_HASH` and `JWT_SECRET`. |
| `make tidy` | `go mod tidy`. |

Vercel does not run templ or Tailwind, so the generated `templates/*_templ.go` files and `static/app.css` **are committed on purpose**. Never add them to `.gitignore`.

---

## Configuration

All settings are environment variables: `.env` locally, **Vercel → Settings → Environment Variables** in production. A change in Vercel only takes effect after a redeploy.

| Variable | Required | Purpose |
|---|---|---|
| `UPSTASH_REDIS_REST_URL` | Yes on Vercel | Upstash database → REST API section. (`KV_REST_API_URL` from Vercel's Upstash integration also works.) |
| `UPSTASH_REDIS_REST_TOKEN` | Yes on Vercel | The read-write token, not the read-only one. |
| `ADMIN_PASSWORD_HASH` | For author mode | bcrypt hash from `make admin-secrets`. |
| `JWT_SECRET` | For author mode | Session signing key (32+ chars) from `make admin-secrets`. Changing it signs out all sessions. |
| `GA_MEASUREMENT_ID` | Optional | GA4 ID (`G-XXXXXXXXXX`). Analytics is off when empty. |

**Quoting:** in `.env`, wrap the hash and secret in single quotes (`ADMIN_PASSWORD_HASH='$2a$12$…'`), because the hash contains `$`. In Vercel, paste them **without** quotes.

On Vercel the app **refuses to start** without Upstash credentials rather than silently losing comments. Misconfigured admin settings disable author mode and log a line starting with `auth:`.

---

## Author mode

- **Sign in:** triple-click "© Vishnu Vyas" in the footer, or press `Ctrl+Shift+A` (Chrome may claim that shortcut; the triple-click always works).
- **Edit Story** (sticky header): edit the title and the story's markdown. Saves straight to Upstash, with no redeploy. Formatting: a blank line starts a new paragraph, `*italics*`, and a paragraph starting with `> ` is a highlighted thought.
- **Delete** (red stamp on each note): hides the note from readers but keeps the record in Upstash.
- Sessions last 7 days. Logins are limited to 5 attempts per 15 minutes per IP address.
- While signed in, copy protection and analytics are off for you.

The story in `internal/story/story.md` is only the **initial seed**: it is copied to Upstash on first start and never overwrites later edits. To reset to the file, delete the `dearchaya:story` key in the Upstash console and redeploy.

---

## Deploying (Vercel)

1. `make build`, then commit and push to GitHub (private repo).
2. Vercel → **Add New → Project** → import the repo. Framework preset **Other** (`vercel.json` also forces this with `"framework": null`); leave the build and output settings as they are.
3. Add the environment variables above, then **Deploy**.
4. Check `https://<project>.vercel.app/health` → `{"status":"ok","store":"ok"}`.
5. **Settings → Functions → Function Region**: pick the region closest to your Upstash database, then redeploy.

Every push to `main` redeploys automatically.

`vercel.json` serves only `public/` as static files and sends every other request to the Go function. That keeps source files such as `internal/story/story.md` from being downloadable.

---

## Project layout

```
api/index.go            Vercel entry point (Handler); routing lives in internal/app
cmd/devserver/          Local dev server (same router). Not named cmd/server on purpose:
                        Vercel would mistake that for a Go server app and skip api/
cmd/adminsecret/        `make admin-secrets`
internal/app/           HTTP handlers: pages, comments, auth, admin, security headers
internal/story/         Bundled story (story.md) and its tiny markdown parser
db/                     Store interface; Upstash Redis client (REST, stdlib only); in-memory store
templates/              templ components (+ generated *_templ.go)
static/                 input.css → app.css, reader.js, fonts, images, vendored Alpine.js
public/                 Files Vercel serves directly from its CDN (favicon.ico only)
tools/make_icon.py      Regenerates the favicon set
```

### Routes

| Route | |
|---|---|
| `GET /` | The story and the notes |
| `GET /health` | Health check (pings Upstash) |
| `GET /api/comments` · `POST /api/comments` | List / add notes (5 per 10 min per IP; honeypot spam field) |
| `POST /api/login` · `GET\|POST /api/logout` | Author session (`admin_token` cookie: HttpOnly, SameSite=Lax, Secure) |
| `POST /api/admin/delete-comment` · `POST /api/admin/edit-story` | Author only |
| `GET /robots.txt` · `/favicon.ico` · `/site.webmanifest` | |

### Data in Upstash

Every key is prefixed `dearchaya:`, so the database can be shared with other projects safely.

| Key | Contents |
|---|---|
| `dearchaya:story` | JSON `{title, content}` |
| `dearchaya:comment:<uuid>` | JSON note, including `is_deleted` |
| `dearchaya:comments` | Sorted set of visible note ids, newest last |
| `dearchaya:rate:*` | Short-lived rate-limit counters |

---

## Analytics (GA4)

Loaded only when `GA_MEASUREMENT_ID` is set, and never for the signed-in author.

| Event | When | Parameter |
|---|---|---|
| `page_view` | Automatic, a visitor arrives | |
| `story_scroll_milestone` | 25 / 50 / 75 / 90 / 100 % of the story seen | `percent` |
| `story_completed` | Reached the end (once per visit) | `active_seconds` |
| `story_time_spent` | Active reading time passes 15 s, 30 s, 1, 2, 3, 5, 8, 10, 15 min | `seconds` |
| `story_comment_posted` | A reader left a note | |

"Active" means the tab is visible and focused. GA's built-in engagement time also works. To see parameter values in reports, register `percent`, `seconds` and `active_seconds` as event-scoped custom metrics (GA Admin → Custom definitions). Mark `story_completed` as a key event. If you have EU/UK readers, a cookie-consent banner is legally required.

---

## Privacy and security

**Copy protection for readers** (`static/reader.js`, `static/input.css`) blocks:
- text selection, copying, right-click and dragging;
- Ctrl+A/C/X/S/P/U;
- printing ("Save as PDF" produces a black page);
- viewing while the page is hidden or unfocused (a black screen covers it);
- PrintScreen on Windows (the clipboard copy is overwritten).

These are **deterrents only**. No website can block operating-system screenshots or a determined copier (view-source, reader mode, JavaScript disabled).

**Security headers** (`internal/app/security.go`):
- a strict Content Security Policy, with a per-request nonce for inline scripts;
- `frame-ancestors 'none'`, `nosniff`, `Referrer-Policy`, `Permissions-Policy`, and HSTS on HTTPS.

Alpine.js requires `'unsafe-eval'` in the policy.

**Other measures:**
- **No third-party loading:** fonts and Alpine.js are served from this site, so the only third party is Google Analytics.
- **Robots:** `robots.txt` allows search engines but asks AI-training crawlers to stay out.

---

## Testing

```bash
make build                          # includes go test ./...
go test -race ./...                 # with the race detector
```

- `db/store_test.go` runs the same checks against the in-memory store and a fake Upstash server.
- `internal/app/*_test.go` covers comments, auth (including forged and expired tokens), admin endpoints, rate limits, security headers and the analytics on/off rules.

---

## Credits

- Story, text and the Stories by Vyas marks © Vishnu Vyas. All rights reserved.
- [Alpine.js](https://alpinejs.dev) (MIT, see `static/vendor/LICENSE-alpinejs.md`).
- Fonts: Kalam, Caveat and Inter (SIL Open Font License), self-hosted.
