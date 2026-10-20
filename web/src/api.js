// Documented JSON API client.
//
// The Go server injects the session token and the read-only flag into the
// index page as meta tags. Every API request, including GET and the SSE
// stream, carries the token in X-Memctl-Token; it is never placed in a URL,
// logged or stored in localStorage. See
// docs/04-ui-and-cli-specification.md section 5.

import { buildQuery } from './queries.js'
import { retryBusy } from './retry.js'
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
  // The Dreaming backend state is injected into the page like the read-only
  // flag: generation stays disabled until the repository has a confirmed
  // private remote and a managed Actions workflow pinned to this memctl
  // version. `dreamSandbox` keeps its historical name because the panel
  // contract already used it.
  const dreamSandbox =
    options.dreamSandbox !== undefined
      ? options.dreamSandbox
      : metaContent('memctl-dream-sandbox') === 'available'
  const dreamReason =
    options.dreamReason !== undefined ? options.dreamReason : metaContent('memctl-dream-reason')

  async function request(path, init = {}) {
    const send = async () => {
      const headers = { Accept: 'application/json', ...(init.headers || {}), 'X-Memctl-Token': token }
      if (init.body !== undefined) headers['Content-Type'] = 'application/json'
      const response = await fetchImpl(path, { ...init, headers })
      if (!response.ok) throw await toApiError(response)
      if (response.status === 204) return null
      return response.json()
    }
    // Reads are idempotent, so a BUSY answer races a lock held for a moment by
    // a sync or a managed write. Writes keep their single attempt: replaying a
    // mutation is the caller's decision, not the transport's.
    const method = (init.method || 'GET').toUpperCase()
    return method === 'GET' ? retryBusy(send) : send()
  }

  function writeJSON(path, method, payload) {
    return request(path, { method, body: JSON.stringify(payload) })
  }

  return {
    token,
    readOnly,
    dreamSandbox,
    dreamReason,
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

    // Adapter detection and install. The install request carries the config
    // revisions the preview returned so a file the user changed after the
    // preview is never overwritten. See
    // docs/02-domain-models-and-contracts.md section 4.2.
    adapters: (signal) => request('/api/v1/adapters', { signal }),
    // The MCP channel status backs the 客户端接入 view: it reports whether the
    // running binary serves the memory tools and the registration command of
    // each host. Read-only; it never edits host configuration.
    mcpStatus: (signal) => request('/api/v1/mcp', { signal }),
    installAdapter: (agent, payload) =>
      writeJSON(`/api/v1/adapters/${encodeURIComponent(agent)}/install`, 'POST', payload),

    // Conflict resolution and audit revert. Both reuse the same state
    // machine and repository baseline as the CLI, see
    // docs/02-domain-models-and-contracts.md section 4.2. GET
    // /api/v1/conflict answers 204 with no active conflict, which request()
    // maps to null.
    conflict: (signal) => request('/api/v1/conflict', { signal }),
    resolveConflict: (payload) => writeJSON('/api/v1/conflict:resolve', 'POST', payload),
    abortConflict: (payload) => writeJSON('/api/v1/conflict:abort', 'POST', payload),
    revertCommit: (hash, baseRevision) =>
      writeJSON(`/api/v1/audit/${encodeURIComponent(hash)}/revert`, 'POST', { baseRevision }),

    // Dreaming. Generation returns 202 with a proposal id; the view polls the
    // proposal until it is ready. Applying quotes the patchHash of the
    // reviewed proposal so a changed proposal can never be applied under a
    // stale hash. See docs/03-workflows-and-dataflows.md section 6.
    //
    // dreamList backs the proposal table of the 整理 view. It lists every
    // persisted proposal, including the `local-agent` ones produced by
    // `memctl dream --propose`, so a proposal from the current agent session is
    // reviewable without a remote or the Actions workflow. See
    // docs/04-ui-and-cli-specification.md section 4.
    dreamList: (limit, signal) => request(`/api/v1/dream${buildQuery({ limit })}`, { signal }),
    dreamPreview: (runner, baseRevision) =>
      writeJSON('/api/v1/dream:preview', 'POST', { runner, baseRevision: baseRevision || '' }),
    dreamProposal: (proposalId, signal) =>
      request(`/api/v1/dream/${encodeURIComponent(proposalId)}`, { signal }),
    dreamApply: (payload) => writeJSON('/api/v1/dream:apply', 'POST', payload),
  }
}
