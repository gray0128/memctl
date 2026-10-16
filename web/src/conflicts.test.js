import { describe, expect, it } from 'vitest'
import {
  buildResolvedFiles,
  canResolve,
  conflictPhaseLabel,
  defaultChoice,
  initialChoices,
  isUnsupportedConflict,
  revertBlockReason,
  sideText,
  supportedConflictFiles,
  unsupportedConflictFiles,
} from './conflicts.js'

// This file verifies the conflict request shaping and the revert gating of
// docs/04-ui-and-cli-specification.md section 4 against the ConflictFile,
// ConflictSide and AuditCommit contracts of
// docs/02-domain-models-and-contracts.md sections 2.2 and 2.3.

const side = (content) => ({ exists: true, content })
const absent = { exists: false }
const binary = { exists: true, binary: true }

function textConflict(path, base, onto, replayed, kind = 'text') {
  return { path, kind, base, onto, replayed }
}

describe('isUnsupportedConflict', () => {
  it('accepts text and delete_modify and rejects binary and unsupported', () => {
    expect(isUnsupportedConflict({ kind: 'text' })).toBe(false)
    expect(isUnsupportedConflict({ kind: 'delete_modify' })).toBe(false)
    expect(isUnsupportedConflict({ kind: 'binary' })).toBe(true)
    expect(isUnsupportedConflict({ kind: 'unsupported' })).toBe(true)
    expect(isUnsupportedConflict(null)).toBe(true)
  })
})

describe('sideText', () => {
  it('distinguishes an empty file from a deletion and hides binary content', () => {
    expect(sideText(side(''))).toBe('')
    expect(sideText(side('fact'))).toBe('fact')
    expect(sideText(absent)).toBeNull()
    expect(sideText(binary)).toBeNull()
    expect(sideText(undefined)).toBeNull()
  })
})

describe('defaultChoice', () => {
  it('keeps the replayed local content when it exists', () => {
    expect(defaultChoice(textConflict('a.md', side('base'), side('onto'), side('local')))).toEqual({
      action: 'put',
      rawContent: 'local',
    })
  })

  it('defaults to a deletion when the local commit deleted the path', () => {
    expect(defaultChoice(textConflict('a.md', side('base'), side('onto'), absent))).toEqual({
      action: 'delete',
      rawContent: '',
    })
  })
})

describe('initialChoices and buildResolvedFiles', () => {
  it('covers every supported file and never includes an unsupported path', () => {
    const conflict = {
      files: [
        textConflict('a.md', side('a-base'), side('a-onto'), side('a-local')),
        textConflict('b.md', side('b-base'), side('b-onto'), absent, 'delete_modify'),
        textConflict('image.png', absent, binary, binary, 'binary'),
      ],
    }
    const choices = initialChoices(conflict)
    expect(Object.keys(choices)).toEqual(['a.md', 'b.md'])
    expect(supportedConflictFiles(conflict).map((file) => file.path)).toEqual(['a.md', 'b.md'])
    expect(unsupportedConflictFiles(conflict).map((file) => file.path)).toEqual(['image.png'])

    // Switching b.md to the upstream baseline keeps the chosen text.
    choices['b.md'] = { action: 'put', rawContent: sideText(conflict.files[1].onto) }
    expect(buildResolvedFiles(conflict, choices)).toEqual([
      { path: 'a.md', action: 'put', rawContent: 'a-local' },
      { path: 'b.md', action: 'put', rawContent: 'b-onto' },
    ])
  })

  it('falls back to the default choice for a missing draft entry', () => {
    const conflict = { files: [textConflict('a.md', side('base'), side('onto'), side('local'))] }
    expect(buildResolvedFiles(conflict, {})).toEqual([
      { path: 'a.md', action: 'put', rawContent: 'local' },
    ])
  })
})

describe('canResolve', () => {
  it('requires a decision for every supported file and no unsupported path', () => {
    const conflict = { files: [textConflict('a.md', side('base'), side('onto'), side('local'))] }
    expect(canResolve(conflict, initialChoices(conflict))).toBe(true)
    expect(canResolve(conflict, {})).toBe(false)
    expect(canResolve({ files: [] }, {})).toBe(false)
    expect(canResolve(null, {})).toBe(false)

    const withUnsupported = {
      files: [
        textConflict('a.md', side('base'), side('onto'), side('local')),
        textConflict('image.png', absent, binary, binary, 'binary'),
      ],
    }
    expect(canResolve(withUnsupported, initialChoices(withUnsupported))).toBe(false)
  })
})

describe('conflictPhaseLabel', () => {
  it('describes the captured and resolving stages', () => {
    expect(conflictPhaseLabel('captured')).toContain('冲突已捕获')
    expect(conflictPhaseLabel('resolving')).toContain('重放进行中')
  })
})

describe('revertBlockReason', () => {
  it('refuses root and merge commits and allows a single-parent commit', () => {
    expect(revertBlockReason({ parents: [] })).toContain('根提交')
    expect(revertBlockReason({ parents: ['p1', 'p2'] })).toContain('合并提交')
    expect(revertBlockReason({ parents: ['p1'] })).toBeNull()
    expect(revertBlockReason({})).toContain('根提交')
  })
})
