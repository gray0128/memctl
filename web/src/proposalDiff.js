// Fixed-diff helpers for the 整理 view.
//
// docs/02-domain-models-and-contracts.md section 2.3 gives every change its own
// single-file patch, so the review view renders one block per file instead of
// one wall of text built from the concatenated `diff`. Memory entries are long
// single lines, which a bare horizontal-scrolling block cuts off mid-sentence
// and makes the pending change unreadable.
// Counting, grouping and rendering stay free of component state so they are
// unit tested without a browser.

import { DIFF_HTML_OPTIONS, diffHtmlRenderer } from './diff.js'

// patchCounts totals the added and removed lines of one single-file patch. The
// `---`/`+++` file headers and the "\ No newline at end of file" marker are not
// content lines.
export function patchCounts(patch) {
  const counts = { additions: 0, deletions: 0 }
  for (const line of String(patch || '').split('\n')) {
    if (line.startsWith('+++') || line.startsWith('---')) continue
    if (line.startsWith('+')) counts.additions += 1
    else if (line.startsWith('-')) counts.deletions += 1
  }
  return counts
}

// proposalDiffFiles lists the renderable changes in proposal order. A change
// without a rendered patch carries nothing to review, so it is left to the
// ordered change summary instead of producing an empty block.
export function proposalDiffFiles(proposal) {
  const changes = (proposal && proposal.changes) || []
  return changes
    .filter(
      (change) => change && change.path && typeof change.patch === 'string' && change.patch.trim() !== '',
    )
    .map((change) => ({
      path: change.path,
      action: change.action,
      patch: change.patch,
      ...patchCounts(change.patch),
    }))
}

// proposalDiffTotals totals the per-file counts for the summary line.
export function proposalDiffTotals(files) {
  return (files || []).reduce(
    (totals, file) => {
      totals.files += 1
      totals.additions += file.additions || 0
      totals.deletions += file.deletions || 0
      return totals
    },
    { files: 0, additions: 0, deletions: 0 },
  )
}

// renderProposalDiff returns one html block per file. Diff2Html escapes the
// memory text, so the patch is rendered as untrusted content per
// docs/04-ui-and-cli-specification.md section 4. An unavailable renderer
// returns no blocks and the caller falls back to the change summary.
export async function renderProposalDiff(files) {
  if (!files || files.length === 0) return []
  try {
    const render = await diffHtmlRenderer()
    return files.map((file) => ({ ...file, html: render(file.patch, DIFF_HTML_OPTIONS) }))
  } catch {
    return []
  }
}
