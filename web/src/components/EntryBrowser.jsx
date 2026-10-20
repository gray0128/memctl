import { useEffect, useState } from 'preact/hooks'
import { isRevisionConflict, lineAt, matchCurrentEntry, replaceEntryFactLine } from '../edits.js'
import { sourceProtocol } from '../sources.js'
import { suggestSplitPath } from '../splits.js'
import { DraftDiff } from './DraftDiff.jsx'
import { FileEditor } from './FileEditor.jsx'

const PAGE_SIZE = 50

const EMPTY_FILTERS = { q: '', agent: '', device: '', file: '', from: '', to: '' }
const EMPTY_LABELS = {
  q: '关键词',
  agent: 'Agent',
  device: '设备',
  file: '文件路径',
  from: '起始日期',
  to: '结束日期',
}

function FilterField({ name, value, onChange }) {
  const isDate = name === 'from' || name === 'to'
  return (
    <label className="flex flex-col gap-1.5 text-xs font-medium text-slate-500 dark:text-slate-400">
      <span>{EMPTY_LABELS[name]}</span>
      <input
        type={isDate ? 'date' : 'text'}
        value={value}
        placeholder={isDate ? undefined : '全部'}
        onInput={(event) => onChange(name, event.currentTarget.value)}
        className="rounded-lg border border-slate-200/80 bg-white px-2.5 py-1.5 text-xs text-slate-900 shadow-2xs outline-none transition-all placeholder:text-slate-400 focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 dark:border-slate-800 dark:bg-slate-950 dark:text-slate-100 dark:placeholder:text-slate-600"
      />
    </label>
  )
}

