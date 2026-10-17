import { describe, expect, it } from 'vitest'
import { loadFreshSnapshot } from './refresh.js'

// This file verifies the UI half of the T039 fix: an invalidation that races an
// external writer returns a stale fallback snapshot, and the view must retry it
// instead of freezing on old data. See docs/04-ui-and-cli-specification.md
// section 4.

describe('loadFreshSnapshot', () => {
  it('returns a fresh snapshot without sleeping', async () => {
    const delays = []
    const api = { snapshot: async () => ({ revision: 'fresh', stale: false }) }

    const result = await loadFreshSnapshot(api, { sleep: async (ms) => delays.push(ms) })

    expect(result).toEqual({ revision: 'fresh', stale: false })
    expect(delays).toEqual([])
  })

  it('retries a stale fallback with a growing delay until it is fresh', async () => {
    const responses = [
      { revision: 'old', stale: true },
      { revision: 'older', stale: true },
      { revision: 'fresh', stale: false },
    ]
    const calls = []
    const delays = []
    const api = {
      snapshot: async () => {
        calls.push(responses.length)
        return responses.shift()
      },
    }

    const result = await loadFreshSnapshot(api, {
      attempts: 4,
      baseDelayMs: 100,
      sleep: async (ms) => delays.push(ms),
    })

    expect(result).toEqual({ revision: 'fresh', stale: false })
    expect(delays).toEqual([100, 200])
  })

  it('returns the last stale snapshot when the writer never releases', async () => {
    const api = { snapshot: async () => ({ revision: 'stuck', stale: true }) }
    const delays = []

    const result = await loadFreshSnapshot(api, {
      attempts: 3,
      baseDelayMs: 50,
      sleep: async (ms) => delays.push(ms),
    })

    expect(result).toEqual({ revision: 'stuck', stale: true })
    expect(delays).toEqual([50, 100])
  })

  it('propagates a snapshot error instead of retrying it', async () => {
    const api = {
      snapshot: async () => {
        throw new Error('offline')
      },
    }

    await expect(loadFreshSnapshot(api)).rejects.toThrow('offline')
  })
})
