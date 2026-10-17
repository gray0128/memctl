import { useEffect, useState } from 'preact/hooks'
import { isRevisionConflict, lineAt, matchCurrentEntry } from '../edits.js'
import { suggestSplitPath } from '../splits.js'
import { DraftDiff } from './DraftDiff.jsx'

// SplitPanel is the knowledge-graph action that moves one entry of the selected
// file into a new file. It reuses PATCH /api/v1/entries/{id} with
// moveToFilePath, technical target file creation when absent.
export function SplitPanel({ api, sourcePath, refreshKey }) {
  const [entries, setEntries] = useState([])
  const [revision, setRevision] = useState('')
  const [files, setFiles] = useState([])
  const [error, setError] = useState(null)
  const [active, setActive] = useState(null)
  const [conflict, setConflict] = useState(null)
  const [notice, setNotice] = useState(null)

  const drafting = Boolean(active || conflict)

  useEffect(() => {
    setActive(null)
    setConflict(null)
    setNotice(null)
  }, [api, sourcePath])

  useEffect(() => {
    if (drafting) return
    let cancelled = false
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
    api
      .snapshot()
      .then((snap) => {
        if (!cancelled) setFiles(((snap && snap.files) || []).map((file) => file.relPath))
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [api, sourcePath, refreshKey, drafting])

  async function freshBaselines(entry) {
    const current = await api.file(entry.filePath)
    const snapshot = await api.snapshot()
    return {
      rawContent: current.rawContent,
      fileRevision: current.file.fileRevision,
      repoRevision: snapshot.revision,
    }
  }

  async function currentEntriesFor(entry) {
    const page = await api.entries({ file: entry.filePath, q: entry.factText, limit: 200, offset: 0 })
    return page.items || []
  }

  async function handleConflict(entry, target) {
    try {
      const current = matchCurrentEntry(await currentEntriesFor(entry), entry) || entry
      const fresh = await freshBaselines(current)
      setConflict({
        currentText: lineAt(fresh.rawContent, current.lineNumber),
        draftText: '',
        busy: false,
        error: null,
        retry: async () => {
          const latest = matchCurrentEntry(await currentEntriesFor(entry), entry)
          if (!latest) {
            throw new Error('条目已被改写，无法定位；请刷新后重建草稿。')
          }
          return split(latest, target, await freshBaselines(latest))
        },
      })
    } catch (refreshError) {
      setError(refreshError)
    }
  }

  async function runConflictRetry() {
    setConflict((current) => (current ? { ...current, busy: true, error: null } : current))
    try {
      await conflict.retry()
    } catch (retryError) {
      setConflict((current) => (current ? { ...current, busy: false, error: retryError } : current))
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
      setNotice(`已成功拆分，提交哈希 ${result.result.commit ? result.result.commit.slice(0, 12) : result.result.transactionId}`)
    } catch (requestError) {
      if (isRevisionConflict(requestError)) {
        setActive(null)
        await handleConflict(entry, target)
      } else {
        setActive((current) => (current ? { ...current, busy: false, error: requestError } : current))
      }
    }
  }

  if (error) {
    return <p className="text-xs text-rose-600 dark:text-rose-400">无法读取条目：{error.message}</p>
  }
  if (entries.length === 0) {
    return <p className="text-xs text-slate-500 dark:text-slate-400">该文件暂无可供拆分的条目。</p>
  }

  const target = active ? active.target.trim() : ''
  const targetExists = target !== '' && files.includes(target)

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h4 className="text-xs font-bold tracking-tight text-slate-700 dark:text-slate-300">
          拆分条目至独立新文件
        </h4>
        <span className="text-[11px] text-slate-400">
          可达拓扑将随拆分后链接自动更新
        </span>
      </div>

      {notice && (
        <div className="flex items-center gap-2 rounded-lg bg-emerald-50 px-3 py-1.5 text-xs font-medium text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300">
          <svg className="h-3.5 w-3.5 text-emerald-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.5">
            <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
          </svg>
          <span>{notice}</span>
        </div>
      )}

      {conflict && (
        <div className="space-y-2 rounded-xl border border-amber-300/80 bg-amber-50 p-3 text-xs dark:border-amber-900/60 dark:bg-amber-950/40">
          <p className="font-semibold text-amber-900 dark:text-amber-200">基线已过期，拆分未提交，草稿已完好保留：</p>
          <div className="rounded bg-white p-2 shadow-inner dark:bg-slate-900">
            <DraftDiff currentText={conflict.currentText} draftText={conflict.draftText} />
          </div>
          {conflict.error && (
            <p className="text-rose-600 dark:text-rose-400">重试失败：{conflict.error.message}</p>
          )}
          <div className="flex gap-2 pt-1">
            <button
              type="button"
              onClick={() => setConflict(null)}
              className="rounded-lg border border-slate-300 px-2.5 py-1 text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-200"
            >
              放弃草稿
            </button>
            <button
              type="button"
              onClick={runConflictRetry}
              disabled={conflict.busy}
              className="rounded-lg bg-amber-600 px-2.5 py-1 font-semibold text-white shadow-2xs hover:bg-amber-700 active:scale-95 disabled:opacity-40"
            >
              用草稿重试拆分
            </button>
          </div>
        </div>
      )}

      <ul className="space-y-2 max-h-80 overflow-y-auto pr-1">
        {entries.map((entry) => {
          const open = active && active.entry.id === entry.id
          return (
            <li
              key={entry.id}
              className="rounded-lg border border-slate-200/80 bg-slate-50/60 p-3 transition-colors hover:border-slate-300 dark:border-slate-800 dark:bg-slate-950/40 dark:hover:border-slate-700"
            >
              <p className="text-xs text-slate-800 dark:text-slate-200">{entry.factText}</p>
              <div className="mt-2 flex items-center justify-between text-[11px] text-slate-400">
                <span className="font-mono">行号 {entry.lineNumber}</span>
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
                  className="rounded-md border border-slate-200/80 bg-white px-2 py-0.5 font-medium text-slate-700 shadow-2xs hover:bg-slate-100 active:scale-95 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
                >
                  {open ? '收起' : '拆分为新文件'}
                </button>
              </div>

              {open && (
                <div className="mt-3 space-y-2 border-t border-slate-200/80 pt-2.5 dark:border-slate-800">
                  <label className="flex flex-col gap-1 text-[11px] font-medium text-slate-500 dark:text-slate-400">
                    <span>新文件路径</span>
                    <input
                      type="text"
                      value={active.target}
                      onInput={(event) =>
                        setActive((current) =>
                          current ? { ...current, target: event.currentTarget.value } : current,
                        )
                      }
                      className="rounded-md border border-slate-200/80 bg-white px-2.5 py-1 font-mono text-xs text-slate-900 shadow-2xs outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500/20 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
                    />
                  </label>
                  {targetExists && (
                    <p className="text-[11px] text-amber-700 dark:text-amber-400">
                      目标文件已存在；确认后条目将追加至该文件，不会覆写现有内容。
                    </p>
                  )}
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      disabled={active.busy || target === '' || api.readOnly}
                      onClick={() => split(entry, target)}
                      className="rounded-lg bg-indigo-600 px-3 py-1 text-xs font-semibold text-white shadow-2xs hover:bg-indigo-700 active:scale-95 disabled:opacity-40"
                    >
                      {targetExists ? '确认追加到该文件' : '确认原子拆分'}
                    </button>
                    {active.error && (
                      <span className="text-xs text-rose-600 dark:text-rose-400">{active.error.message}</span>
                    )}
                  </div>
                </div>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
