import { describe, expect, it } from 'vitest'
import { buildUnifiedDiff, renderDiffHtml, summarizeDiff } from './diff.js'

// This file verifies the pure diff assembly and rendering of the read-only
// audit view: per-file patches from GET /api/v1/audit/{hash}/diff become one
// escaped Diff2Html document, and binary files stay out of the text diff.

const patch = [
  'diff --git a/alpha.md b/alpha.md',
  'index 1111111..2222222 100644',
  '--- a/alpha.md',
  '+++ b/alpha.md',
  '@@ -1,2 +1,3 @@',
  ' # Alpha',
  ' ',
  '-old line',
  '+new line',
  '+<script>alert(1)</script>',
  '',
].join('\n')

describe('diff rendering', () => {
  it('joins text patches and renders them with Diff2Html', async () => {
    const files = [
      { path: 'alpha.md', additions: 2, deletions: 1, patch, binary: false },
      { path: 'bin.dat', additions: 0, deletions: 0, patch: '', binary: true },
    ]

    expect(buildUnifiedDiff(files)).toContain('diff --git a/alpha.md b/alpha.md')
    const html = await renderDiffHtml(files)
    expect(html).toContain('d2h-file-wrapper')
    expect(html).toContain('alpha.md')
    // Memory text is untrusted and must not inject markup.
    expect(html).not.toContain('<script>alert(1)</script>')
  })

  it('returns an empty string when only binary files changed', async () => {
    const files = [{ path: 'bin.dat', additions: 0, deletions: 0, patch: '', binary: true }]
    expect(await renderDiffHtml(files)).toBe('')
  })

  it('totals counts and binary files', () => {
    const files = [
      { path: 'a.md', additions: 2, deletions: 1, patch: '', binary: false },
      { path: 'b.md', additions: 5, deletions: 0, patch: '', binary: false },
      { path: 'c.bin', additions: 0, deletions: 0, patch: '', binary: true },
    ]
    expect(summarizeDiff(files)).toEqual({ files: 3, additions: 7, deletions: 1, binary: 1 })
  })
})
