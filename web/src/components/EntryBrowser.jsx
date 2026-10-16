import { useEffect, useState } from 'preact/hooks'
import { isRevisionConflict, lineAt, replaceEntryFactLine } from '../edits.js'
import { suggestSplitPath } from '../splits.js'
import { DraftDiff } from './DraftDiff.jsx'
import { FileEditor } from './FileEditor.jsx'

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

// EntryBrowser is the entry search and edit view. It sends the documented
// q/agent/device/file/from/to filters to GET /api/v1/entries, paginates the
// result and edits each entry through the managed entry routes. A stale
// revision keeps the draft and shows the server's current line next to it.
// See docs/04-ui-and-cli-specification.md section 4.
export function EntryBrowser({ api, refreshKey, snapshot }) {
  const [draft, setDraft] = useState(EMPTY_FILTERS)
  const [applied, setApplied] = useState(EMPTY_FILTERS)
  const [offset, setOffset] = useState(0)
  const [page, setPage] = useState(null)
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)
  const [active, setActive] = useState(null)
  const [confirming, setConfirming] = useState('')
  const [conflict, setConflict] = useState(null)
  const [actionError, setActionError] = useState(null)
  const [notice, setNotice] = useState(null)

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

  const repoRevision = page ? page.revision : ''

  // freshBaselines re-reads the file and the repository revision that a retry
  // must use after a 412. The draft itself is never reconstructed from server
  // state; it is passed in by the caller.
  async function freshBaselines(entry) {
    const current = await api.file(entry.filePath)
    const snapshotNow = await api.snapshot()
    return { rawContent: current.rawContent, fileRevision: current.file.fileRevision, repoRevision: snapshotNow.revision }
  }

  // handleConflict keeps the draft in the conflict panel and offers an explicit
  // retry against the fresh baselines. It never reapplies without confirmation.
  async function handleConflict(entry, error, description) {
    try {
      const fresh = await freshBaselines(entry)
      setConflict({
        message: description.message,
        currentLabel: description.currentLabel,
        draftLabel: description.draftLabel,
        currentText: description.currentText(fresh.rawContent),
        draftText: description.draftText,
        actionLabel: description.actionLabel,
        retry: () => description.retry(fresh),
      })
    } catch (refreshError) {
      setActionError(refreshError)
    }
  }

  function reportWrite(result) {
    setNotice({
      tone: 'ok',
      message: `已提交 ${result.commit ? result.commit.slice(0, 12) : result.transactionId}`,
    })
  }

  async function saveEdit(entry, factText, override) {
    setActive((current) => (current ? { ...current, busy: true, error: null } : current))
    try {
      const result = await api.patchEntry(
        entry.id,
        { filePath: entry.filePath, factText },
        override ? override.fileRevision : entry.fileRevision,
      )
      setActive(null)
      setConflict(null)
      reportWrite(result.result)
    } catch (requestError) {
      if (isRevisionConflict(requestError)) {
        setActive(null)
        await handleConflict(entry, requestError, {
          message: '条目所在文件已被其他写入改变，草稿已保留。',
          currentLabel: `服务器当前第 ${entry.lineNumber} 行`,
          draftLabel: '草稿条目',
          currentText: (rawContent) => lineAt(rawContent, entry.lineNumber),
          draftText: replaceEntryFactLine(entry.rawLine, factText),
          actionLabel: '用草稿覆盖当前',
          retry: (fresh) => saveEdit(entry, factText, fresh),
        })
      } else {
        setActive((current) => (current ? { ...current, busy: false, error: requestError } : current))
      }
    }
  }

  async function saveMove(entry, target, override) {
    setActive((current) => (current ? { ...current, busy: true, error: null } : current))
    try {
      const result = await api.patchEntry(
        entry.id,
        {
          filePath: entry.filePath,
          factText: entry.factText,
          moveToFilePath: target,
          baseRevision: override ? override.repoRevision : repoRevision,
        },
        override ? override.fileRevision : entry.fileRevision,
      )
      setActive(null)
      setConflict(null)
      reportWrite(result.result)
    } catch (requestError) {
      if (isRevisionConflict(requestError)) {
        setActive(null)
        await handleConflict(entry, requestError, {
          message: '条目或仓库基线已过期，草稿未提交。',
          currentLabel: `服务器当前第 ${entry.lineNumber} 行`,
          draftLabel: '草稿：从源文件移除',
          currentText: (rawContent) => lineAt(rawContent, entry.lineNumber),
          draftText: '',
          actionLabel: '用草稿重试移动',
          retry: (fresh) => saveMove(entry, target, fresh),
        })
      } else {
        setActive((current) => (current ? { ...current, busy: false, error: requestError } : current))
      }
    }
  }

  async function removeEntry(entry, override) {
    setConfirming('')
    setNotice(null)
    try {
      const result = await api.deleteEntry(
        entry.id,
        entry.filePath,
        override ? override.fileRevision : entry.fileRevision,
      )
      setConflict(null)
      reportWrite(result)
    } catch (requestError) {
      if (isRevisionConflict(requestError)) {
        await handleConflict(entry, requestError, {
          message: '条目所在文件已被其他写入改变，未删除。',
          currentLabel: `服务器当前第 ${entry.lineNumber} 行`,
          draftLabel: '草稿：删除该行',
          currentText: (rawContent) => lineAt(rawContent, entry.lineNumber),
          draftText: '',
          actionLabel: '重试删除',
          retry: (fresh) => removeEntry(entry, fresh),
        })
      } else {
        setActionError(requestError)
      }
    }
  }

  const total = page ? page.total : 0
  const items = page ? page.items : []
  const end = Math.min(offset + PAGE_SIZE, total)

  return (
    <section className="space-y-6">
      <h1 className="text-xl font-semibold">条目浏览</h1>

      <FileEditor api={api} files={snapshot ? snapshot.files : []} />

      {notice && <p className="text-sm text-emerald-700 dark:text-emerald-300">{notice.message}</p>}
      {conflict && (
        <div className="space-y-2 rounded border border-amber-300 bg-amber-50 p-3 dark:border-amber-900 dark:bg-amber-900/20">
          <p className="text-sm text-amber-900 dark:text-amber-100">{conflict.message}</p>
          <div className="grid gap-2 text-xs text-slate-600 dark:text-slate-300 sm:grid-cols-2">
            <span>{conflict.currentLabel}</span>
            <span>{conflict.draftLabel}</span>
          </div>
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
              {conflict.actionLabel}
            </button>
          </div>
        </div>
      )}
      {actionError && (
        <p className="text-sm text-rose-700 dark:text-rose-300">条目操作失败：{actionError.message}</p>
      )}

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
        <span>{total === 0 ? '0' : `${offset + 1}–${end}`}</span>
      </div>

      {page && items.length === 0 && !error && (
        <p className="text-sm text-slate-500 dark:text-slate-400">没有匹配的条目。</p>
      )}

      <ul className="space-y-2">
        {items.map((entry) => {
          const editing = active && active.entry.id === entry.id && active.mode === 'edit'
          const moving = active && active.entry.id === entry.id && active.mode === 'move'
          return (
            <li
              key={entry.id}
              className="rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900"
            >
              {!editing && !moving && (
                <>
                  <p className="text-sm text-slate-900 dark:text-slate-100">{entry.factText}</p>
                  <div className="mt-1 flex flex-wrap items-baseline gap-x-3 text-xs text-slate-500 dark:text-slate-400">
                    <span className="font-mono">
                      {entry.filePath}:{entry.lineNumber}
                    </span>
                    {entry.addedDate && <span>{entry.addedDate}</span>}
                    <SourceList sources={entry.sources} />
                  </div>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <button
                      type="button"
                      disabled={api.readOnly}
                      onClick={() => {
                        setNotice(null)
                        setActionError(null)
                        setActive({ entry, mode: 'edit', draft: entry.factText, target: '', busy: false, error: null })
                      }}
                      className="rounded border border-slate-300 px-2 py-1 text-xs disabled:opacity-40 dark:border-slate-700"
                    >
                      编辑
                    </button>
                    <button
                      type="button"
                      disabled={api.readOnly}
                      onClick={() => {
                        setNotice(null)
                        setActionError(null)
                        setActive({
                          entry,
                          mode: 'move',
                          draft: entry.factText,
                          target: suggestSplitPath(entry.filePath, entry),
                          busy: false,
                          error: null,
                        })
                      }}
                      className="rounded border border-slate-300 px-2 py-1 text-xs disabled:opacity-40 dark:border-slate-700"
                    >
                      移动
                    </button>
                    {confirming === entry.id ? (
                      <>
                        <button
                          type="button"
                          onClick={() => removeEntry(entry)}
                          className="rounded bg-rose-600 px-2 py-1 text-xs font-medium text-white"
                        >
                          确认删除
                        </button>
                        <button
                          type="button"
                          onClick={() => setConfirming('')}
                          className="rounded border border-slate-300 px-2 py-1 text-xs dark:border-slate-700"
                        >
                          取消
                        </button>
                      </>
                    ) : (
                      <button
                        type="button"
                        disabled={api.readOnly}
                        onClick={() => setConfirming(entry.id)}
                        className="rounded border border-rose-300 px-2 py-1 text-xs text-rose-700 disabled:opacity-40 dark:border-rose-900 dark:text-rose-300"
                      >
                        删除
                      </button>
                    )}
                  </div>
                </>
              )}

              {editing && (
                <div className="space-y-2">
                  <textarea
                    value={active.draft}
                    rows={3}
                    onInput={(event) =>
                      setActive((current) =>
                        current ? { ...current, draft: event.currentTarget.value } : current,
                      )
                    }
                    className="w-full rounded border border-slate-300 bg-white p-2 text-sm text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                  />
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      disabled={active.busy || active.draft.trim() === ''}
                      onClick={() => saveEdit(entry, active.draft)}
                      className="rounded bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
                    >
                      保存条目
                    </button>
                    <button
                      type="button"
                      onClick={() => setActive(null)}
                      className="rounded border border-slate-300 px-3 py-1.5 text-sm dark:border-slate-700"
                    >
                      取消
                    </button>
                  </div>
                  {active.error && (
                    <p className="text-sm text-rose-700 dark:text-rose-300">{active.error.message}</p>
                  )}
                </div>
              )}

              {moving && (
                <div className="space-y-2">
                  <p className="text-sm text-slate-500 dark:text-slate-400">移动条目到另一个文件；目标不存在时创建该文件。</p>
                  <label className="flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
                    目标文件路径
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
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      disabled={
                        active.busy ||
                        active.target.trim() === '' ||
                        active.target.trim() === entry.filePath
                      }
                      onClick={() => saveMove(entry, active.target.trim())}
                      className="rounded bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
                    >
                      移动条目
                    </button>
                    <button
                      type="button"
                      onClick={() => setActive(null)}
                      className="rounded border border-slate-300 px-3 py-1.5 text-sm dark:border-slate-700"
                    >
                      取消
                    </button>
                  </div>
                  {active.error && (
                    <p className="text-sm text-rose-700 dark:text-rose-300">{active.error.message}</p>
                  )}
                </div>
              )}
            </li>
          )
        })}
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
