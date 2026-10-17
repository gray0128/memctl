package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAssetName(t *testing.T) {
	tests := []struct {
		goos, goarch string
		want         string
		ok           bool
	}{
		{"darwin", "arm64", "memctl-darwin-arm64", true},
		{"darwin", "amd64", "memctl-darwin-amd64", true},
		{"linux", "amd64", "memctl-linux-amd64", true},
		{"windows", "amd64", "memctl-windows-amd64.exe", true},
		{"linux", "arm64", "", false},
		{"windows", "arm64", "", false},
	}
	for _, tc := range tests {
		got, ok := assetName(tc.goos, tc.goarch)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("assetName(%q, %q) = %q, %v; want %q, %v", tc.goos, tc.goarch, got, ok, tc.want, tc.ok)
		}
	}
}

func TestUpdateAvailable(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v0.2.1", "v0.2.1", false},
		{"0.2.1", "v0.2.1", false},
		{"v0.2.0", "v0.2.1", true},
		{"v0.2.1", "v0.2.0", false},
		{"v0.3.0", "v0.2.1", false},
		{"v1.0.0", "v0.9.9", false},
		{"v2.0.0-dev", "v0.2.1", true},
		{"v0.2.1-3-gabc123", "v0.2.1", true},
		{"", "v0.2.1", true},
	}
	for _, tc := range tests {
		if got := updateAvailable(tc.current, tc.latest); got != tc.want {
			t.Fatalf("updateAvailable(%q, %q) = %v; want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	sum := strings.Repeat("a", 64)
	list := []byte("deadbeef  other-arm64\n" + strings.ToUpper(sum) + "  *memctl-darwin-arm64\n")
	got, ok := checksumFor(list, "memctl-darwin-arm64")
	if !ok || got != sum {
		t.Fatalf("checksumFor() = %q, %v; want %q, true", got, ok, sum)
	}
	if _, ok := checksumFor(list, "missing"); ok {
		t.Fatal("checksumFor found an absent asset")
	}
	if _, ok := checksumFor([]byte("not-a-checksum  memctl-darwin-arm64\n"), "memctl-darwin-arm64"); ok {
		t.Fatal("checksumFor accepted an invalid digest")
	}
}

func TestCheckReportsLatestWithoutWriting(t *testing.T) {
	server, requests := releaseServer(t, "old-binary", "v0.2.1", true, false)
	defer server.Close()

	result, err := Check(context.Background(), Options{
		CurrentVersion: "v0.2.0",
		APIBaseURL:     server.URL,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.LatestVersion != "v0.2.1" || !result.UpdateAvailable || result.Updated {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("Check made %d requests, want only the release metadata request", got)
	}
}

func TestApplyReplacesVerifiedBinary(t *testing.T) {
	server, _ := releaseServer(t, "new-binary", "v0.3.0", true, false)
	defer server.Close()

	target := filepath.Join(t.TempDir(), "memctl")
	if err := os.WriteFile(target, []byte("old-binary"), 0o750); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(context.Background(), Options{
		CurrentVersion: "v0.2.1",
		APIBaseURL:     server.URL,
		HTTPClient:     server.Client(),
		BinaryPath:     target,
		goos:           "darwin",
		goarch:         "arm64",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.BinaryPath != resolvedTarget || result.LatestVersion != "v0.3.0" {
		t.Fatalf("unexpected result: %+v", result)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new-binary" {
		t.Fatalf("binary content %q, want new-binary", content)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("mode %o, want 750", info.Mode().Perm())
	}
	assertNoTemporaryFiles(t, filepath.Dir(target))
}

func TestApplyLeavesBinaryOnChecksumMismatch(t *testing.T) {
	server, _ := releaseServer(t, "new-binary", "v0.3.0", true, true)
	defer server.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "memctl")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Apply(context.Background(), Options{
		CurrentVersion: "v0.2.1",
		APIBaseURL:     server.URL,
		HTTPClient:     server.Client(),
		BinaryPath:     target,
		goos:           "linux",
		goarch:         "amd64",
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Apply error = %v, want checksum mismatch", err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "old-binary" {
		t.Fatalf("binary was changed to %q", content)
	}
	assertNoTemporaryFiles(t, dir)
}

func TestApplyRequiresChecksumsAsset(t *testing.T) {
	server, _ := releaseServer(t, "new-binary", "v0.3.0", false, false)
	defer server.Close()

	target := filepath.Join(t.TempDir(), "memctl")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Apply(context.Background(), Options{
		CurrentVersion: "v0.2.1",
		APIBaseURL:     server.URL,
		HTTPClient:     server.Client(),
		BinaryPath:     target,
		goos:           "darwin",
		goarch:         "arm64",
	})
	if err == nil || !strings.Contains(err.Error(), checksumsAsset) {
		t.Fatalf("Apply error = %v, want a missing checksums error", err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "old-binary" {
		t.Fatalf("binary was changed to %q", content)
	}
}

func TestApplyUnsupportedPlatform(t *testing.T) {
	server, _ := releaseServer(t, "new-binary", "v0.3.0", true, false)
	defer server.Close()

	_, err := Apply(context.Background(), Options{
		CurrentVersion: "v0.2.1",
		APIBaseURL:     server.URL,
		HTTPClient:     server.Client(),
		goos:           "linux",
		goarch:         "arm64",
	})
	if err == nil || !strings.Contains(err.Error(), "not supported on linux/arm64") {
		t.Fatalf("Apply error = %v, want unsupported platform", err)
	}
}

func TestApplyWindowsIsUnsupported(t *testing.T) {
	server, _ := releaseServer(t, "new-binary", "v0.3.0", true, false)
	defer server.Close()

	_, err := Apply(context.Background(), Options{
		CurrentVersion: "v0.2.1",
		APIBaseURL:     server.URL,
		HTTPClient:     server.Client(),
		goos:           "windows",
		goarch:         "amd64",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot replace a running executable on windows") {
		t.Fatalf("Apply error = %v, want windows unsupported", err)
	}
}

func TestApplyNoopWhenCurrent(t *testing.T) {
	server, requests := releaseServer(t, "new-binary", "v0.2.1", true, false)
	defer server.Close()

	target := filepath.Join(t.TempDir(), "memctl")
	if err := os.WriteFile(target, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), Options{
		CurrentVersion: "v0.2.1",
		APIBaseURL:     server.URL,
		HTTPClient:     server.Client(),
		BinaryPath:     target,
		goos:           "darwin",
		goarch:         "arm64",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Updated || result.UpdateAvailable {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("Apply made %d requests, want only the metadata request", got)
	}
	content, _ := os.ReadFile(target)
	if string(content) != "current" {
		t.Fatalf("binary was changed to %q", content)
	}
}

func TestCheckMissingRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := Check(context.Background(), Options{
		CurrentVersion: "v0.2.1",
		APIBaseURL:     server.URL,
		HTTPClient:     server.Client(),
	})
	if err == nil || !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Fatalf("Check error = %v, want NOT_FOUND", err)
	}
}

// releaseServer serves a latest-release document, SHA256SUMS and the binary.
// The checksum of the binary is intentionally wrong for the mismatch test when
// the current platform is linux/amd64.
func releaseServer(t *testing.T, binary, tag string, includeChecksums, wrongChecksum bool) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var requests atomic.Int64
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	serverURL := server.URL

	mux.HandleFunc("/repos/"+Repository+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assets := fmt.Sprintf(`{"name":"memctl-darwin-arm64","browser_download_url":%q},{"name":"memctl-linux-amd64","browser_download_url":%q},{"name":"memctl-windows-amd64.exe","browser_download_url":%q}`,
			serverURL+"/memctl-darwin-arm64", serverURL+"/memctl-linux-amd64", serverURL+"/memctl-windows-amd64.exe")
		if includeChecksums {
			assets += fmt.Sprintf(`,{"name":%q,"browser_download_url":%q}`, checksumsAsset, serverURL+"/"+checksumsAsset)
		}
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[%s]}`, tag, assets)
	})
	mux.HandleFunc("/memctl-darwin-arm64", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, binary)
	})
	mux.HandleFunc("/memctl-linux-amd64", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, binary)
	})
	mux.HandleFunc("/"+checksumsAsset, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		content := binary
		if wrongChecksum {
			content = "other-binary"
		}
		sum := sha256.Sum256([]byte(content))
		fmt.Fprintf(w, "%s  memctl-darwin-arm64\n%s  memctl-linux-amd64\n",
			hex.EncodeToString(sum[:]), hex.EncodeToString(sum[:]))
	})
	return server, &requests
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".memctl-update-") {
			t.Fatalf("temporary update file %q was left behind", entry.Name())
		}
	}
}
