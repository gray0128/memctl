// Source-reference rendering rules for the entry browser.
//
// docs/04-ui-and-cli-specification.md section 4 allows navigation only for an
// explicit safe protocol and offers copy for `agent://`; every other value stays
// untrusted plain text. This module keeps that decision free of component state
// so it can be unit tested and reused by any view that shows a source.

const HTTPS_PATTERN = /^https:\/\//i
const AGENT_PATTERN = /^agent:\/\//i

// sourceProtocol classifies one raw source value.
export function sourceProtocol(raw) {
  const value = String(raw === undefined || raw === null ? '' : raw).trim()
  if (HTTPS_PATTERN.test(value)) return 'https'
  if (AGENT_PATTERN.test(value)) return 'agent'
  return 'text'
}
