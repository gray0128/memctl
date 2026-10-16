import { describe, expect, it } from 'vitest'
import { slugFromFact, suggestSplitPath } from './splits.js'

// This file verifies the default target path of the graph split action; the
// operator can still replace it before the transaction runs.

describe('slugFromFact', () => {
  it('builds a lowercase dash-separated slug', () => {
    expect(slugFromFact('Graph Split Entry')).toBe('graph-split-entry')
    expect(slugFromFact('  trailing and leading  ')).toBe('trailing-and-leading')
  })

  it('keeps non-latin letters so Chinese facts stay readable', () => {
    expect(slugFromFact('拆分条目为新文件')).toBe('拆分条目为新文件')
    expect(slugFromFact('条目 2：图谱')).toBe('条目-2-图谱')
  })

  it('returns an empty slug for punctuation-only facts', () => {
    expect(slugFromFact('!!! --- ???')).toBe('')
  })
})

describe('suggestSplitPath', () => {
  it('keeps the source directory and uses a markdown extension', () => {
    expect(suggestSplitPath('notes/alpha.md', { factText: 'Split me', lineNumber: 3 })).toBe(
      'notes/split-me.md',
    )
  })

  it('falls back to the line number for a punctuation-only fact', () => {
    expect(suggestSplitPath('MEMORY.md', { factText: '???', lineNumber: 7 })).toBe('entry-7.md')
  })
})
