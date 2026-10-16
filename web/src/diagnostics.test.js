import { describe, expect, it } from 'vitest'
import { autofixableCodes, summarizeDiagnostics } from './diagnostics.js'

describe('summarizeDiagnostics', () => {
  it('groups issues by the documented severities and counts them', () => {
    const report = {
      issues: [
        { code: 'MISSING_LINK', severity: 'error' },
        { code: 'ORPHAN_FILE', severity: 'warning' },
        { code: 'ENTRY_BLOAT', severity: 'warning' },
        { code: 'MISSING_RECOMMENDED_METADATA', severity: 'info' },
      ],
    }

    const summary = summarizeDiagnostics(report)
    expect(summary.counts).toEqual({ error: 1, warning: 2, info: 1 })
    expect(summary.total).toBe(4)
    expect(summary.groups.map((group) => [group.severity, group.count])).toEqual([
      ['error', 1],
      ['warning', 2],
      ['info', 1],
    ])
    // No health score is derived anywhere in the view.
    expect(summary.score).toBeUndefined()
  })

  it('handles an empty report', () => {
    expect(summarizeDiagnostics({})).toMatchObject({
      counts: { error: 0, warning: 0, info: 0 },
      total: 0,
    })
  })
})

describe('autofixableCodes', () => {
  it('returns the distinct auto-fixable codes only', () => {
    const report = {
      issues: [
        { code: 'ENTRY_MISSING', autoFixable: true },
        { code: 'ENTRY_MISSING', autoFixable: true },
        { code: 'MISSING_LINK', autoFixable: false },
        { code: 'ORPHAN_FILE' },
      ],
    }
    expect(autofixableCodes(report)).toEqual(['ENTRY_MISSING'])
  })

  it('returns an empty list when nothing is auto-fixable', () => {
    expect(autofixableCodes({ issues: [{ code: 'MISSING_LINK', autoFixable: false }] })).toEqual([])
    expect(autofixableCodes(null)).toEqual([])
  })
})