function SourceList({ sources }) {
  const [copiedId, setCopiedId] = useState(null)

  function copySource(raw) {
    if (typeof navigator !== 'undefined' && navigator.clipboard) {
      navigator.clipboard.writeText(raw).then(() => {
        setCopiedId(raw)
        setTimeout(() => setCopiedId(null), 1500)
      }).catch(() => {})
    }
  }

  if (!sources || sources.length === 0) {
    return <span className="text-[11px] text-slate-400">来源未知</span>
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {sources.map((source) => {
        const protocol = sourceProtocol(source.raw)
        if (protocol === 'https') {
          return (
            <a
              key={source.raw}
              href={source.raw}
              target="_blank"
              rel="noreferrer noopener"
              title={source.raw}
              className="inline-flex items-center gap-1 rounded-md border border-indigo-200/60 bg-indigo-50/70 px-2 py-0.5 font-mono text-[11px] text-indigo-700 transition-colors hover:bg-indigo-100 hover:text-indigo-900 dark:border-indigo-900/50 dark:bg-indigo-950/40 dark:text-indigo-300 dark:hover:bg-indigo-900/60"
            >
              <svg className="h-3 w-3 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
              </svg>
              <span className="truncate max-w-[14rem]">{source.raw}</span>
            </a>
          )
        }
        return (
          <span
            key={source.raw}
            title={source.raw}
            className="inline-flex items-center gap-1 rounded-md border border-slate-200/60 bg-slate-100/70 px-2 py-0.5 font-mono text-[11px] text-slate-600 dark:border-slate-800 dark:bg-slate-800/60 dark:text-slate-300"
          >
            <span className="truncate max-w-[14rem]">{source.raw}</span>
            {protocol === 'agent' && (
              <button
                type="button"
                onClick={() => copySource(source.raw)}
                title="复制来源 URI"
                className="ml-0.5 inline-flex items-center justify-center rounded text-slate-500 hover:text-slate-800 dark:hover:text-slate-100"
              >
                {copiedId === source.raw ? (
                  <svg className="h-3 w-3 text-emerald-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.5">
                    <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                  </svg>
                ) : (
                  <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                    <path strokeLinecap="round" strokeLinejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                  </svg>
                )}
              </button>
            )}
          </span>
        )
      })}
    </div>
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
  const [showFileEditor, setShowFileEditor] = useState(false)
  const [showFilters, setShowFilters] = useState(false)

  const drafting = Boolean(active || conflict)

  useEffect(() => {
    if (drafting) return
    let cancelled = false
    setLoading(true)

    const params = { limit: PAGE_SIZE, offset }
    if (applied.q) params.q = applied.q
    if (applied.agent) params.agent = applied.agent
    if (applied.device) params.device = applied.device
    if (applied.file) params.file = applied.file
    if (applied.from) params.from = applied.from
    if (applied.to) params.to = applied.to

    api
      .entries(params)
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
  }, [api, applied, offset, refreshKey, drafting])

  function handleDraftChange(key, value) {
    setDraft((current) => ({ ...current, [key]: value }))
  }

  function applyFilters(event) {
    event.preventDefault()
    setOffset(0)
    setApplied({ ...draft })
  }

  function reset() {
    setDraft(EMPTY_FILTERS)
    setApplied(EMPTY_FILTERS)
    setOffset(0)
  }

  async function freshBaselines(entry) {
    const current = await api.file(entry.filePath)
    const snap = await api.snapshot()
    return {
      rawContent: current.rawContent,
      fileRevision: current.file.fileRevision,
      repoRevision: snap.revision,
    }
  }

  async function currentEntriesFor(entry) {
    const query = { file: entry.filePath, q: entry.factText, limit: 200, offset: 0 }
    const result = await api.entries(query)
    return result.items || []
  }

  async function handleConflict(entry, kind, proposedDraft, targetFile) {
    try {
      const current = matchCurrentEntry(await currentEntriesFor(entry), entry) || entry
      const fresh = await freshBaselines(current)
      const currentFact = lineAt(fresh.rawContent, current.lineNumber)
      setConflict({
        kind,
        entry: current,
        targetFile,
        proposedDraft,
        currentText: currentFact,
        draftText: proposedDraft,
        busy: false,
        error: null,
        retry: async () => {
          const latest = matchCurrentEntry(await currentEntriesFor(entry), entry)
          if (!latest) {
            throw new Error('条目已被改写，无法定位；请刷新后重建草稿。')
          }
          const freshAgain = await freshBaselines(latest)
          if (kind === 'edit') {
            return saveEdit(latest, proposedDraft, freshAgain)
          }
          if (kind === 'move') {
            return saveMove(latest, targetFile, freshAgain)
          }
          if (kind === 'remove') {
            return removeEntry(latest, freshAgain)
          }
        },
      })
    } catch (refreshError) {
      setActionError(refreshError)
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

  async function saveEdit(entry, newFactText, override) {
    setActive((current) => (current ? { ...current, busy: true, error: null } : current))
    setActionError(null)
    setNotice(null)
    try {
      const result = await api.patchEntry(
        entry.id,
        {
          filePath: entry.filePath,
          factText: newFactText,
          baseRevision: override ? override.repoRevision : page.revision,
        },
        override ? override.fileRevision : entry.fileRevision,
      )
      setActive(null)
      setConflict(null)
      setNotice(`已更新条目，提交 ${result.result.commit ? result.result.commit.slice(0, 12) : result.result.transactionId}`)
    } catch (saveError) {
      if (isRevisionConflict(saveError)) {
        setActive(null)
        await handleConflict(entry, 'edit', newFactText, '')
      } else {
        setActive((current) => (current ? { ...current, busy: false, error: saveError } : current))
      }
    }
  }

  async function saveMove(entry, targetFile, override) {
    setActive((current) => (current ? { ...current, busy: true, error: null } : current))
    setActionError(null)
    setNotice(null)
    try {
      const result = await api.patchEntry(
        entry.id,
        {
          filePath: entry.filePath,
          factText: entry.factText,
          moveToFilePath: targetFile,
          baseRevision: override ? override.repoRevision : page.revision,
        },
        override ? override.fileRevision : entry.fileRevision,
      )
      setActive(null)
      setConflict(null)
      setNotice(`已移动条目到 ${targetFile}，提交 ${result.result.commit ? result.result.commit.slice(0, 12) : result.result.transactionId}`)
    } catch (moveError) {
      if (isRevisionConflict(moveError)) {
        setActive(null)
        await handleConflict(entry, 'move', entry.factText, targetFile)
      } else {
        setActive((current) => (current ? { ...current, busy: false, error: moveError } : current))
      }
    }
  }

  async function removeEntry(entry, override) {
    setActionError(null)
    setNotice(null)
    try {
      const result = await api.deleteEntry(entry.id, {
        filePath: entry.filePath,
        fileRevision: override ? override.fileRevision : entry.fileRevision,
        baseRevision: override ? override.repoRevision : page.revision,
      })
      setConfirming('')
      setConflict(null)
      setNotice(`已删除条目，提交 ${result.commit ? result.commit.slice(0, 12) : result.transactionId}`)
    } catch (deleteError) {
      if (isRevisionConflict(deleteError)) {
        setConfirming('')
        await handleConflict(entry, 'remove', '', '')
      } else {
        setActionError(deleteError)
      }
    }
  }

  const items = page ? page.items : []
  const total = page ? page.total : 0
  // Entries hidden by an expired `until` are excluded from `total` by the
  // server. Surface the count so an empty result is not mistaken for "nothing
  // was ever recorded". See docs/10-memory-lifecycle.md section 3.4.
  const hiddenExpired = (snapshot && snapshot.hiddenExpired) || 0
  const end = Math.min(offset + PAGE_SIZE, total)

  return (
    <section className="space-y-6">
      {/* Top Header & Editor Toggle */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-slate-900 dark:text-white">记忆条目检索</h1>
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            跨文件要点检索、事实编辑、改链迁移与原子删除
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowFileEditor(!showFileEditor)}
          className={`inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-semibold shadow-2xs transition-all active:scale-95 ${
            showFileEditor
              ? 'border-indigo-300 bg-indigo-50 text-indigo-700 dark:border-indigo-800 dark:bg-indigo-950/60 dark:text-indigo-300'
              : 'border-slate-300/80 bg-white text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700'
          }`}
        >
          <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
            <path strokeLinecap="round" strokeLinejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
          </svg>
          <span>{showFileEditor ? '收起文件编辑器' : '打开文件原文编辑器'}</span>
        </button>
      </div>

      {/* Raw File Editor Panel */}
      {showFileEditor && (
        <div className="rounded-2xl border border-indigo-200/80 bg-white p-5 shadow-xs dark:border-indigo-900/60 dark:bg-slate-900">
          <FileEditor api={api} files={(snapshot && snapshot.files) || []} />
        </div>
      )}

      {/* Global Action Notices */}
      {notice && (
        <div className="flex items-center gap-2 rounded-xl border border-emerald-200/80 bg-emerald-50/80 px-4 py-2.5 text-xs font-medium text-emerald-800 shadow-2xs dark:border-emerald-800/60 dark:bg-emerald-950/40 dark:text-emerald-300">
          <svg className="h-4 w-4 shrink-0 text-emerald-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.5">
            <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
          </svg>
          <span>{notice}</span>
        </div>
      )}

      {actionError && (
        <div className="flex items-center gap-2 rounded-xl border border-rose-200/80 bg-rose-50/80 px-4 py-2.5 text-xs font-medium text-rose-800 shadow-2xs dark:border-rose-800/60 dark:bg-rose-950/40 dark:text-rose-300">
          <svg className="h-4 w-4 shrink-0 text-rose-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span>{actionError.message}</span>
        </div>
      )}

      {/* Revision Conflict Banner */}
      {conflict && (
        <div className="space-y-3 rounded-xl border border-amber-300/80 bg-amber-50/90 p-4 shadow-xs dark:border-amber-800/80 dark:bg-amber-950/40">
          <div className="flex items-center gap-2">
            <svg className="h-5 w-5 text-amber-600 dark:text-amber-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            <h4 className="text-xs font-bold text-amber-900 dark:text-amber-200">
              文件修订版本已过期（412 Conflict）· 草稿内容已完好保留
            </h4>
          </div>
          <p className="text-xs text-amber-800/90 dark:text-amber-300/90">
            服务端当前内容与您的待提交草稿存在分歧，请对比下方差异后决定重试或放弃：
          </p>
          <div className="rounded-lg bg-white p-2 shadow-inner dark:bg-slate-900">
            <DraftDiff currentText={conflict.currentText} draftText={conflict.draftText} />
          </div>
          {conflict.error && (
            <p className="text-xs font-semibold text-rose-700 dark:text-rose-300">
              重试失败：{conflict.error.message}
            </p>
          )}
          <div className="flex items-center gap-2 pt-1">
            <button
              type="button"
              onClick={runConflictRetry}
              disabled={conflict.busy}
              className="rounded-lg bg-amber-600 px-3.5 py-1.5 text-xs font-semibold text-white shadow-2xs hover:bg-amber-700 active:scale-95 disabled:opacity-40"
            >
              {conflict.busy ? '重试提交中…' : '以当前草稿重新提交'}
            </button>
            <button
              type="button"
              onClick={() => setConflict(null)}
              className="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 shadow-2xs hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
            >
              放弃草稿
            </button>
          </div>
        </div>
      )}

      {/* Modern Search Command Bar */}
      <form onSubmit={applyFilters} className="space-y-3">
        <div className="relative flex items-center">
          <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5 text-slate-400">
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
          </div>
          <input
            type="text"
            value={draft.q}
            onInput={(event) => handleDraftChange('q', event.currentTarget.value)}
            placeholder="搜索记忆事实关键词 (Enter 检索)..."
            className="w-full rounded-xl border border-slate-200/80 bg-white py-2.5 pr-28 pl-10 text-sm text-slate-900 shadow-2xs outline-none transition-all placeholder:text-slate-400 focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-100 dark:placeholder:text-slate-600"
          />
          <div className="absolute inset-y-0 right-0 flex items-center gap-1.5 pr-2">
            <button
              type="button"
              onClick={() => setShowFilters(!showFilters)}
              className={`rounded-lg px-2.5 py-1 text-xs font-medium transition-colors ${
                showFilters || draft.agent || draft.device || draft.file || draft.from || draft.to
                  ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-950/60 dark:text-indigo-300 font-semibold'
                  : 'text-slate-500 hover:bg-slate-100 hover:text-slate-700 dark:text-slate-400 dark:hover:bg-slate-800'
              }`}
            >
              筛选
            </button>
            <button
              type="submit"
              className="rounded-lg bg-indigo-600 px-3 py-1 text-xs font-semibold text-white shadow-2xs hover:bg-indigo-700 active:scale-95"
            >
              查询
            </button>
          </div>
        </div>

        {/* Collapsible Advanced Filters Drawer */}
        {showFilters && (
          <div className="rounded-xl border border-slate-200/70 bg-white/70 p-4 shadow-2xs backdrop-blur-xs dark:border-slate-800/70 dark:bg-slate-900/60">
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
              <FilterField name="agent" value={draft.agent} onChange={handleDraftChange} />
              <FilterField name="device" value={draft.device} onChange={handleDraftChange} />
              <FilterField name="file" value={draft.file} onChange={handleDraftChange} />
              <FilterField name="from" value={draft.from} onChange={handleDraftChange} />
              <FilterField name="to" value={draft.to} onChange={handleDraftChange} />
            </div>
            <div className="mt-3 flex items-center justify-end gap-2 border-t border-slate-100 pt-3 dark:border-slate-800/60">
              <button
                type="button"
                onClick={reset}
                className="rounded-lg border border-slate-200 px-3 py-1 text-xs font-medium text-slate-600 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
              >
                重置条件
              </button>
              <button
                type="submit"
                className="rounded-lg bg-indigo-600 px-3 py-1 text-xs font-semibold text-white shadow-2xs hover:bg-indigo-700 active:scale-95"
              >
                应用筛选
              </button>
            </div>
          </div>
        )}
      </form>

      {/* Query Status Bar */}
      <div className="flex items-center justify-between text-xs text-slate-500 dark:text-slate-400">
        <span className="flex items-center gap-2">
          <span>共找到 <strong className="text-slate-800 dark:text-slate-200 font-mono">{total}</strong> 条记忆事实</span>
          {hiddenExpired > 0 && (
            <span
              className="rounded-full border border-amber-200/80 bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-300"
              title="`until` 已到期的条目从读取中隐藏，正文与 Git 历史仍在"
            >
              另有 {hiddenExpired} 条已过期并隐藏
            </span>
          )}
          {loading && (
            <span className="inline-flex items-center gap-1 text-indigo-600 dark:text-indigo-400">
              <svg className="h-3 w-3 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              检索中…
            </span>
          )}
        </span>
        <span className="font-mono text-slate-400">
          {total === 0 ? '0' : `${offset + 1}–${end}`}
        </span>
      </div>

      {error && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-xs font-medium text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300">
          无法读取条目：{error.message}
        </div>
      )}

      {page && items.length === 0 && !error && (
        <div className="rounded-2xl border border-dashed border-slate-300 p-12 text-center dark:border-slate-800">
          <svg className="mx-auto h-8 w-8 text-slate-400 dark:text-slate-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5">
            <path strokeLinecap="round" strokeLinejoin="round" d="M9.172 16.172a4 4 0 015.656 0M9 10h.01M15 10h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <p className="mt-3 text-sm font-medium text-slate-600 dark:text-slate-300">未找到匹配的记忆条目</p>
          <p className="mt-1 text-xs text-slate-400 dark:text-slate-500">可尝试调整关键词或放宽筛选维度</p>
        </div>
      )}

      {/* Entry Cards List */}
      <ul className="space-y-3">
        {items.map((entry) => {
          const editing = active && active.entry.id === entry.id && active.mode === 'edit'
          const moving = active && active.entry.id === entry.id && active.mode === 'move'

          return (
            <li
              key={entry.id}
              className="group relative overflow-hidden rounded-xl border border-slate-200/80 bg-white p-4 shadow-2xs transition-all duration-150 hover:border-slate-300/90 hover:shadow-xs dark:border-slate-800/80 dark:bg-slate-900/80 dark:hover:border-slate-700"
            >
              {/* Left Accent Bar */}
              <div className="absolute inset-y-0 left-0 w-1 bg-gradient-to-b from-indigo-500/80 to-indigo-600/20 opacity-0 transition-opacity group-hover:opacity-100" />

              {!editing && !moving && (
                <div className="space-y-2.5">
                  {/* Fact Content */}
                  <p className="text-sm font-normal leading-relaxed text-slate-900 dark:text-slate-100">
                    {entry.factText}
                  </p>

                  {/* Metadata Badges */}
                  <div className="flex flex-wrap items-center gap-2 pt-1 text-xs text-slate-500 dark:text-slate-400">
                    <span className="inline-flex items-center gap-1 rounded-md bg-slate-100/80 px-2 py-0.5 font-mono text-[11px] font-medium text-slate-600 dark:bg-slate-800/80 dark:text-slate-300">
                      <svg className="h-3 w-3 text-slate-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z" />
                      </svg>
                      {entry.filePath}:{entry.lineNumber}
                    </span>

                    {entry.addedDate && (
                      <span className="inline-flex items-center gap-1 rounded-md bg-slate-100/80 px-2 py-0.5 font-mono text-[11px] text-slate-500 dark:bg-slate-800/80 dark:text-slate-400">
                        <svg className="h-3 w-3 text-slate-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
                        </svg>
                        {entry.addedDate}
                      </span>
                    )}

                    <SourceList sources={entry.sources} />
                  </div>

                  {/* Actions Ribbon */}
                  <div className="flex items-center justify-end gap-2 border-t border-slate-100 pt-2.5 dark:border-slate-800/60">
                    <button
                      type="button"
                      disabled={api.readOnly}
                      onClick={() => {
                        setNotice(null)
                        setActionError(null)
                        setActive({ entry, mode: 'edit', draft: entry.factText, target: '', busy: false, error: null })
                      }}
                      className="rounded-lg border border-slate-200/80 px-2.5 py-1 text-xs font-medium text-slate-700 shadow-2xs hover:bg-slate-50 active:scale-95 disabled:opacity-40 dark:border-slate-700/80 dark:text-slate-300 dark:hover:bg-slate-800"
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
                      className="rounded-lg border border-slate-200/80 px-2.5 py-1 text-xs font-medium text-slate-700 shadow-2xs hover:bg-slate-50 active:scale-95 disabled:opacity-40 dark:border-slate-700/80 dark:text-slate-300 dark:hover:bg-slate-800"
                    >
                      迁移
                    </button>
                    {confirming === entry.id ? (
                      <div className="flex items-center gap-1.5">
                        <button
                          type="button"
                          disabled={api.readOnly}
                          onClick={() => removeEntry(entry)}
                          className="rounded-lg bg-rose-600 px-2.5 py-1 text-xs font-semibold text-white shadow-2xs hover:bg-rose-700 active:scale-95 disabled:opacity-40"
                        >
                          确认删除
                        </button>
                        <button
                          type="button"
                          onClick={() => setConfirming('')}
                          className="rounded-lg border border-slate-200 px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
                        >
                          取消
                        </button>
                      </div>
                    ) : (
                      <button
                        type="button"
                        disabled={api.readOnly}
                        onClick={() => setConfirming(entry.id)}
                        className="rounded-lg border border-rose-200/80 px-2.5 py-1 text-xs font-medium text-rose-600 hover:bg-rose-50 active:scale-95 disabled:opacity-40 dark:border-rose-900/60 dark:text-rose-400 dark:hover:bg-rose-950/40"
                      >
                        删除
                      </button>
                    )}
                  </div>
                </div>
              )}

              {/* Inline Fact Text Editor */}
              {editing && (
                <div className="space-y-3">
                  <div className="flex items-center justify-between text-xs font-medium text-slate-500 dark:text-slate-400">
                    <span>编辑条目内容</span>
                    <span className="font-mono text-[11px]">{entry.filePath}:{entry.lineNumber}</span>
                  </div>
                  <textarea
                    value={active.draft}
                    rows={3}
                    onInput={(event) =>
                      setActive((current) =>
                        current ? { ...current, draft: event.currentTarget.value } : current,
                      )
                    }
                    className="w-full rounded-xl border border-slate-200/80 bg-white p-3 text-sm text-slate-900 shadow-inner outline-none transition-all focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                  />
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        disabled={active.busy || active.draft.trim() === ''}
                        onClick={() => saveEdit(entry, active.draft)}
                        className="rounded-lg bg-indigo-600 px-3.5 py-1.5 text-xs font-semibold text-white shadow-2xs hover:bg-indigo-700 active:scale-95 disabled:opacity-40"
                      >
                        {active.busy ? '正在保存…' : '保存修改'}
                      </button>
                      <button
                        type="button"
                        onClick={() => setActive(null)}
                        className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
                      >
                        取消
                      </button>
                    </div>
                    {active.error && (
                      <span className="text-xs font-medium text-rose-600 dark:text-rose-400">
                        {active.error.message}
                      </span>
                    )}
                  </div>
                </div>
              )}

              {/* Inline Move Form */}
              {moving && (
                <div className="space-y-3">
                  <div className="text-xs text-slate-500 dark:text-slate-400">
                    将此事实迁移至目标 Markdown 文件，若目标不存在将自动创建：
                  </div>
                  <label className="flex flex-col gap-1.5 text-xs font-medium text-slate-500 dark:text-slate-400">
                    <span>目标文件相对路径</span>
                    <input
                      type="text"
                      value={active.target}
                      onInput={(event) =>
                        setActive((current) =>
                          current ? { ...current, target: event.currentTarget.value } : current,
                        )
                      }
                      className="rounded-lg border border-slate-200/80 bg-white px-3 py-1.5 font-mono text-xs text-slate-900 shadow-2xs outline-none transition-all focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                    />
                  </label>
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        disabled={
                          active.busy ||
                          active.target.trim() === '' ||
                          active.target.trim() === entry.filePath
                        }
                        onClick={() => saveMove(entry, active.target.trim())}
                        className="rounded-lg bg-indigo-600 px-3.5 py-1.5 text-xs font-semibold text-white shadow-2xs hover:bg-indigo-700 active:scale-95 disabled:opacity-40"
                      >
                        {active.busy ? '正在迁移…' : '确认迁移'}
                      </button>
                      <button
                        type="button"
                        onClick={() => setActive(null)}
                        className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
                      >
                        取消
                      </button>
                    </div>
                    {active.error && (
                      <span className="text-xs font-medium text-rose-600 dark:text-rose-400">
                        {active.error.message}
                      </span>
                    )}
                  </div>
                </div>
              )}
            </li>
          )
        })}
      </ul>

      {/* Modern Paginator */}
      <div className="flex items-center justify-between border-t border-slate-200/80 pt-4 dark:border-slate-800/80">
        <span className="text-xs text-slate-400">
          每页显示 {PAGE_SIZE} 条
        </span>
        <div className="flex items-center gap-2">
          <button
            type="button"
            disabled={offset === 0}
            onClick={() => setOffset((current) => Math.max(0, current - PAGE_SIZE))}
            className="inline-flex items-center gap-1 rounded-lg border border-slate-300/80 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 shadow-2xs transition-all hover:bg-slate-50 active:scale-95 disabled:cursor-not-allowed disabled:opacity-40 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700"
          >
            <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M15 19l-7-7 7-7" />
            </svg>
            上一页
          </button>
          <button
            type="button"
            disabled={end >= total}
            onClick={() => setOffset((current) => current + PAGE_SIZE)}
            className="inline-flex items-center gap-1 rounded-lg border border-slate-300/80 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 shadow-2xs transition-all hover:bg-slate-50 active:scale-95 disabled:cursor-not-allowed disabled:opacity-40 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700"
          >
            下一页
            <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" />
            </svg>
          </button>
        </div>
      </div>
    </section>
  )
}
