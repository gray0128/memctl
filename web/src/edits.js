// Draft-preserving edit helpers for the entry browser and the file editor.
//
// docs/04-ui-and-cli-specification.md section 4 requires a version conflict to
// keep the unsaved draft and show the current content next to it instead of
// overwriting. This module detects that conflict from the documented error
// envelope and computes a bounded line diff so the view can render both sides.

// isRevisionConflict reports the 412 REVISION_MISMATCH contract error that a
// stale If-Match or baseRevision produces.
export function isRevisionConflict(error) {
  return Boolean(error) && error.code === 'REVISION_MISMATCH'
}

// MAX_DIFF_LINES bounds the O(n*m) line comparison. Larger files fall back to a
// coarse position-based diff so the browser never allocates a huge table.
const MAX_DIFF_LINES = 800

function splitLines(text) {
  if (text === '') return []
  return String(text).replace(/\r\n/g, '\n').split('\n')
}

// lineAt returns the 1-based line of a text, or an empty string when the line
// number is outside the file. It is used to show the server's current entry
// line after a conflict.
export function lineAt(text, lineNumber) {
  const lines = splitLines(text)
  if (lineNumber < 1 || lineNumber > lines.length) return ''
  return lines[lineNumber - 1]
}

// diffLines returns an ordered list of {type, text} entries with type
// `context`, `removed` or `added`. Longest-common-subsequence alignment keeps
// unchanged lines together, so a conflict view shows which bytes the draft
// would change.
export function diffLines(currentText, draftText) {
  const current = splitLines(currentText)
  const draft = splitLines(draftText)
  if (current.length > MAX_DIFF_LINES || draft.length > MAX_DIFF_LINES) {
    return coarseDiff(current, draft)
  }
  const rows = current.length + 1
  const cols = draft.length + 1
  const lengths = new Uint32Array(rows * cols)
  for (let i = current.length - 1; i >= 0; i--) {
    for (let j = draft.length - 1; j >= 0; j--) {
      lengths[i * cols + j] =
        current[i] === draft[j]
          ? lengths[(i + 1) * cols + (j + 1)] + 1
          : Math.max(lengths[(i + 1) * cols + j], lengths[i * cols + (j + 1)])
    }
  }
  const result = []
  let i = 0
  let j = 0
  while (i < current.length && j < draft.length) {
    if (current[i] === draft[j]) {
      result.push({ type: 'context', text: current[i] })
      i++
      j++
    } else if (lengths[(i + 1) * cols + j] >= lengths[i * cols + (j + 1)]) {
      result.push({ type: 'removed', text: current[i] })
      i++
    } else {
      result.push({ type: 'added', text: draft[j] })
      j++
    }
  }
  while (i < current.length) result.push({ type: 'removed', text: current[i++] })
  while (j < draft.length) result.push({ type: 'added', text: draft[j++] })
  return result
}

// coarseDiff compares both sides by position. It is only used for files above
// MAX_DIFF_LINES, where an exact alignment would allocate a large table.
function coarseDiff(current, draft) {
  const result = []
  const length = Math.max(current.length, draft.length)
  for (let index = 0; index < length; index++) {
    const left = current[index]
    const right = draft[index]
    if (left === right) {
      if (left !== undefined) result.push({ type: 'context', text: left })
    } else {
      if (left !== undefined) result.push({ type: 'removed', text: left })
      if (right !== undefined) result.push({ type: 'added', text: right })
    }
  }
  return result
}

const METADATA_KEY = /^[A-Za-z0-9_.-]+$/

function isMetadataGroup(content) {
  if (content === '' || content.includes('[') || content.includes(']')) return false
  return content.split(';').every((segment) => {
    const index = segment.indexOf(':')
    if (index < 0) return false
    const key = segment.slice(0, index).trim()
    return key !== '' && METADATA_KEY.test(key)
  })
}

// entrySuffix returns the exact trailing metadata bytes of a bullet body,
// mirroring vault.SplitFactAndMetadata so a fact edit keeps every metadata byte.
function entrySuffix(body) {
  const trimmed = body.replace(/[ \t]+$/, '')
  let index = trimmed.length
  while (index > 0 && trimmed[index - 1] === ']') {
    const open = trimmed.lastIndexOf('[', index - 2)
    if (open < 0) break
    if (!isMetadataGroup(trimmed.slice(open + 1, index - 1))) break
    index = open
    while (index > 0 && (trimmed[index - 1] === ' ' || trimmed[index - 1] === '\t')) index--
  }
  return body.slice(index)
}

// replaceEntryFactLine mirrors vault.ReplaceEntryFact: it rewrites the fact of
// one bullet line and keeps the trailing metadata suffix. A non-bullet line is
// returned unchanged.
export function replaceEntryFactLine(rawLine, factText) {
  const text = String(rawLine)
  if (text.length < 2 || text[0] !== '-' || (text[1] !== ' ' && text[1] !== '\t')) {
    return text
  }
  return text.slice(0, 2) + String(factText).trim() + entrySuffix(text.slice(2))
}

// matchCurrentEntry locates the entry a stale draft refers to. entryId binds the
// file revision, so after an external write the editor must find the entry by
// its fact text (preferring the same line) and retry with the new id. It returns
// null when the entry no longer exists, so the caller can ask for a rebuild
// instead of sending a request that can only fail. See
// docs/02-domain-models-and-contracts.md section 1.1.
export function matchCurrentEntry(entries, stale) {
  if (!stale) return null
  const list = entries || []
  const exact = list.find(
    (entry) => entry.lineNumber === stale.lineNumber && entry.factText === stale.factText,
  )
  if (exact) return exact
  const sameFact = list.filter((entry) => entry.factText === stale.factText)
  return sameFact.length > 0 ? sameFact[0] : null
}
