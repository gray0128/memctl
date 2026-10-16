// Diff preview helpers for the read-only audit view.
//
// GET /api/v1/audit/{hash}/diff returns one FileDiff per changed file
// (docs/02-domain-models-and-contracts.md section 2.3). This module joins the
// text patches into one unified diff and renders them with Diff2Html, which is
// imported on demand so the library stays out of the initial bundle. The
// assembly and rendering logic stays free of component state so it is unit
// tested without a browser.

// buildUnifiedDiff concatenates the non-binary patches. Binary files carry no
// patch by contract, so they are reported separately instead of being rendered
// as empty hunks.
export function buildUnifiedDiff(files) {
  return (files || [])
    .filter((file) => !file.binary && file.patch)
    .map((file) => file.patch.replace(/\s+$/, '\n'))
    .join('\n')
}

// renderDiffHtml returns Diff2Html markup for the text patches, or an empty
// string when there is nothing to render. Diff2Html escapes code content, so
// memory text is rendered as untrusted text per
// docs/04-ui-and-cli-specification.md section 4.
export async function renderDiffHtml(files) {
  const text = buildUnifiedDiff(files)
  if (!text.trim()) return ''
  try {
    const { html } = await import('diff2html')
    return html(text, {
      drawFileList: false,
      matching: 'lines',
      outputFormat: 'line-by-line',
    })
  } catch {
    return ''
  }
}

// summarizeDiff totals the per-file counts and the binary file count.
export function summarizeDiff(files) {
  return (files || []).reduce(
    (accumulator, file) => {
      accumulator.files += 1
      accumulator.additions += file.additions || 0
      accumulator.deletions += file.deletions || 0
      if (file.binary) accumulator.binary += 1
      return accumulator
    },
    { files: 0, additions: 0, deletions: 0, binary: 0 },
  )
}
