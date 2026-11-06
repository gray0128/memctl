#!/usr/bin/env bash
#
# Publishes each plugin package whose package.json version is not on npm yet.
# The release workflow calls this on a v* tag with npm trusted publishing
# (OIDC); no long-lived npm token is stored in the repository. A version that is
# already published is skipped, so rerunning a failed release publishes only
# what is missing.
#
# Trusted publishing requires npm >= 11.5.1, Node >= 22.14.0 and a Trusted
# Publisher entry on npmjs.com for each package: GitHub user/org gray0128,
# repository memctl, workflow file build.yml. Do not set registry-url or
# NODE_AUTH_TOKEN in the workflow: an empty token line disables the OIDC
# exchange.
#
# Usage: scripts/publish-npm.sh [npm publish flags]
# See docs/04-ui-and-cli-specification.md section 6.1.

set -euo pipefail

cd "$(dirname "$0")/.."

for dir in plugins/pi plugins/dsh; do
  name="$(node -p "require('./$dir/package.json').name")"
  version="$(node -p "require('./$dir/package.json').version")"

  if npm view "$name@$version" version >/dev/null 2>&1; then
    printf '%s@%s is already published; skipping\n' "$name" "$version"
    continue
  fi

  printf 'publishing %s@%s\n' "$name" "$version"
  ( cd "$dir" && npm publish --access public "$@" )
done
