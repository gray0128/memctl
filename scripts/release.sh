#!/usr/bin/env bash
#
# Builds the cross-platform release candidates, their SHA-256 checksums, the
# third-party license notices and the release info file. Run through `make
# release`, which first executes the full test suite and the frontend build so
# web/dist is populated. See docs/04-ui-and-cli-specification.md section 6 and
# docs/05-validation-and-delivery.md sections 3 and 4.
#
# Cross-compiled targets are buildable, not runtime-verified. Only the platform
# recorded in the release info as runtime-verified (currently darwin/arm64) has
# lifecycle and isolation test results; see docs/validation/stage-d.md.

set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo v2.0.0-dev)}"
DIST="${DIST:-dist}"
LDFLAGS="-s -w -X main.version=$VERSION"

mkdir -p "$DIST"
rm -f "$DIST"/memctl-* "$DIST/SHA256SUMS" "$DIST/THIRD_PARTY_LICENSES.txt" "$DIST/RELEASE-INFO.txt"

# The release embeds web/dist, so refuse to produce candidates without a built
# frontend or with a stale one instead of shipping an old UI silently.
if [ ! -f web/dist/index.html ]; then
  printf 'error: web/dist is missing; run `make ui` or `make release` first\n' >&2
  exit 1
fi

# The host target is the only one this run can execute the test suite on, so it
# is the only line that may claim runtime verification.
case "$(uname -s)/$(uname -m)" in
  Darwin/arm64)  HOST_TARGET="darwin/arm64" ;;
  Darwin/x86_64) HOST_TARGET="darwin/amd64" ;;
  Linux/x86_64)  HOST_TARGET="linux/amd64" ;;
  Linux/aarch64) HOST_TARGET="linux/arm64" ;;
  *)             HOST_TARGET="$(uname -s)/$(uname -m)" ;;
esac

host_verification() {
  if [ "$1" = "$HOST_TARGET" ]; then
    printf 'runtime-verified on this build host'
  else
    printf 'cross-compiled only, not runtime-verified by this build host'
  fi
}

build() {
  local goos="$1" goarch="$2" out="$3"
  printf 'build %s/%s -> %s/%s\n' "$goos" "$goarch" "$DIST" "$out"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$DIST/$out" ./cmd/memctl
}

build darwin arm64 memctl-darwin-arm64
build darwin amd64 memctl-darwin-amd64
build linux amd64 memctl-linux-amd64
build windows amd64 memctl-windows-amd64.exe

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

# Checksums cover the four candidate binaries, not the metadata files.
( cd "$DIST" && sha256 memctl-* > SHA256SUMS )
printf 'wrote %s/SHA256SUMS\n' "$DIST"

license_file() {
  local dir="$1" candidate
  for candidate in LICENSE LICENSE.txt LICENSE.md LICENCE COPYING; do
    if [ -f "$dir/$candidate" ]; then
      printf '%s' "$dir/$candidate"
      return 0
    fi
  done
  return 1
}

missing_licenses=0
# Capture the module graph in a plain assignment so a `go list` failure aborts
# the release instead of silently producing a license file with no Go entries.
go_modules="$(go list -m -f '{{.Path}}|{{.Version}}|{{.Dir}}' all)"
{
  printf 'memctl third-party license notices\n'
  printf 'version: %s\n' "$VERSION"
  printf '\n'
  printf '%s\n' "$(printf '=%.0s' {1..80})"
  printf 'Go module dependencies listed by `go list -m all` (full license text)\n'
  printf '%s\n\n' "$(printf '=%.0s' {1..80})"
  while IFS='|' read -r path version dir; do
    [ -n "$version" ] || continue
    printf '%s\n' "$(printf -- '-%.0s' {1..80})"
    printf '%s %s\n' "$path" "$version"
    printf '%s\n\n' "$(printf -- '-%.0s' {1..80})"
    if file="$(license_file "$dir")"; then
      cat "$file"
    else
      printf 'LICENSE TEXT NOT FOUND\n'
      missing_licenses=$((missing_licenses + 1))
    fi
    printf '\n'
  done <<<"$go_modules"
  printf '\n'
} > "$DIST/THIRD_PARTY_LICENSES.txt"

node web/scripts/npm-licenses.mjs >> "$DIST/THIRD_PARTY_LICENSES.txt"
printf 'wrote %s/THIRD_PARTY_LICENSES.txt\n' "$DIST"
if [ "$missing_licenses" -gt 0 ]; then
  printf 'warning: %d Go module(s) had no license file; see %s/THIRD_PARTY_LICENSES.txt\n' "$missing_licenses" "$DIST" >&2
fi

{
  printf 'memctl release candidate\n'
  printf 'version: %s\n' "$VERSION"
  printf 'commit: %s\n' "$(git rev-parse HEAD 2>/dev/null || echo unknown)"
  printf 'go: %s\n' "$(go version)"
  printf 'node: %s\n' "$(node --version 2>/dev/null || echo unavailable)"
  printf 'npm: %s\n' "$(npm --version 2>/dev/null || echo unavailable)"
  printf 'git: %s\n' "$(git --version)"
  printf 'generated: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '\n'
  printf 'targets (cross-compiled; only the build host can be runtime-verified here):\n'
  printf '  darwin/arm64    memctl-darwin-arm64       %s\n' "$(host_verification darwin/arm64)"
  printf '  darwin/amd64    memctl-darwin-amd64       %s\n' "$(host_verification darwin/amd64)"
  printf '  linux/amd64     memctl-linux-amd64        %s\n' "$(host_verification linux/amd64)"
  printf '  windows/amd64   memctl-windows-amd64.exe  cross-compiled only, not runnable on this build host\n'
  printf '  build host:     %s\n' "$HOST_TARGET"
  printf '\n'
  printf 'notes:\n'
  printf '  - embedded assets: default instructions, adapter templates and web/dist UI\n'
  printf '  - runtime dependency: system Git; Dreaming also needs a runner and a verified sandbox\n'
  printf '  - no external CDN: UI assets are served from the binary\n'
} > "$DIST/RELEASE-INFO.txt"
printf 'wrote %s/RELEASE-INFO.txt\n' "$DIST"

printf 'release %s ready in %s/\n' "$VERSION" "$DIST"
