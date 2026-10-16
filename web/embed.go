// Package web embeds the compiled frontend so a copied binary serves the local
// UI without an external file or CDN. See
// docs/04-ui-and-cli-specification.md section 6.
//
// The dist directory is produced by `make ui` (npm build) and is not committed.
// A Go build therefore requires the UI assets to be built first; the Makefile
// encodes that order.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

// DistFS returns the root of the compiled frontend (index.html and assets).
func DistFS() (fs.FS, error) {
	return fs.Sub(files, "dist")
}
