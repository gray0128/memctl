import { useEffect, useState } from 'preact/hooks'
import { isRevisionConflict, lineAt } from '../edits.js'
import { suggestSplitPath } from '../splits.js'
import { DraftDiff } from './DraftDiff.jsx'

// SplitPanel is the knowledge-graph action that moves one entry of the selected
// file into a new file. It reuses PATCH /api/v1/entries/{id} with
// moveToFilePath, which creates the target file when it does not exist. A stale
// baseline keeps the draft decision to the operator. See
// docs/04-ui-and-cli-specification.md section 4.
export function SplitPanel({ api, sourcePath, refreshKey }) {
  const [entries, setEntries] = useState([])
  const [revision, setRevision] = useState('')
  const [error, setError] = useState(null)
  const [active, setActive] = useState(null)
  const [conflict, setConflict] = useState(null)
  const [notice, setNotice] = useState(null)

  useEffect(() => {
    let cancelled = false
    setActive(null)
    setConflict(null)
    setNotice(null)
    api
      .entries({ file: sourcePath, limit: 200 })
      .then((page) => {
        if (cancelled) return
        setEntries(page.items || [])
        setRevision(page.revision)
        setError(null)
      })
      .catch((requestError) => {
        if (!cancelled) setError(requestError)
      })
    return () => {
      cancelled = true
    }
  }, [api, sourcePath, refreshKey])

  async function freshBaselines(entry) {
    const current = await api.file(entry.filePath)
    const snapshot = await api.snapshot()
    return {
      rawContent: current.rawContent,
      fileRevision: current.file.fileRevision,
      repoRevision: snapshot.revision,
    }
  }

  async function split(entry, target, override) {
    setActive((current) => (current ? { ...current, busy: true, error: null } : current))
    try {
      const result = await api.patchEntry(
        entry.id,
        {
          filePath: entry.filePath,
          factText: entry.factText,
          moveToFilePath: target,
          baseRevision: override ? override.repoRevision : revision,
        },
        override ? override.fileRevision : entry.fileRevision,
      )
      setActive(null)
      setConflict(null)
      setNotice(`已提交 ${result.result.commit ? result.result.commit.slice(0, 12) : result.result.transactionId}`)
    } catch (requestError) {
      if (isRevisionConflict(requestError)) {
        setActive(null)
        try {
          const fresh = await freshBaselines(entry)
          setConflict({
            currentText: lineAt(fresh.rawContent, entry.lineNumber),
            draftText: '',
            retry: () => split(entry, target, fresh),
          })
        } catch (refreshError) {
          setError(refreshError)
        }
      } else {
        setActive((current) => (current ? { ...current, busy: false, error: requestError } : current))
      }
    }
  }

  if (error) {
    return <p className="text-sm text-rose-700 dark:text-rose-300">无法读取条目：{error.message}</p>
  }
  if (entries.length === 0) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">该文件没有可拆分的条目。</p>
  }

  return (
    <div className="space-y-2">
      <h3 className="text-sm font-semibold text-slate-600 dark:text-slate-300">拆分条目为新文件</h3>
      {notice && <p className="text-sm text-emerald-700 dark:text-emerald-300">{notice}</p>}
      {conflict && (
        <div className="space-y-2 rounded border border-amber-300 bg-amber-50 p-3 dark:border-amber-900 dark:bg-amber-900/20">
          <p className="text-sm text-amber-900 dark:text-amber-100">基线已过期，拆分未提交，草稿已保留。</p>
          <DraftDiff currentText={conflict.currentText} draftText={conflict.draftText} />
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={() => setConflict(null)}
              className="rounded border border-slate-300 px-3 py-1.5 text-sm dark:border-slate-700"
            >
              放弃草稿
            </button>
            <button
              type="button"
              onClick={() => conflict.retry()}
              className="rounded bg-amber-600 px-3 py-1.5 text-sm font-medium text-white"
            >
              用草稿重试拆分
            </button>
          </div>
        </div>
      )}
      <ul className="space-y-2">
        {entries.map((entry) => {
          const open = active && active.entry.id === entry.id
          return (
            <li key={entry.id} className="rounded border border-slate-200 bg-white p-2 dark:border-slate-800 dark:bg-slate-900">
              <p className="text-sm">{entry.factText}</p>
              <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
                <span className="font-mono">
                  {entry.filePath}:{entry.lineNumber}
                </span>
                <button
                  type="button"
                  disabled={api.readOnly}
                  onClick={() =>
                    setActive(
                      open
                        ? null
                        : { entry, target: suggestSplitPath(entry.filePath, entry), busy: false, error: null },
                    )
                  }
                  className="rounded border border-slate-300 px-2 py-1 disabled:opacity-40 dark:border-slate-700"
                >
                  {open ? '取消' : '拆分为新文件'}
                </button>
              </div>
              {open && (
                <div className="mt-2 space-y-2">
                  <label className="flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
                    新文件路径
                    <input
                      type="text"
                      value={active.target}
                      onInput={(event) =>
                        setActive((current) =>
                          current ? { ...current, target: event.currentTarget.value } : current,
                        )
                      }
                      className="rounded border border-slate-300 bg-white px-2 py-1 font-mono text-sm text-slate-900 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
                    />
                  </label>
                  <button
                    type="button"
                    disabled={active.busy || active.target.trim() === ''}
                    onClick={() => split(entry, active.target.trim())}
                    className="rounded bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
                  >
                    确认拆分
                  </button>
                  {active.error && (
                    <p className="text-sm text-rose-700 dark:text-rose-300">{active.error.message}</p>
                  )}
                </div>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
