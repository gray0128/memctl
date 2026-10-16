// Documented JSON API client.
//
// The Go server injects the session token and the read-only flag into the
// index page as meta tags. Every API request, including GET and the SSE
// stream, carries the token in X-Memctl-Token; it is never placed in a URL,
// logged or stored in localStorage. See
// docs/04-ui-and-cli-specification.md section 5.

import { buildQuery } from './queries.js'
import { encodeFilePath, ifMatchHeader, putFileHeaders } from './writes.js'

function metaContent(name) {
  if (typeof document === 'undefined') return ''
  const element = document.querySelector(`meta[name="${name}"]`)
  return element ? element.getAttribute('content') || '' : ''
}

async function toApiError(response) {
  let code = 'IO_FAILED'
  let message = `request failed with status ${response.status}`
  try {
    const body = await response.json()
    if (body && body.error) {
      if (body.error.code) code = body.error.code
      if (body.error.message) message = body.error.message
    }
  } catch {
    // The error envelope is JSON by contract; a non-JSON body is not expected.
  }
  const error = new Error(message)
  error.code = code
  error.status = response.status
  return error
}

export function createApi(options = {}) {
  const fetchImpl = options.fetchImpl || globalThis.fetch
  const token = options.token !== undefined ? options.token : metaContent('memctl-token')
  const readOnly =
    options.readOnly !== undefined ? options.readOnly : metaContent('memctl-readonly') === 'true'

  async function request(path, init = {}) {
    const headers = { Accept: 'application/json', ...(init.headers || {}), 'X-Memctl-Token': token }
    if (init.body !== undefined) headers['Content-Type'] = 'application/json'
    const response = await fetchImpl(path, { ...init, headers })
    if (!response.ok) throw await toApiError(response)
    if (response.status === 204) return null
    return response.json()
  }

  function writeJSON(path, method, payload) {
    return request(path, { method, body: JSON.stringify(payload) })
  }

  return {
    token,
    readOnly,
    snapshot: (signal) => request('/api/v1/snapshot', { signal }),
    file: (path, signal) => request(`/api/v1/files/${encodeFilePath(path)}`, { signal }),
    entries: (params, signal) => request(`/api/v1/entries${buildQuery(params)}`, { signal }),
    audit: (params, signal) => request(`/api/v1/audit${buildQuery(params)}`, { signal }),
    auditDiff: (hash, signal) =>
      request(`/api/v1/audit/${encodeURIComponent(hash)}/diff`, { signal }),
    doctor: (signal) => request('/api/v1/doctor', { signal }),

    // Managed writes. Every call reuses the file revision or repository
    // baseRevision the caller holds, so the CLI and the UI share one version
    // protocol and one repository lock.
    putFile: (path, rawContent, fileRevision) =>
      request(`/api/v1/files/${encodeFilePath(path)}`, {
        method: 'PUT',
        headers: putFileHeaders(fileRevision),
        body: JSON.stringify({ rawContent }),
      }),
    moveFile: (payload) => writeJSON('/api/v1/files:move', 'POST', payload),
    createEntry: (payload) => writeJSON('/api/v1/entries', 'POST', payload),
    patchEntry: (id, payload, fileRevision) =>
      request(`/api/v1/entries/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        headers: ifMatchHeader(fileRevision),
        body: JSON.stringify(payload),
      }),
    deleteEntry: (id, filePath, fileRevision) =>
      request(`/api/v1/entries/${encodeURIComponent(id)}${buildQuery({ filePath })}`, {
        method: 'DELETE',
        headers: ifMatchHeader(fileRevision),
      }),
    doctorFix: (payload) => writeJSON('/api/v1/doctor:fix', 'POST', payload),
    sync: () => request('/api/v1/sync', { method: 'POST' }),
  }
}
