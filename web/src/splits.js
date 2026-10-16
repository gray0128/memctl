// Split path suggestion for the knowledge-graph action that moves one entry
// into a new file, per docs/04-ui-and-cli-specification.md section 4.
//
// The suggestion is only a default for the confirmation field. The operator can
// always replace it before the transaction runs, and the server still enforces
// the unified path boundary.

const MAX_SLUG = 48

// slugFromFact turns a fact text into a file-name component. Letters and
// numbers from any script are kept (so Chinese facts produce readable names),
// every other run becomes one dash.
export function slugFromFact(factText) {
  return String(factText)
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, MAX_SLUG)
    .replace(/-+$/g, '')
}

// suggestSplitPath returns a new Markdown path in the source file's directory.
// An empty slug falls back to the entry line number, so the suggestion is
// always a usable file name.
export function suggestSplitPath(sourcePath, entry) {
  const source = String(sourcePath || '')
  const slash = source.lastIndexOf('/')
  const directory = slash < 0 ? '' : source.slice(0, slash + 1)
  const fact = entry && entry.factText ? entry.factText : ''
  const lineNumber = (entry && entry.lineNumber) || 1
  const slug = slugFromFact(fact) || `entry-${lineNumber}`
  return `${directory}${slug}.md`
}
