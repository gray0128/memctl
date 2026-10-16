package integration_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// This file verifies stage B-3 subtask 1 (T025) from
// docs/05-validation-and-delivery.md through the HTTP handler:
// R01's embedded UI asset scenario (the page and its static files are served
// only from the binary, with no external CDN), A02 (the injected meta token is
// the server session token and authenticates the API) and A03 (a read-only
// server rejects write requests for the page and the API). Every run uses a
// temporary HOME and memory repository.

var (
	uiTokenPattern  = regexp.MustCompile(`name="memctl-token" content="([^"]+)"`)
	uiScriptPattern = regexp.MustCompile(`src="(/assets/[^"]+\.js)"`)
	uiStylePattern  = regexp.MustCompile(`href="(/assets/[^"]+\.css)"`)
)

func mustUIAssetPath(t *testing.T, pattern *regexp.Regexp, body string) string {
	t.Helper()
	match := pattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("index does not reference a matching asset for %s:\n%s", pattern.String(), body)
	}
	return match[1]
}

// TestB3EmbeddedUIAssetsCoverR01A02 checks that the index page and its assets
// come from the embedded filesystem, carry no external reference and expose the
// session token only in the meta tag.
func TestB3EmbeddedUIAssetsCoverR01A02(t *testing.T) {
	fixture := newB1Fixture(t, false)

	index := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/", "", "127.0.0.1:18420", "")
	mustStatus(t, index.status, http.StatusOK)
	if contentType := index.header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("index content type = %q, want text/html", contentType)
	}
	body := string(index.body)
	if !strings.Contains(body, `name="memctl-readonly" content="false"`) {
		t.Fatalf("index must carry the read-only flag:\n%s", body)
	}

	// R01: the page must not load an external CDN resource.
	for _, bad := range []string{`src="http`, `href="http`, `src="//`, `href="//`} {
		if strings.Contains(body, bad) {
			t.Fatalf("index references an external resource (%q):\n%s", bad, body)
		}
	}

	// The page CSP allows only this server and forbids inline or external code.
	csp := index.header.Get("Content-Security-Policy")
	for _, want := range []string{"script-src 'self'", "connect-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("page CSP %q is missing %q", csp, want)
		}
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "http:") || strings.Contains(csp, "https:") {
		t.Fatalf("page CSP must not allow inline or external sources: %q", csp)
	}

	// A02: the injected token is the server session token and authenticates the
	// API, so the page needs no separate token handshake.
	match := uiTokenPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("index has no injected session token:\n%s", body)
	}
	injected := match[1]
	if injected != b1Token {
		t.Fatalf("injected token = %q, want the server session token", injected)
	}
	snapshot := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/api/v1/snapshot", injected, "127.0.0.1:18420", "")
	mustStatus(t, snapshot.status, http.StatusOK)

	// R01: the referenced assets are embedded and served locally; they never
	// carry the session token, which stays in the page meta tag.
	assets := []string{
		mustUIAssetPath(t, uiScriptPattern, body),
		mustUIAssetPath(t, uiStylePattern, body),
	}
	for _, assetPath := range assets {
		asset := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420"+assetPath, "", "127.0.0.1:18420", "")
		mustStatus(t, asset.status, http.StatusOK)
		if len(asset.body) == 0 {
			t.Fatalf("embedded asset %s is empty", assetPath)
		}
		if strings.Contains(string(asset.body), b1Token) {
			t.Fatalf("embedded asset %s leaks the session token", assetPath)
		}
	}

	// An unknown asset is a plain 404 instead of falling back to the page, and
	// the asset directory never produces a listing.
	for _, missingPath := range []string{"/assets/missing.js", "/assets/"} {
		missing := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420"+missingPath, "", "127.0.0.1:18420", "")
		mustStatus(t, missing.status, http.StatusNotFound)
	}
}

// TestB3ReadOnlyUIRejectsWritesCoversA03 checks that the read-only mode is
// enforced by the server for the page and the API, not by hiding UI controls.
func TestB3ReadOnlyUIRejectsWritesCoversA03(t *testing.T) {
	fixture := newB1Fixture(t, true)

	index := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/", "", "127.0.0.1:18420", "")
	mustStatus(t, index.status, http.StatusOK)
	if !strings.Contains(string(index.body), `name="memctl-readonly" content="true"`) {
		t.Fatal("a read-only server must advertise the flag in the served page")
	}

	write := fixture.do(t, http.MethodPost, "http://127.0.0.1:18420/api/v1/snapshot", b1Token, "127.0.0.1:18420", "")
	mustStatus(t, write.status, http.StatusForbidden)
	mustCode(t, errorCode(t, write), "READ_ONLY")

	// The page route is GET-only; a write to it is rejected before routing.
	post := fixture.do(t, http.MethodPost, "http://127.0.0.1:18420/", b1Token, "127.0.0.1:18420", "")
	mustStatus(t, post.status, http.StatusForbidden)
	mustCode(t, errorCode(t, post), "READ_ONLY")

	// Host validation applies to the page exactly like the API.
	badHost := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/", "", "evil.example:18420", "")
	mustStatus(t, badHost.status, http.StatusBadRequest)
}
