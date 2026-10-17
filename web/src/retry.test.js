import { describe, expect, it } from 'vitest'
import { retryBusy } from './retry.js'

function busyError() {
  const error = new Error('repository lock /tmp/memory/.git/memctl.lock is held by another process')
  error.code = 'BUSY'
  error.status = 409
  return error
}

describe('retryBusy', () => {
  it('retries BUSY reads with a doubling delay and returns the first success', async () => {
    const delays = []
    let calls = 0
    const result = await retryBusy(
      async () => {
        calls += 1
        if (calls < 3) throw busyError()
        return 'ok'
      },
      { baseDelayMs: 100, sleep: async (ms) => delays.push(ms) },
    )

    expect(result).toBe('ok')
    expect(calls).toBe(3)
    expect(delays).toEqual([100, 200])
  })

  it('does not retry a non-BUSY error', async () => {
    let calls = 0
    await expect(
      retryBusy(async () => {
        calls += 1
        const error = new Error('forbidden')
        error.code = 'AUTH_REQUIRED'
        throw error
      }),
    ).rejects.toMatchObject({ code: 'AUTH_REQUIRED' })
    expect(calls).toBe(1)
  })

  it('throws a user-facing BUSY error after the bounded attempts', async () => {
    let calls = 0
    const attempt = async () => {
      calls += 1
      throw busyError()
    }

    await expect(
      retryBusy(attempt, { attempts: 3, sleep: async () => {} }),
    ).rejects.toMatchObject({
      code: 'BUSY',
      status: 409,
      message: '仓库正忙（可能正在同步），请稍后重试',
    })
    expect(calls).toBe(3)
  })
})
