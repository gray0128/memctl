import { describe, expect, it } from 'vitest'
import {
  actionLabel,
  adapterByAgent,
  canInstall,
  capabilityLabel,
  installBaseline,
  mcpAction,
  mcpAvailable,
  mcpReason,
  mcpTools,
  planArtifacts,
  statusLabel,
  summarizeInstall,
} from './adapters.js'

const plan = {
  agent: 'claude-code',
  status: 'not_installed',
  applicable: true,
  artifacts: [
    { path: '/home/u/.claude/settings.json', format: 'json', action: 'create' },
    {
      path: '/home/u/.codex/config.toml',
      format: 'toml',
      action: 'update',
      currentRevision: 'sha256:old',
    },
    { path: '/home/u/.grok/hooks.json', format: 'json', action: 'manual', note: '需人工补丁' },
    { path: '/home/u/.dsh/hooks.json', format: 'json', action: 'skip' },
  ],
}

describe('adapter helpers', () => {
  it('labels statuses, actions and capabilities', () => {
    expect(statusLabel('configured')).toBe('已配置')
    expect(statusLabel('verified')).toBe('已验证')
    expect(statusLabel('')).toBe('未知')
    expect(actionLabel('manual')).toBe('人工补丁')
    expect(capabilityLabel('hooks')).toBe('命令 Hook')
    expect(capabilityLabel('custom')).toBe('custom')
  })

  it('builds the install baseline from existing targets only', () => {
    expect(installBaseline(plan)).toEqual({
      '/home/u/.codex/config.toml': 'sha256:old',
    })
    expect(installBaseline(null)).toEqual({})
  })

  it('only allows an install when the plan applies or a recovery is pending', () => {
    expect(canInstall(plan, false)).toBe(true)
    expect(canInstall(plan, true)).toBe(false)
    expect(canInstall({ ...plan, applicable: false }, false)).toBe(false)
    expect(canInstall({ ...plan, applicable: false }, false, true)).toBe(true)
    expect(canInstall({ ...plan, applicable: false }, true, true)).toBe(false)
    expect(canInstall(null, false)).toBe(false)
  })

  it('orders writes before manual patches and skips', () => {
    expect(planArtifacts(plan).map((artifact) => artifact.action)).toEqual([
      'create',
      'update',
      'manual',
      'skip',
    ])
    expect(planArtifacts(null)).toEqual([])
  })

  it('finds one adapter by name', () => {
    const adapters = [{ agent: 'pi' }, { agent: 'dsh' }]
    expect(adapterByAgent(adapters, 'dsh')).toEqual({ agent: 'dsh' })
    expect(adapterByAgent(adapters, 'codex')).toBeNull()
    expect(adapterByAgent(null, 'pi')).toBeNull()
  })

  it('summarizes the install result without claiming host verification', () => {
    expect(
      summarizeInstall({
        status: { status: 'configured' },
        applied: [{ path: 'a' }, { path: 'b' }],
        manual: [{ path: 'c' }],
        skipped: [],
      }),
    ).toBe('已配置：已写入 2 个文件，需人工处理 1 个文件')
    expect(
      summarizeInstall({ status: { status: 'configured' }, applied: [], manual: [], skipped: ['a'] }),
    ).toBe('已配置：配置已是最新')
    expect(summarizeInstall({ status: { status: 'needs_attention' } })).toBe(
      '需要处理：没有可安装的变更',
    )
    expect(summarizeInstall(null)).toBe('')
  })
})

describe('mcp helpers', () => {
  const status = {
    executable: '/opt/memctl',
    available: true,
    version: 'v9.9.9',
    tools: ['memory_entries', 'memory_read', 'memory_write'],
    registrations: [
      { agent: 'codex', command: 'codex mcp add memctl -- /opt/memctl mcp --agent codex' },
      { agent: 'dsh', instruction: 'add an @deepseek-ai/dsh-mcp-client stdio entry' },
    ],
  }

  it('reports availability, reason and tools', () => {
    expect(mcpAvailable(status)).toBe(true)
    expect(mcpReason(status)).toBe('')
    expect(mcpTools(status)).toBe('memory_entries、memory_read、memory_write')
    expect(mcpAvailable({ available: false, reason: 'no tools/list response' })).toBe(false)
    expect(mcpReason({ available: false, reason: 'no tools/list response' })).toBe('no tools/list response')
    expect(mcpReason({ available: false })).toBe('二进制未返回 tools/list')
    expect(mcpReason(null)).toBe('未探测')
    expect(mcpTools(null)).toBe('无')
  })

  it('renders the actionable text of one registration', () => {
    expect(mcpAction(status.registrations[0])).toBe(
      'codex mcp add memctl -- /opt/memctl mcp --agent codex',
    )
    expect(mcpAction(status.registrations[1])).toBe('add an @deepseek-ai/dsh-mcp-client stdio entry')
    expect(mcpAction(null)).toBe('')
  })
})
