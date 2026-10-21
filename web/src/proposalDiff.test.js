import { describe, expect, it } from 'vitest'
import {
  patchCounts,
  proposalDiffFiles,
  proposalDiffTotals,
  renderProposalDiff,
} from './proposalDiff.js'

const updatePatch =
  '--- a/MEMORY.md\n+++ b/MEMORY.md\n@@ -14,3 +14,4 @@\n context line\n-not kept\n+- [[notes/memctl-skill-install]] — 用 skills CLI 在线安装配套 Skill\n\\ No newline at end of file\n'

const createPatch = '--- /dev/null\n+++ b/notes/new.md\n@@ -0,0 +1,2 @@\n+# Title\n+\n'

const proposal = {
  proposalId: 'dp-abc',
  changes: [
    { path: 'MEMORY.md', action: 'update', patch: updatePatch },
    { path: 'notes/no-patch.md', action: 'update' },
    { path: 'notes/new.md', action: 'create', patch: createPatch },
  ],
}

describe('proposal diff helpers', () => {
  it('counts content lines without the file headers or the newline marker', () => {
    expect(patchCounts(updatePatch)).toEqual({ additions: 1, deletions: 1 })
    expect(patchCounts(createPatch)).toEqual({ additions: 2, deletions: 0 })
    expect(patchCounts('')).toEqual({ additions: 0, deletions: 0 })
    expect(patchCounts(null)).toEqual({ additions: 0, deletions: 0 })
  })

  it('keeps proposal order and skips changes without a patch', () => {
    const files = proposalDiffFiles(proposal)
    expect(files.map((file) => file.path)).toEqual(['MEMORY.md', 'notes/new.md'])
    expect(files[0]).toMatchObject({ action: 'update', additions: 1, deletions: 1 })
    expect(files[1]).toMatchObject({ action: 'create', additions: 2, deletions: 0 })
    expect(proposalDiffFiles(null)).toEqual([])
    expect(proposalDiffFiles({ changes: [{ path: 'x.md', action: 'update', patch: '  \n' }] })).toEqual([])
  })

  it('totals the per-file counts', () => {
    expect(proposalDiffTotals(proposalDiffFiles(proposal))).toEqual({
      files: 2,
      additions: 3,
      deletions: 1,
    })
    expect(proposalDiffTotals([])).toEqual({ files: 0, additions: 0, deletions: 0 })
  })

  it('renders one escaped html block per file', async () => {
    const rendered = await renderProposalDiff(proposalDiffFiles(proposal))
    expect(rendered).toHaveLength(2)
    expect(rendered[0].html).toContain('MEMORY.md')
    expect(rendered[1].html).toContain('notes/new.md')
    await expect(renderProposalDiff([])).resolves.toEqual([])
  })
})
