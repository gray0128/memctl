import { describe, expect, it } from 'vitest'
import { createApi } from './api.js'

describe('createApi', () => {
  it('returns the decoded snapshot and sends the session token on every request', async () => {
    const calls = []
    const fetchImpl = async (path, init) => {
      calls.push({ path, init })
      return { ok: true, status: 200, json: async () => ({ repoPath: '/tmp/memory' }) }
    }

    const api = createApi({ token: 'secret', readOnly: true, fetchImpl })
    const snapshot = await api.snapshot()

    expect(snapshot).toEqual({ repoPath: '/tmp/memory' })
    expect(api.readOnly).toBe(true)
    expect(calls[0].path).toBe('/api/v1/snapshot')
    expect(calls[0].init.headers['X-Memctl-Token']).toBe('secret')
  })

  it('maps the documented error envelope to a coded error', async () => {
    const fetchImpl = async () => ({
      ok: false,
      status: 401,
      json: async () => ({ error: { code: 'AUTH_REQUIRED', message: 'a valid token is required' } }),
    })

    const api = createApi({ token: 'wrong', fetchImpl })
    await expect(api.snapshot()).rejects.toMatchObject({
      code: 'AUTH_REQUIRED',
      status: 401,
      message: 'a valid token is required',
    })
  })

  it('sends the managed write requests with the documented preconditions', async () => {
    const calls = []
    const fetchImpl = async (path, init) => {
      calls.push({ path, init })
      return { ok: true, status: 200, json: async () => ({ commit: 'abc' }) }
    }
    const api = createApi({ token: 'secret', fetchImpl })

    await api.putFile('notes/alpha beta.md', '# Alpha\n', 'sha256:old')
    expect(calls[0].path).toBe('/api/v1/files/notes/alpha%20beta.md')
    expect(calls[0].init.method).toBe('PUT')
    expect(calls[0].init.headers['If-Match']).toBe('"sha256:old"')
    expect(calls[0].init.headers['Content-Type']).toBe('application/json')
    expect(JSON.parse(calls[0].init.body)).toEqual({ rawContent: '# Alpha\n' })

    await api.putFile('new.md', '# New\n', 'absent')
    expect(calls[1].init.headers['If-None-Match']).toBe('*')
    expect(calls[1].init.headers['If-Match']).toBeUndefined()

    await api.moveFile({ fromPath: 'a.md', toPath: 'b.md', baseRevision: 'sha256:rev' })
    expect(calls[2].path).toBe('/api/v1/files:move')
    expect(calls[2].init.method).toBe('POST')
    expect(JSON.parse(calls[2].init.body)).toEqual({
      fromPath: 'a.md',
      toPath: 'b.md',
      baseRevision: 'sha256:rev',
    })

    await api.createEntry({ filePath: 'a.md', factText: 'fact', expectedRevision: 'sha256:file' })
    expect(calls[3].path).toBe('/api/v1/entries')
    expect(calls[3].init.method).toBe('POST')

    await api.patchEntry('entry-id', { filePath: 'a.md', factText: 'new' }, 'sha256:file')
    expect(calls[4].path).toBe('/api/v1/entries/entry-id')
    expect(calls[4].init.method).toBe('PATCH')
    expect(calls[4].init.headers['If-Match']).toBe('"sha256:file"')

    await api.deleteEntry('entry-id', 'a.md', 'sha256:file')
    expect(calls[5].path).toBe('/api/v1/entries/entry-id?filePath=a.md')
    expect(calls[5].init.method).toBe('DELETE')
    expect(calls[5].init.headers['If-Match']).toBe('"sha256:file"')

    await api.doctorFix({ issueCodes: ['ENTRY_MISSING'], baseRevision: 'sha256:repo' })
    expect(calls[6].path).toBe('/api/v1/doctor:fix')
    expect(calls[6].init.method).toBe('POST')

    await api.sync()
    expect(calls[7].path).toBe('/api/v1/sync')
    expect(calls[7].init.method).toBe('POST')
  })

  it('reads one file through the path wildcard', async () => {
    const calls = []
    const fetchImpl = async (path, init) => {
      calls.push({ path, init })
      return { ok: true, status: 200, json: async () => ({ file: {}, rawContent: 'x' }) }
    }
    const api = createApi({ token: 'secret', fetchImpl })
    await api.file('notes/alpha beta.md')
    expect(calls[0].path).toBe('/api/v1/files/notes/alpha%20beta.md')
    expect(calls[0].init.headers['X-Memctl-Token']).toBe('secret')
  })

  it('sends the conflict and revert requests and maps 204 to null', async () => {
    const calls = []
    const fetchImpl = async (path, init) => {
      calls.push({ path, init })
      if (path === '/api/v1/conflict') {
        return { ok: true, status: 204, json: async () => ({}) }
      }
      return { ok: true, status: 200, json: async () => ({ commit: 'abc' }) }
    }
    const api = createApi({ token: 'secret', fetchImpl })

    await expect(api.conflict()).resolves.toBeNull()
    expect(calls[0].path).toBe('/api/v1/conflict')
    expect(calls[0].init.body).toBeUndefined()

    await api.resolveConflict({
      conflictId: 'c1',
      baseRevision: 'sha256:rev',
      resolvedFiles: [{ path: 'a.md', action: 'put', rawContent: 'x' }],
    })
    expect(calls[1].path).toBe('/api/v1/conflict:resolve')
    expect(calls[1].init.method).toBe('POST')
    expect(JSON.parse(calls[1].init.body).resolvedFiles[0].path).toBe('a.md')

    await api.abortConflict({ conflictId: 'c1' })
    expect(calls[2].path).toBe('/api/v1/conflict:abort')
    expect(calls[2].init.method).toBe('POST')

    await api.revertCommit('abc123', 'sha256:rev')
    expect(calls[3].path).toBe('/api/v1/audit/abc123/revert')
    expect(calls[3].init.method).toBe('POST')
    expect(JSON.parse(calls[3].init.body)).toEqual({ baseRevision: 'sha256:rev' })
  })

  it('sends the adapter preview and install requests', async () => {
    const calls = []
    const fetchImpl = async (path, init) => {
      calls.push({ path, init })
      return { ok: true, status: 200, json: async () => ({ adapters: [] }) }
    }
    const api = createApi({ token: 'secret', fetchImpl })

    await api.adapters()
    expect(calls[0].path).toBe('/api/v1/adapters')
    expect(calls[0].init.method).toBeUndefined()
    expect(calls[0].init.headers['X-Memctl-Token']).toBe('secret')

    await api.installAdapter('claude-code', { dryRun: true })
    expect(calls[1].path).toBe('/api/v1/adapters/claude-code/install')
    expect(calls[1].init.method).toBe('POST')
    expect(JSON.parse(calls[1].init.body)).toEqual({ dryRun: true })

    await api.installAdapter('pi', {
      dryRun: false,
      expectedConfigRevisions: { '/home/u/.pi/agent/hook/hooks.yaml': 'sha256:old' },
    })
    expect(calls[2].path).toBe('/api/v1/adapters/pi/install')
    expect(JSON.parse(calls[2].init.body).expectedConfigRevisions).toEqual({
      '/home/u/.pi/agent/hook/hooks.yaml': 'sha256:old',
    })
  })

  it('sends the Dreaming preview, status and apply requests', async () => {
    const calls = []
    const fetchImpl = async (path, init) => {
      calls.push({ path, init })
      if (path === '/api/v1/dream:preview') {
        return { ok: true, status: 202, json: async () => ({ proposalId: 'dp-1' }) }
      }
      return { ok: true, status: 200, json: async () => ({ proposalId: 'dp-1' }) }
    }
    const api = createApi({ token: 'secret', dreamSandbox: true, fetchImpl })
    expect(api.dreamSandbox).toBe(true)

    await api.dreamPreview('claude', '')
    expect(calls[0].path).toBe('/api/v1/dream:preview')
    expect(calls[0].init.method).toBe('POST')
    expect(JSON.parse(calls[0].init.body)).toEqual({ runner: 'claude', baseRevision: '' })

    await api.dreamProposal('dp-1')
    expect(calls[1].path).toBe('/api/v1/dream/dp-1')
    expect(calls[1].init.method).toBeUndefined()

    await api.dreamApply({ proposalId: 'dp-1', patchHash: 'sha256:patch' })
    expect(calls[2].path).toBe('/api/v1/dream:apply')
    expect(calls[2].init.method).toBe('POST')
    expect(JSON.parse(calls[2].init.body)).toEqual({ proposalId: 'dp-1', patchHash: 'sha256:patch' })
  })
})
