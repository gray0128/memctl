import { describe, expect, it } from 'vitest'
import {
  applyPayload,
  canApply,
  canGenerate,
  changeActionLabel,
  changeSummary,
  isLocalProposal,
  isTerminalProposal,
  previewFailureDisablesBackend,
  proposalDetail,
  proposalStatusLabel,
  runnerLabel,
  RUNNERS,
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

  it('disables generation without a ready backend or in read-only mode', () => {
    expect(canGenerate(false, true)).toBe(true)
    expect(canGenerate(false, false)).toBe(false)
    expect(canGenerate(true, true)).toBe(false)
  })

  it('only latches generation off when a backend prerequisite is missing', () => {
    expect(previewFailureDisablesBackend('UNSUPPORTED', false)).toBe(true)
    expect(previewFailureDisablesBackend('REMOTE_NOT_APPROVED', false)).toBe(true)
    // A provider failure is also UNSUPPORTED, but the backend is ready, so the
    // capability state must not change.
    expect(previewFailureDisablesBackend('UNSUPPORTED', true)).toBe(false)
    expect(previewFailureDisablesBackend('BUSY', true)).toBe(false)
    expect(previewFailureDisablesBackend('', true)).toBe(false)
  })

  it('lists exactly the four built-in providers', () => {
    expect(RUNNERS.map((item) => item.id)).toEqual(['deepseek', 'minimax', 'openai', 'anthropic'])
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

  it('labels the backend and keeps the local corpus counters', () => {
    expect(runnerLabel('local-agent')).toBe('本地 Agent')
    expect(runnerLabel('deepseek')).toBe('DeepSeek')
    expect(runnerLabel('')).toBe('未知')
    // Only a local-agent proposal carries the scanned and extracted counters.
    const local = { ...ready, runner: 'local-agent', sessionsScanned: 7, newMemoriesExtracted: 2 }
    expect(isLocalProposal(local)).toBe(true)
    expect(proposalDetail(local)).toBe('本地 Agent · 扫描 7 个会话 · 提炼 2 条')
    // A remote proposal never claims counters it does not have.
    expect(isLocalProposal({ ...ready, runner: 'openai' })).toBe(false)
    expect(proposalDetail({ ...ready, runner: 'openai' })).toBe('OpenAI')
    expect(proposalDetail(null)).toBe('')
  })

  it('shows a missing local counter as zero instead of dropping the line', () => {
    const local = { ...ready, runner: 'local-agent' }
    expect(proposalDetail(local)).toBe('本地 Agent · 扫描 0 个会话 · 提炼 0 条')
  })

  it('appends the failure reason to the backend line', () => {
    expect(proposalDetail({ ...ready, runner: 'local-agent', error: 'INVALID_ARGUMENT' })).toBe(
      '本地 Agent · 扫描 0 个会话 · 提炼 0 条 · INVALID_ARGUMENT',
    )
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
