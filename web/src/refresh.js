// loadFreshSnapshot re-reads the repository snapshot until it is no longer a
// stale fallback. A read can race the repository lock of an external writer, so
// the server answers with the most recent snapshot marked `stale: true`. The
// invalidation hint carries no second event, so accepting that answer would
// freeze the status bar on old data. The retry is bounded and doubles its
// delay; the last response is returned when the writer keeps the lock.
//
// See docs/04-ui-and-cli-specification.md section 4.

function defaultSleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

export async function loadFreshSnapshot(api, options = {}) {
  const attempts = options.attempts ?? 4
  const baseDelayMs = options.baseDelayMs ?? 300
  const sleep = options.sleep ?? defaultSleep
  let snapshot = null
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    snapshot = await api.snapshot()
    if (!snapshot || !snapshot.stale) return snapshot
    if (attempt < attempts - 1) await sleep(baseDelayMs * 2 ** attempt)
  }
  return snapshot
}
