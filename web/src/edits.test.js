import { describe, expect, it } from 'vitest'
import {
  diffLines,
  isRevisionConflict,
  lineAt,
  matchCurrentEntry,
  replaceEntryFactLine,
} from './edits.js'

// This file verifies the conflict and draft-diff logic behind
// docs/04-ui-and-cli-specification.md section 4: a 412 must keep the draft and
// show the current content, and an entry fact edit must preserve metadata.

describe('isRevisionConflict', () => {
  it('matches only the documented 412 revision conflict', () => {
    expect(isRevisionConflict({ code: 'REVISION_MISMATCH' })).toBe(true)
    expect(isRevisionConflict({ code: 'NOT_FOUND' })).toBe(false)
    expect(isRevisionConflict(null)).toBe(false)
  })
})

describe('lineAt', () => {
  it('returns the 1-based line and an empty string out of range', () => {
    expect(lineAt('a\nb\nc', 2)).toBe('b')
    expect(lineAt('a\nb\nc', 9)).toBe('')
    expect(lineAt('', 1)).toBe('')
  })
})

describe('diffLines', () => {
  it('aligns unchanged lines and marks the changed ones', () => {
    const rows = diffLines('alpha\none\nbeta\n', 'alpha\ntwo\nbeta\n')
    expect(rows).toEqual([
      { type: 'context', text: 'alpha' },
      { type: 'removed', text: 'one' },
      { type: 'added', text: 'two' },
      { type: 'context', text: 'beta' },
      { type: 'context', text: '' },
    ])
  })

  it('keeps insertions and deletions in order', () => {
    const rows = diffLines('a\nc\n', 'a\nb\nc\n')
    expect(rows.map((row) => `${row.type}:${row.text}`)).toEqual([
      'context:a',
      'added:b',
      'context:c',
      'context:',
    ])
    const removed = diffLines('a\nb\nc\n', 'a\nc\n')
    expect(removed.map((row) => row.type)).toEqual(['context', 'removed', 'context', 'context'])
    expect(removed[1].text).toBe('b')
  })

  it('falls back to a bounded position diff for very large inputs', () => {
    const current = Array.from({ length: 900 }, (_, index) => `old-${index}`).join('\n')
    const draft = Array.from({ length: 900 }, (_, index) => `new-${index}`).join('\n')
    const rows = diffLines(current, draft)
    // The coarse fallback emits one removed and one added row per position.
    expect(rows).toHaveLength(1800)
    expect(rows[0]).toEqual({ type: 'removed', text: 'old-0' })
    expect(rows[1]).toEqual({ type: 'added', text: 'new-0' })
  })

  it('handles empty inputs', () => {
    expect(diffLines('', '')).toEqual([])
    expect(diffLines('', 'x')).toEqual([{ type: 'added', text: 'x' }])
    expect(diffLines('x', '')).toEqual([{ type: 'removed', text: 'x' }])
  })
})

describe('replaceEntryFactLine', () => {
  it('rewrites the fact and keeps trailing metadata bytes', () => {
    const raw = '- old fact [source: agent://dev/claude/s1; added: 2026-01-02]'
    expect(replaceEntryFactLine(raw, ' new fact ')).toBe(
      '- new fact [source: agent://dev/claude/s1; added: 2026-01-02]',
    )
  })

  it('keeps a line without metadata minimal', () => {
    expect(replaceEntryFactLine('- plain', 'changed')).toBe('- changed')
  })

  it('does not touch a non-bullet line', () => {
    expect(replaceEntryFactLine('# heading', 'x')).toBe('# heading')
  })

  it('keeps a link inside the fact instead of treating it as metadata', () => {
    expect(replaceEntryFactLine('- see [[beta]] [source: x]', 'see [[gamma]]')).toBe(
      '- see [[gamma]] [source: x]',
    )
  })
})

describe('matchCurrentEntry', () => {
  const stale = { id: 'old-id', filePath: 'a.md', lineNumber: 3, factText: 'fact' }

  it('prefers the same line and fact after an external write', () => {
    const entries = [
      { id: 'new-a', lineNumber: 3, factText: 'fact' },
      { id: 'new-b', lineNumber: 9, factText: 'fact' },
    ]
    expect(matchCurrentEntry(entries, stale).id).toBe('new-a')
  })

  it('falls back to the unique fact when the line moved', () => {
    const entries = [
      { id: 'other', lineNumber: 1, factText: 'other' },
      { id: 'moved', lineNumber: 8, factText: 'fact' },
    ]
    expect(matchCurrentEntry(entries, stale).id).toBe('moved')
  })

  it('returns null when the entry no longer exists', () => {
    expect(matchCurrentEntry([{ id: 'other', lineNumber: 1, factText: 'other' }], stale)).toBe(null)
    expect(matchCurrentEntry([], stale)).toBe(null)
    expect(matchCurrentEntry(null, stale)).toBe(null)
  })
})
