// retryBusy retries idempotent reads that raced the repository lock. The
// server answers BUSY when another memctl process holds the lock (a sync, a
// managed write or an external writer) and the read does not wait for it.
// The retry is bounded and doubles its delay; when the lock stays held the
// error is rethrown with a user-facing message instead of the server's
// diagnostic path. See docs/04-ui-and-cli-specification.md section 4.

const BUSY_MESSAGE = '仓库正忙（可能正在同步），请稍后重试'

function defaultSleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

export async function retryBusy(fn, options = {}) {
  const attempts = options.attempts ?? 4
  const baseDelayMs = options.baseDelayMs ?? 200
  const sleep = options.sleep ?? defaultSleep
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    try {
      return await fn()
    } catch (error) {
      if (!error || error.code !== 'BUSY') throw error
      if (attempt === attempts - 1) {
        const busy = new Error(BUSY_MESSAGE)
        busy.code = 'BUSY'
        busy.status = error.status
        throw busy
      }
      await sleep(baseDelayMs * 2 ** attempt)
    }
  }
  return undefined
}
