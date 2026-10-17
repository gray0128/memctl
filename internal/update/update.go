// Package update implements `memctl update`: it queries the latest GitHub
// release, verifies the platform binary against the release checksums, and
// atomically replaces the running executable. See
// docs/04-ui-and-cli-specification.md section 1.2.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/memerr"
)

const (
	// DefaultAPIBaseURL is the unauthenticated GitHub REST API. The release
	// repository is fixed so an update cannot be redirected to another host by
	// an argument or environment variable.
	DefaultAPIBaseURL = "https://api.github.com"
	// Repository is the GitHub repository that publishes memctl releases.
	Repository = "gray0128/memctl"

	checksumsAsset    = "SHA256SUMS"
	requestTimeout    = 5 * time.Minute
	maxReleaseBytes   = 1 << 20
	maxChecksumsBytes = 1 << 20
	maxBinaryBytes    = 256 << 20
)

// Options configures one update check or application. The zero value uses the
// public GitHub API, the running executable and the host platform.
type Options struct {
	CurrentVersion string
	APIBaseURL     string
	HTTPClient     *http.Client
	// BinaryPath overrides the executable to replace. It exists so tests can
	// target a temporary file; production callers leave it empty.
	BinaryPath string

	// goos and goarch override runtime.GOOS and runtime.GOARCH in tests.
	goos, goarch string
}

// Result reports the outcome of a check or update.
type Result struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	Updated         bool
	BinaryPath      string
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

func (r release) asset(name string) (string, bool) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset.URL, true
		}
	}
	return "", false
}

// Check reports whether a newer release exists without writing any file.
func Check(ctx context.Context, opts Options) (Result, error) {
	latest, _, err := fetchLatest(ctx, opts)
	if err != nil {
		return Result{}, err
	}
	return resultFor(opts, latest), nil
}

// Apply downloads and replaces the running executable when a newer release
// exists. A checksum mismatch, a missing asset or an unsupported platform
// leaves the current executable untouched.
func Apply(ctx context.Context, opts Options) (Result, error) {
	latest, rel, err := fetchLatest(ctx, opts)
	if err != nil {
		return Result{}, err
	}
	result := resultFor(opts, latest)
	if !result.UpdateAvailable {
		return result, nil
	}

	goos, goarch := platform(opts)
	name, ok := assetName(goos, goarch)
	if !ok {
		return Result{}, memerr.New(memerr.Unsupported,
			"self-update is not supported on %s/%s; supported targets are darwin/arm64, darwin/amd64, linux/amd64 and windows/amd64",
			goos, goarch)
	}
	if goos == "windows" {
		return Result{}, memerr.New(memerr.Unsupported,
			"self-update cannot replace a running executable on windows; download %s from https://github.com/%s/releases/tag/%s",
			name, Repository, latest)
	}

	binaryURL, ok := rel.asset(name)
	if !ok {
		return Result{}, memerr.New(memerr.NotFound, "release %s has no asset %q", latest, name)
	}
	checksumsURL, ok := rel.asset(checksumsAsset)
	if !ok {
		return Result{}, memerr.New(memerr.NotFound, "release %s has no asset %q; refusing to replace the binary without a checksum", latest, checksumsAsset)
	}

	checksums, err := fetchBytes(ctx, opts, checksumsURL, maxChecksumsBytes)
	if err != nil {
		return Result{}, err
	}
	expected, ok := checksumFor(checksums, name)
	if !ok {
		return Result{}, memerr.New(memerr.IOFailed, "release %s checksums do not contain %q", latest, name)
	}

	target, err := executablePath(opts.BinaryPath)
	if err != nil {
		return Result{}, err
	}
	if err := replaceExecutable(ctx, opts, binaryURL, target, name, expected); err != nil {
		return Result{}, err
	}

	result.Updated = true
	result.BinaryPath = target
	return result, nil
}

func resultFor(opts Options, latest string) Result {
	current := strings.TrimSpace(opts.CurrentVersion)
	return Result{
		CurrentVersion:  current,
		LatestVersion:   latest,
		UpdateAvailable: updateAvailable(current, latest),
	}
}

// updateAvailable treats a non-release development build as older so local
// builds can move to the latest published release. Release versions compare
// numerically; an equal or newer current version needs no update.
func updateAvailable(current, latest string) bool {
	if current == latest {
		return false
	}
	if current == "" {
		return true
	}
	currentVersion, currentOK := parseReleaseVersion(current)
	if !currentOK {
		return true
	}
	latestVersion, latestOK := parseReleaseVersion(latest)
	if !latestOK {
		return false
	}
	return compareVersions(currentVersion, latestVersion) < 0
}

type releaseVersion [3]int

func parseReleaseVersion(value string) (releaseVersion, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return releaseVersion{}, false
	}
	var parsed releaseVersion
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return releaseVersion{}, false
		}
		parsed[index] = number
	}
	return parsed, true
}

func compareVersions(left, right releaseVersion) int {
	for index := range left {
		switch {
		case left[index] < right[index]:
			return -1
		case left[index] > right[index]:
			return 1
		}
	}
	return 0
}

func platform(opts Options) (string, string) {
	goos, goarch := opts.goos, opts.goarch
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return goos, goarch
}

// assetName maps a platform to the release candidate name produced by
// scripts/release.sh.
func assetName(goos, goarch string) (string, bool) {
	switch goos + "/" + goarch {
	case "darwin/arm64":
		return "memctl-darwin-arm64", true
	case "darwin/amd64":
		return "memctl-darwin-amd64", true
	case "linux/amd64":
		return "memctl-linux-amd64", true
	case "windows/amd64":
		return "memctl-windows-amd64.exe", true
	default:
		return "", false
	}
}

