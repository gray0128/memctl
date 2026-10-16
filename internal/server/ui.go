package server

import (
	"bytes"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/web"
)

// The Go server replaces these placeholders in the built index.html before the
// page is served. The session token is injected only into the current page's
// meta tag; it is never placed in a URL, a log or localStorage.
const (
	indexFileName       = "index.html"
	tokenPlaceholder    = "__MEMCTL_TOKEN__"
	readOnlyPlaceholder = "__MEMCTL_READONLY__"

	// codeUIAssetsMissing is reported when the binary was built without the
	// compiled frontend. It is an operational build error, not an API contract
	// code: `memctl ui` fails fast with UNSUPPORTED instead of serving a page.
	codeUIAssetsMissing = "UI_ASSETS_MISSING"
)

// loadUI reads the embedded frontend once and renders the index page with the
// session token and read-only flag. A missing dist is recorded instead of
// panicking so API-only callers still work; the UI routes and `memctl ui`
// surface the error explicitly.
func (s *Server) loadUI() {
	dist, err := web.DistFS()
	if err != nil {
		s.uiErr = memerr.Wrap(memerr.IOFailed, err, "cannot open the embedded UI assets")
		return
	}
	index, err := fs.ReadFile(dist, indexFileName)
	if err != nil {
		s.uiErr = memerr.New(memerr.Unsupported,
			"the embedded UI assets do not contain %s; build them with `make ui`", indexFileName)
		return
	}
	rendered := bytes.ReplaceAll(index, []byte(tokenPlaceholder), []byte(s.token))
	readOnly := []byte("false")
	if s.readOnly {
		readOnly = []byte("true")
	}
	rendered = bytes.ReplaceAll(rendered, []byte(readOnlyPlaceholder), readOnly)
	s.uiFS = dist
	s.indexHTML = rendered
}

// UIReady reports whether the compiled frontend is available.
func (s *Server) UIReady() error { return s.uiErr }

// handleIndex serves the current page with the session token injected. The
// response is never cached, so a restarted server never hands out a stale token.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if s.uiErr != nil {
		writeError(w, http.StatusServiceUnavailable, codeUIAssetsMissing,
			s.uiErr.Error(), false)
		return
	}
	setUICSP(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(s.indexHTML)
}

// handleUIAsset serves the compiled script and style files from the embedded
// filesystem. Only regular files are served, so a directory request never
// produces a listing. Assets carry no token.
func (s *Server) handleUIAsset(w http.ResponseWriter, r *http.Request) {
	if s.uiFS == nil {
		writeError(w, http.StatusServiceUnavailable, codeUIAssetsMissing,
			"the compiled UI assets are not embedded in this binary", false)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	info, err := fs.Stat(s.uiFS, name)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	setUICSP(w)
	http.FileServerFS(s.uiFS).ServeHTTP(w, r)
}

// setUICSP relaxes the strict API policy for the current page only: scripts,
// styles and connections are limited to this server, and no external CDN or
// inline script is allowed.
func setUICSP(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; "+
			"font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
}
