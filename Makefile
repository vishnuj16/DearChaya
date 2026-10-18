# Vercel's Go runtime does not run templ or Tailwind, so the generated
# *_templ.go files and static/app.css must be committed. Run `make build` first.

TAILWIND := npx @tailwindcss/cli
TEMPL    := go tool templ

.PHONY: build generate css test dev watch-css tidy admin-secrets

build: generate css test

generate:
	$(TEMPL) generate

css:
	$(TAILWIND) -i static/input.css -o static/app.css --minify

test:
	go vet ./...
	go build ./...
	go test ./...
	@# Vercel compiles api/ as a package outside this module's tree, so api/
	@# may not import any internal/ package (the build fails only on Vercel).
	@if go list -f '{{join .Imports "\n"}}' ./api | grep -q '/internal'; then \
		echo "api/ imports an internal/ package; Vercel cannot build that"; exit 1; fi

# Loads .env if present, so local runs can use your Upstash database.
dev: generate css
	set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/devserver

watch-css:
	$(TAILWIND) -i static/input.css -o static/app.css --watch

tidy:
	go mod tidy

# Prompts for the author password and prints ADMIN_PASSWORD_HASH and JWT_SECRET.
admin-secrets:
	@go run ./cmd/adminsecret
