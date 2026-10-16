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
})
