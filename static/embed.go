// Package static embeds the compiled stylesheet and scripts so the Go function
// can serve them without relying on files being present next to the binary.
package static

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
)

//go:embed app.css reader.js img/*.png img/*.ico vendor/*.js fonts/*.woff2
var FS embed.FS

// Version is a content hash of the embedded assets, appended as ?v= so
// browsers can cache them indefinitely and still pick up new deploys.
var Version = func() string {
	h := sha256.New()
	fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := FS.ReadFile(path)
			h.Write(b)
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:10]
}()

// URL returns the cache-busted path for an embedded asset.
func URL(name string) string { return "/static/" + name + "?v=" + Version }