func fetchLatest(ctx context.Context, opts Options) (string, release, error) {
	base := strings.TrimRight(strings.TrimSpace(opts.APIBaseURL), "/")
	if base == "" {
		base = DefaultAPIBaseURL
	}
	endpoint := base + "/repos/" + Repository + "/releases/latest"
	data, err := fetchBytes(ctx, opts, endpoint, maxReleaseBytes)
	if err != nil {
		return "", release{}, err
	}

	var rel release
	if err := json.Unmarshal(data, &rel); err != nil {
		return "", release{}, memerr.Wrap(memerr.IOFailed, err, "cannot parse release metadata")
	}
	latest := strings.TrimSpace(rel.TagName)
	if latest == "" {
		return "", release{}, memerr.New(memerr.IOFailed, "release metadata has no tag_name")
	}
	if _, ok := parseReleaseVersion(latest); !ok {
		return "", release{}, memerr.New(memerr.IOFailed, "latest release tag %q is not a release version", latest)
	}
	return latest, rel, nil
}

func httpClient(opts Options) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	return &http.Client{Timeout: requestTimeout}
}

func newRequest(ctx context.Context, opts Options, url string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, memerr.Wrap(memerr.InvalidArgument, err, "cannot build update request")
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	userAgent := "memctl-update"
	if version := strings.TrimSpace(opts.CurrentVersion); version != "" {
		userAgent += "/" + version
	}
	request.Header.Set("User-Agent", userAgent)
	return request, nil
}

func fetchBytes(ctx context.Context, opts Options, url string, limit int64) ([]byte, error) {
	response, err := doRequest(ctx, opts, url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, memerr.Wrap(memerr.NetworkUnavailable, err, "cannot read update response")
	}
	if int64(len(data)) > limit {
		return nil, memerr.New(memerr.LimitExceeded, "update response exceeds the %d byte limit", limit)
	}
	return data, nil
}

func doRequest(ctx context.Context, opts Options, url string) (*http.Response, error) {
	request, err := newRequest(ctx, opts, url)
	if err != nil {
		return nil, err
	}
	response, err := httpClient(opts).Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, memerr.Wrap(memerr.Timeout, err, "update request timed out")
		}
		return nil, memerr.Wrap(memerr.NetworkUnavailable, err, "cannot reach the release server")
	}
	if response.StatusCode == http.StatusNotFound {
		response.Body.Close()
		return nil, memerr.New(memerr.NotFound, "release server returned 404 for %s", url)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, memerr.New(memerr.NetworkUnavailable, "release server returned %s for %s", response.Status, url)
	}
	return response, nil
}

// checksumFor finds one file in a sha256sum-style checksum list.
func checksumFor(contents []byte, name string) (string, bool) {
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != sha256.Size*2 {
			continue
		}
		if _, err := hex.DecodeString(sum); err != nil {
			continue
		}
		return sum, true
	}
	return "", false
}

func executablePath(override string) (string, error) {
	target := override
	if target == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", memerr.Wrap(memerr.IOFailed, err, "cannot locate the running executable")
		}
		target = executable
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", memerr.Wrap(memerr.IOFailed, err, "cannot resolve %q", target)
	}
	return absolute, nil
}

func replaceExecutable(ctx context.Context, opts Options, url, target, asset, expected string) error {
	dir := filepath.Dir(target)
	temp, err := os.CreateTemp(dir, ".memctl-update-*")
	if err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create a temporary file next to %q", target)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		temp.Close()
		if !keep {
			os.Remove(tempPath)
		}
	}()

	sum, err := downloadTo(ctx, opts, url, temp)
	if err != nil {
		return err
	}
	if sum != expected {
		return memerr.New(memerr.IOFailed, "checksum mismatch for %q: got sha256:%s, want sha256:%s; the current binary was not changed", asset, sum, expected)
	}

	mode := os.FileMode(0o755)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
		if mode&0o100 == 0 {
			mode |= 0o755
		}
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return memerr.Wrap(memerr.IOFailed, err, "cannot set permissions on the new binary")
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return memerr.Wrap(memerr.IOFailed, err, "cannot flush the new binary")
	}
	if err := temp.Close(); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot close the new binary")
	}

	if err := os.Rename(tempPath, target); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot replace %q; check write permission on its directory", target)
	}
	keep = true
	syncDir(dir)
	return nil
}

func downloadTo(ctx context.Context, opts Options, url string, destination *os.File) (string, error) {
	response, err := doRequest(ctx, opts, url)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	hasher := sha256.New()
	target := &writeTracker{writer: destination}
	written, err := io.Copy(io.MultiWriter(target, hasher), io.LimitReader(response.Body, maxBinaryBytes+1))
	if err != nil {
		if target.err != nil {
			return "", memerr.Wrap(memerr.IOFailed, target.err, "cannot write the new binary")
		}
		return "", memerr.Wrap(memerr.NetworkUnavailable, err, "cannot download the release binary")
	}
	if written > maxBinaryBytes {
		return "", memerr.New(memerr.LimitExceeded, "release binary exceeds the %d byte limit", maxBinaryBytes)
	}
	if written == 0 {
		return "", memerr.New(memerr.IOFailed, "downloaded release binary is empty")
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// writeTracker remembers a destination write failure so a failed copy is not
// misreported as a network error.
type writeTracker struct {
	writer io.Writer
	err    error
}

func (w *writeTracker) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	if err != nil {
		w.err = err
	}
	return written, err
}

// syncDir makes the rename durable where the platform supports directory
// fsync; failure is not fatal because the rename already completed.
func syncDir(dir string) {
	handle, err := os.Open(dir)
	if err != nil {
		return
	}
	defer handle.Close()
	_ = handle.Sync()
}
