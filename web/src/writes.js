// Request-shape helpers for the managed write routes of stage C-1.
//
// The REST contract of docs/02-domain-models-and-contracts.md section 4 uses a
// {path...} wildcard, a quoted If-Match file revision and the `absent` literal
// for creations. This module keeps that mapping out of the view components so
// it can be unit tested without a DOM.

// encodeFilePath escapes every path segment but keeps the `/` separators the
// {path...} wildcard expects. The server's unified path boundary still rejects
// traversal and Git-internal paths.
export function encodeFilePath(filePath) {
  return String(filePath)
    .split('/')
    .map((segment) => encodeURIComponent(segment))
    .join('/')
}

// isAbsent reports the documented literal for a path that does not exist yet.
// An empty value is treated as absent so a creation never sends an empty tag.
export function isAbsent(fileRevision) {
  const value =
    fileRevision === undefined || fileRevision === null ? '' : String(fileRevision).trim()
  return value === '' || value === 'absent'
}

// ifMatchHeader binds an existing file revision. The value is quoted because
// the server parses one quoted entity tag.
export function ifMatchHeader(fileRevision) {
  return { 'If-Match': `"${String(fileRevision).trim()}"` }
}

// putFileHeaders selects the creation precondition for an absent file and the
// revision precondition for an existing one, per REST section 4.1.
export function putFileHeaders(fileRevision) {
  if (isAbsent(fileRevision)) return { 'If-None-Match': '*' }
  return ifMatchHeader(fileRevision)
}
