// Query-string builder shared by the entry browser and the audit view. Empty
// and whitespace-only filters are omitted, so the server sees only the filters
// the operator set.

export function buildQuery(params) {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params || {})) {
    if (value === undefined || value === null) continue
    const text = String(value).trim()
    if (text === '') continue
    search.set(key, text)
  }
  const query = search.toString()
  return query ? `?${query}` : ''
}
