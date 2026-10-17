import { describe, expect, it } from 'vitest'
import {
  applyPayload,
  canApply,
  canGenerate,
  changeActionLabel,
  changeSummary,
  isTerminalProposal,
  previewFailureDisablesSandbox,
  proposalStatusLabel,
  summarizeProposal,
} from './dreams.js'

const ready = {
  proposalId: 'dp-abc',
  status: 'ready',
  patchHash: 'sha256:abc',
  changes: [
    { path: 'MEMORY.md', action: 'update' },
    { path: 'new.md', action: 'create' },
  ],
  warnings: ['one warning'],
}

describe('dream helpers', () => {
  it('labels proposal statuses and change actions', () => {
    expect(proposalStatusLabel('ready')).toBe('待确认')
    expect(proposalStatusLabel('stale')).toBe('已过期')
    expect(proposalStatusLabel('')).toBe('未知')
    expect(changeActionLabel('create')).toBe('新增')
    expect(changeActionLabel('delete')).toBe('删除')
  })

  it('stops polling only on a terminal status', () => {
    expect(isTerminalProposal('generating')).toBe(false)
    expect(isTerminalProposal('applying')).toBe(false)
    expect(isTerminalProposal('ready')).toBe(true)
    expect(isTerminalProposal('applied')).toBe(true)
    expect(isTerminalProposal('stale')).toBe(true)
    expect(isTerminalProposal('failed')).toBe(true)
  })

  it('disables generation without a sandbox or in read-only mode', () => {
    expect(canGenerate(false, true)).toBe(true)
    expect(canGenerate(false, false)).toBe(false)
    expect(canGenerate(true, true)).toBe(false)
  })

  it('only latches generation off when the sandbox is the missing capability', () => {
    expect(previewFailureDisablesSandbox('UNSUPPORTED', false)).toBe(true)
    // A missing runner is also UNSUPPORTED, but the sandbox is available, so
    // the capability state must not change.
    expect(previewFailureDisablesSandbox('UNSUPPORTED', true)).toBe(false)
    expect(previewFailureDisablesSandbox('BUSY', true)).toBe(false)
    expect(previewFailureDisablesSandbox('', true)).toBe(false)
  })

  it('only allows applying a ready proposal with a patch hash', () => {
    expect(canApply(ready, false)).toBe(true)
    expect(canApply(ready, true)).toBe(false)
    expect(canApply({ ...ready, status: 'stale' }, false)).toBe(false)
    expect(canApply({ ...ready, patchHash: '' }, false)).toBe(false)
    expect(canApply(null, false)).toBe(false)
  })

  it('quotes the reviewed id and hash', () => {
    expect(applyPayload(ready)).toEqual({ proposalId: 'dp-abc', patchHash: 'sha256:abc' })
  })

  it('summarizes the ordered change list and applied commit', () => {
    expect(changeSummary(ready)).toBe('更新 MEMORY.md，新增 new.md')
    expect(changeSummary({ changes: [] })).toBe('无变化')
    expect(summarizeProposal({ ...ready, status: 'applied', appliedCommit: 'abcdef0123456789' })).toBe(
      '已应用 · 提交 abcdef012345 · 2 项变化 · 1 条警告',
    )
    expect(summarizeProposal(null)).toBe('')
  })
})
