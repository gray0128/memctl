import { useEffect, useState } from 'preact/hooks'

const PAGE_SIZE = 50

const EMPTY_FILTERS = { q: '', agent: '', device: '', file: '', from: '', to: '' }
const EMPTY_LABELS = {
  q: '关键词',
  agent: 'Agent',
  device: '设备',
  file: '文件',
  from: '起始日期',
  to: '结束日期',
}

function FilterInput({ name, value, onChange }) {
  const type = name === 'from' || name === 'to' ? 'date' : 'text'
  return (
    <label className="flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
      {EMPTY_LABELS[name]}
      <input
        type={type}
        value={value}
        placeholder={type === 'text' ? '全部' : undefined}
        onInput={(event) => onChange(name, event.currentTarget.value)}
        className="rounded border border-slate-300 bg-white px-2 py-1 text-sm text-slate-900 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
      />
    </label>
  )
}

function SourceList({ sources }) {
  if (!sources || sources.length === 0) return <span className="text-slate-400">来源未知</span>
  return (
    <span>
      {sources.map((source) => (
        <span key={source.raw} className="mr-2 break-all font-mono text-xs text-slate-500 dark:text-slate-400">
          {source.raw}
        </span>
      ))}
    </span>
  )
}

// EntryBrowser is the read-only entry search view. It sends the documented
// q/agent/device/file/from/to filters to GET /api/v1/entries, paginates the
// result and renders the original line text. Editing belongs to stage C-1.
export function EntryBrowser({ api, refreshKey }) {
  const [draft, setDraft] = useState(EMPTY_FILTERS)
  const [applied, setApplied] = useState(EMPTY_FILTERS)
  const [offset, setOffset] = useState(0)
  const [page, setPage] = useState(null)
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    api
      .entries({ ...applied, limit: PAGE_SIZE, offset })
      .then((result) => {
        if (cancelled) return
        setPage(result)
        setError(null)
      })
      .catch((requestError) => {
        if (!cancelled) setError(requestError)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [api, applied, offset, refreshKey])

  function submit(event) {
    event.preventDefault()
    setOffset(0)
    setApplied({ ...draft })
  }

  function reset() {
    setDraft(EMPTY_FILTERS)
    setApplied(EMPTY_FILTERS)
    setOffset(0)
  }

  const total = page ? page.total : 0
  const items = page ? page.items : []
  const end = Math.min(offset + PAGE_SIZE, total)

  return (
    <section className="space-y-4">
      <h1 className="text-xl font-semibold">条目浏览</h1>
      <form
        onSubmit={submit}
        className="flex flex-wrap items-end gap-3 rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900"
      >
        {Object.keys(EMPTY_FILTERS).map((name) => (
          <FilterInput
            key={name}
            name={name}
            value={draft[name]}
            onChange={(key, value) => setDraft((current) => ({ ...current, [key]: value }))}
          />
        ))}
        <button
          type="submit"
          className="rounded bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white"
        >
          查询
        </button>
        <button
          type="button"
          onClick={reset}
          className="rounded border border-slate-300 px-3 py-1.5 text-sm dark:border-slate-700"
        >
          重置
        </button>
      </form>

      {error && (
        <p className="text-sm text-rose-700 dark:text-rose-300">无法读取条目：{error.message}</p>
      )}

      <div className="flex items-baseline justify-between text-sm text-slate-500 dark:text-slate-400">
        <span>
          共 {total} 条{loading ? ' · 读取中…' : ''}
        </span>
        <span>
          {total === 0 ? '0' : `${offset + 1}–${end}`}
        </span>
      </div>

      {page && items.length === 0 && !error && (
        <p className="text-sm text-slate-500 dark:text-slate-400">没有匹配的条目。</p>
      )}

      <ul className="space-y-2">
        {items.map((entry) => (
          <li
            key={entry.id}
            className="rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900"
          >
            <p className="text-sm text-slate-900 dark:text-slate-100">{entry.factText}</p>
            <div className="mt-1 flex flex-wrap items-baseline gap-x-3 text-xs text-slate-500 dark:text-slate-400">
              <span className="font-mono">
                {entry.filePath}:{entry.lineNumber}
              </span>
              {entry.addedDate && <span>{entry.addedDate}</span>}
              <SourceList sources={entry.sources} />
            </div>
          </li>
        ))}
      </ul>

      <div className="flex gap-2">
        <button
          type="button"
          disabled={offset === 0}
          onClick={() => setOffset((current) => Math.max(0, current - PAGE_SIZE))}
          className="rounded border border-slate-300 px-3 py-1.5 text-sm disabled:opacity-40 dark:border-slate-700"
        >
          上一页
        </button>
        <button
          type="button"
          disabled={end >= total}
          onClick={() => setOffset((current) => current + PAGE_SIZE)}
          className="rounded border border-slate-300 px-3 py-1.5 text-sm disabled:opacity-40 dark:border-slate-700"
        >
          下一页
        </button>
      </div>
    </section>
  )
}
