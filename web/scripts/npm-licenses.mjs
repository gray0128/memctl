// Emits the third-party license notices for the frontend dependencies.
//
// The release binary embeds web/dist, so every package whose code reaches the
// bundle is redistributed. Runtime dependencies (npm ls --omit=dev) get their
// full license text; build-only dependencies are listed by SPDX identifier
// only because they are not redistributed. Invoked by scripts/release.sh; see
// docs/04-ui-and-cli-specification.md section 6.
//
// Usage: node web/scripts/npm-licenses.mjs

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const webRoot = dirname(dirname(fileURLToPath(import.meta.url)));

function npmLs(omitDev) {
  const args = ['ls', '--all', '--json'];
  if (omitDev) args.push('--omit=dev');
  const raw = execFileSync('npm', args, {
    cwd: webRoot,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'ignore'],
    maxBuffer: 64 * 1024 * 1024,
  });
  return JSON.parse(raw);
}

// collect walks the npm dependency tree and returns unique name@version pairs.
function collect(tree, found) {
  for (const [name, node] of Object.entries(tree.dependencies ?? {})) {
    if (!node || !node.version) continue;
    found.set(`${name}@${node.version}`, { name, version: node.version });
    collect(node, found);
  }
  return found;
}

function packagePath(name) {
  const direct = join(webRoot, 'node_modules', ...name.split('/'));
  if (existsSync(join(direct, 'package.json'))) return direct;
  // Fall back to Node resolution for nested installs.
  const require = createRequire(join(webRoot, 'package.json'));
  try {
    return dirname(require.resolve(`${name}/package.json`));
  } catch {
    return '';
  }
}

function licenseFile(dir) {
  for (const candidate of ['LICENSE', 'LICENSE.md', 'LICENSE.txt', 'LICENCE', 'COPYING', 'license']) {
    const path = join(dir, candidate);
    if (existsSync(path)) return readFileSync(path, 'utf8').trimEnd();
  }
  return '';
}

function licenseId(dir) {
  try {
    const pkg = JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8'));
    if (typeof pkg.license === 'string') return pkg.license;
    if (pkg.license && typeof pkg.license.type === 'string') return pkg.license.type;
    if (Array.isArray(pkg.licenses)) return pkg.licenses.map((l) => l.type).join(', ');
  } catch {
    // fall through to unknown
  }
  return 'UNKNOWN';
}

const divider = '='.repeat(80);
const sub = '-'.repeat(80);

function section(title, packages, fullText) {
  const lines = [divider, title, divider, ''];
  for (const { name, version } of packages) {
    const dir = packagePath(name);
    lines.push(sub, `${name} ${version} (${dir ? licenseId(dir) : 'UNKNOWN'})`, sub, '');
    if (!dir) {
      lines.push('LICENSE TEXT NOT FOUND');
    } else if (fullText) {
      const text = licenseFile(dir);
      lines.push(text || 'LICENSE TEXT NOT FOUND');
    } else {
      lines.push(`License: ${licenseId(dir)} (build-only dependency, not redistributed)`);
    }
    lines.push('');
  }
  return lines.join('\n');
}

const runtime = [...collect(npmLs(true), new Map()).values()].sort((a, b) =>
  a.name === b.name ? a.version.localeCompare(b.version) : a.name.localeCompare(b.name),
);
const all = [...collect(npmLs(false), new Map()).values()].sort((a, b) =>
  a.name === b.name ? a.version.localeCompare(b.version) : a.name.localeCompare(b.name),
);
const runtimeKeys = new Set(runtime.map((p) => `${p.name}@${p.version}`));
const buildOnly = all.filter((p) => !runtimeKeys.has(`${p.name}@${p.version}`));

process.stdout.write(
  section('Frontend runtime dependencies bundled into the release binary (full license text)', runtime, true) +
    '\n' +
    section('Frontend build-only dependencies (not bundled, SPDX identifier only)', buildOnly, false),
);
