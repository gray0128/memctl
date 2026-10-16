// Documented JSON API client.
//
// The Go server injects the session token and the read-only flag into the
// index page as meta tags. Every API request, including GET and the SSE
// stream, carries the token in X-Memctl-Token; it is never placed in a URL,
// logged or stored in localStorage. See
// docs/04-ui-and-cli-specification.md section 5.

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
    const response = await fetchImpl(path, {
      ...init,
      headers: { Accept: 'application/json', ...(init.headers || {}), 'X-Memctl-Token': token },
    })
    if (!response.ok) throw await toApiError(response)
    if (response.status === 204) return null
    return response.json()
  }

  return {
    token,
    readOnly,
    snapshot: (signal) => request('/api/v1/snapshot', { signal }),
  }
}
