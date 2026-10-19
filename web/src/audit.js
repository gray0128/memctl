// Audit view helpers: source resolution, the date grouping of the commit
// timeline and the cursor pager. They live outside the component so the
// rendering rules stay unit testable without a DOM.
// See docs/04-ui-and-cli-specification.md section 4.

import { formatTimestamp } from './time.js'

// sourceParts splits one commit's recorded provenance into labelled values.
// The audit view renders only what the commit actually recorded; a commit with
// no trailer is reported as unattributed instead of being guessed from the
// author or the subject.
export function sourceParts(commit) {
  if (!commit) return []
  const parts = []
  if (commit.agent) parts.push({ key: 'agent', label: 'Agent', value: commit.agent })
  if (commit.device) parts.push({ key: 'device', label: '设备', value: commit.device })
  if (commit.sessionId) parts.push({ key: 'session', label: '会话', value: commit.sessionId })
  return parts
}

export function hasSource(commit) {
  return sourceParts(commit).length > 0
}

// SOURCE_MISSING is the label of a commit whose message carries no provenance
// trailer: a root commit, or history written before memctl recorded trailers.
export const SOURCE_MISSING = '未记录来源'

// shortHash abbreviates one commit id for the card header.
export function shortHash(hash) {
  return hash ? hash.slice(0, 10) : ''
}

// displayTimestamp renders a commit timestamp in local time. The raw ISO value
// stays available as the hover title, so the exact instant and offset are not
// lost. See docs/04-ui-and-cli-specification.md section 4.
export function displayTimestamp(timestamp) {
  return formatTimestamp(timestamp)
}

// dayKey is the local calendar day a commit belongs to. `git log` returns the
// author time with an offset, and the timeline groups by the browser's local
// day so two commits around midnight are not split by their offsets.
export function dayKey(timestamp) {
  const date = new Date(timestamp)
  if (Number.isNaN(date.getTime())) return String(timestamp || '')
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${date.getFullYear()}-${month}-${day}`
}

// groupByDay turns one ordered commit list into contiguous day groups. Git
// returns newest first, so the list is never reordered.
export function groupByDay(commits) {
  const groups = []
  for (const commit of commits || []) {
    const key = dayKey(commit.timestamp)
    const last = groups[groups.length - 1]
    if (last && last.key === key) {
      last.commits.push(commit)
    } else {
      groups.push({ key, commits: [commit] })
    }
  }
  return groups
}

// PAGE_SIZE is the number of commits one audit page loads. It is smaller than
// the entry page because every commit card can expand into a Diff.
export const PAGE_SIZE = 20

// pageRequest builds the audit request of one page. Page 1 has no cursor, and
// every later page starts strictly older than the last commit of the previous
// page, which is the server's `before` contract.
export function pageRequest(page, pages) {
  const cursor = page > 1 ? (pages || [])[page - 2]?.nextCursor || '' : ''
  return cursor ? { limit: PAGE_SIZE, before: cursor } : { limit: PAGE_SIZE }
}

// pagerState describes the pager of the current page. The cursor API exposes no
// total, so the pager numbers only the pages actually visited plus the next
// one: `hasNext` is exactly the presence of a cursor on the current page.
export function pagerState(pages, page) {
  return {
    hasPrev: page > 1,
    hasNext: Boolean((pages || [])[page - 1]?.nextCursor),
    loaded: (pages || []).length,
  }
}

// pageNumbers lists the page buttons to render. Page 1 is always present and
// the button set grows as the operator walks forward.
export function pageNumbers(pages, page) {
  const count = Math.max((pages || []).length, page, 1)
  return Array.from({ length: count }, (_, index) => index + 1)
}
