import { useState } from 'preact/hooks'
import { isRevisionConflict } from '../edits.js'
import { DraftDiff } from './DraftDiff.jsx'

function StatusLine({ status }) {
  if (!status) return null
  const tones = {
    info: 'bg-slate-50 text-slate-700 border-slate-200 dark:bg-slate-800/60 dark:text-slate-300 dark:border-slate-700',
    ok: 'bg-emerald-50 text-emerald-800 border-emerald-200 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-800',
    warn: 'bg-amber-50 text-amber-800 border-amber-200 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-800',
    error: 'bg-rose-50 text-rose-800 border-rose-200 dark:bg-rose-950/40 dark:text-rose-300 dark:border-rose-800',
  }
  return (
    <div className={`flex items-center gap-2 rounded-lg border px-3 py-2 text-xs font-medium ${tones[status.tone] || tones.info}`}>
      <span>{status.message}</span>
    </div>
  )
}

// FileEditor is the raw-text editor of the entry browser. It loads one managed
// file, saves through PUT /api/v1/files/{path...} with the file revision it
// read, and on a 412 REVISION_MISMATCH keeps the draft and shows the server's
// current content next to it instead of overwriting. See
// docs/04-ui-and-cli-specification.md section 4.
export function FileEditor({ api, files }) {
  const [path, setPath] = useState('')
  const [loaded, setLoaded] = useState(null)
  const [draft, setDraft] = useState('')
  const [status, setStatus] = useState(null)
  const [conflict, setConflict] = useState(null)
  const [busy, setBusy] = useState(false)

  const dirty = loaded ? draft !== loaded.rawContent : false

  async function load(target = path) {
    const value = String(target).trim()
    if (value === '') {
      setStatus({ tone: 'error', message: '请先填写文件相对路径。' })
      return
    }
    setBusy(true)
    setConflict(null)
    setStatus(null)
    try {
      const result = await api.file(value)
      setPath(value)
      setLoaded({
        path: value,
        fileRevision: result.file.fileRevision,
        rawContent: result.rawContent,
      })
      setDraft(result.rawContent)
      setStatus({ tone: 'info', message: `已载入 ${value}（修订 ${result.file.fileRevision.slice(0, 10)}）` })
    } catch (error) {
      if (error.status === 404 && error.code === 'NOT_FOUND') {
        setPath(value)
        setLoaded({ path: value, fileRevision: 'absent', rawContent: '' })
        setDraft('')
        setStatus({ tone: 'info', message: `${value} 尚不存在，保存将原子新建该文件。` })
      } else {
        setStatus({ tone: 'error', message: error.message })
      }
    } finally {
      setBusy(false)
    }
  }

  async function save(fileRevision) {
    if (!loaded) return
    const baseline = fileRevision || loaded.fileRevision
    setBusy(true)
    setStatus(null)
    try {
      const result = await api.putFile(loaded.path, draft, baseline)
      const current = await api.file(loaded.path)
      setPath(loaded.path)
      setLoaded({
        path: loaded.path,
        fileRevision: current.file.fileRevision,
        rawContent: current.rawContent,
      })
      setDraft(current.rawContent)
      setConflict(null)
      setStatus({
        tone: 'ok',
        message: `已提交修改，提交哈希 ${result.commit ? result.commit.slice(0, 12) : result.transactionId}`,
      })
    } catch (error) {
      if (isRevisionConflict(error)) {
        try {
          const current = await api.file(loaded.path)
          setConflict({ rawContent: current.rawContent, fileRevision: current.file.fileRevision })
          setStatus({ tone: 'warn', message: '文件已被外部或其他写入改写，您的草稿已完整保留。' })
        } catch (refreshError) {
          setStatus({ tone: 'error', message: refreshError.message })
        }
      } else {
        setStatus({ tone: 'error', message: error.message })
      }
    } finally {
      setBusy(false)
    }
  }

  function useCurrent() {
    if (!conflict || !loaded) return
    setLoaded({ path: loaded.path, fileRevision: conflict.fileRevision, rawContent: conflict.rawContent })
    setDraft(conflict.rawContent)
    setConflict(null)
    setStatus({ tone: 'info', message: '已重新载入服务器当前最新内容，已放弃本次草稿。' })
  }

  const lineCount = draft ? draft.split('\n').length : 0

  return (
    <section className="space-y-4">
      {/* Editor Header */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b border-slate-200/70 pb-3 dark:border-slate-800/70">
        <div className="flex items-center gap-2">
          <div className="flex h-6 w-6 items-center justify-center rounded-md bg-indigo-50 text-indigo-600 dark:bg-indigo-950/50 dark:text-indigo-400">
            <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
            </svg>
          </div>
          <h2 className="text-sm font-bold tracking-tight text-slate-900 dark:text-white">文件原文受管编辑</h2>
        </div>
        <span className="text-xs text-slate-500 dark:text-slate-400">
          基于安全修订基线（Optimistic Locking），防止静默覆写
        </span>
      </div>

      {/* Target Selector Bar */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <div className="relative flex-1">
          <input
            type="text"
            list="memctl-file-list"
            value={path}
            placeholder="输入或选择文件路径，如 MEMORY.md 或 notes/design.md"
            onInput={(event) => setPath(event.currentTarget.value)}
            className="w-full rounded-lg border border-slate-200/80 bg-white px-3 py-1.5 font-mono text-xs text-slate-900 shadow-2xs outline-none transition-all placeholder:text-slate-400 focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
          />
          <datalist id="memctl-file-list">
            {(files || []).map((file) => (
              <option key={file.relPath} value={file.relPath} />
            ))}
          </datalist>
        </div>
        <button
          type="button"
          onClick={() => load()}
          disabled={busy}
          className="rounded-lg bg-slate-900 px-3.5 py-1.5 text-xs font-semibold text-white shadow-2xs hover:bg-slate-800 active:scale-95 disabled:opacity-40 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
        >
          {busy ? '读取中…' : '载入文件'}
        </button>
      </div>

      {/* Code Textarea & Workspace */}
      {loaded && (
        <div className="space-y-3 rounded-xl border border-slate-200/80 bg-slate-950 p-1 shadow-inner dark:border-slate-800">
          {/* Editor Meta Bar */}
          <div className="flex items-center justify-between px-3 py-1.5 text-[11px] font-mono text-slate-400 border-b border-slate-800/80">
            <span className="truncate max-w-xs">{loaded.path}</span>
            <div className="flex items-center gap-3">
              <span>{lineCount} 行</span>
              <span>{draft.length} 字符</span>
              {dirty ? (
                <span className="rounded-full bg-amber-900/60 px-2 py-0.2 font-sans font-semibold text-amber-300">
                  已修改 (未保存)
                </span>
              ) : (
                <span className="rounded-full bg-emerald-900/50 px-2 py-0.2 font-sans text-emerald-300">
                  同步基线
                </span>
              )}
            </div>
          </div>

          <textarea
            value={draft}
            rows={16}
            onInput={(event) => setDraft(event.currentTarget.value)}
            spellCheck={false}
            className="w-full bg-transparent p-3 font-mono text-xs leading-relaxed text-slate-100 outline-none resize-y selection:bg-indigo-500/30"
          />

          <div className="flex flex-wrap items-center justify-between gap-2 bg-slate-900 px-3 py-2 rounded-b-lg border-t border-slate-800/80">
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => save()}
                disabled={busy || !dirty || api.readOnly}
                className="rounded-lg bg-indigo-600 px-3.5 py-1.5 text-xs font-semibold text-white shadow-xs hover:bg-indigo-500 active:scale-95 disabled:cursor-not-allowed disabled:opacity-40"
              >
                {busy ? '正在提交…' : '保存更改'}
              </button>
              <button
                type="button"
                onClick={() => {
                  setDraft(loaded.rawContent)
                  setConflict(null)
                  setStatus(null)
                }}
                disabled={busy || !dirty}
                className="rounded-lg border border-slate-700 bg-slate-800 px-3 py-1.5 text-xs font-medium text-slate-300 hover:bg-slate-700 active:scale-95 disabled:opacity-40"
              >
                放弃修改
              </button>
            </div>
            {api.readOnly && (
              <span className="text-xs text-slate-400">只读保护：写权限已禁用</span>
            )}
          </div>
        </div>
      )}

      {/* Revision Conflict Area */}
      {conflict && (
        <div className="space-y-3 rounded-xl border border-amber-300/80 bg-amber-50/90 p-4 shadow-xs dark:border-amber-800/80 dark:bg-amber-950/40">
          <p className="text-xs font-bold text-amber-900 dark:text-amber-200">
            文件版本基线不一致：服务器当前内容 vs 您待保存的草稿差异
          </p>
          <div className="rounded-lg bg-white p-2 shadow-inner dark:bg-slate-900">
            <DraftDiff currentText={conflict.rawContent} draftText={draft} />
          </div>
          <div className="flex flex-wrap gap-2 pt-1">
            <button
              type="button"
              onClick={useCurrent}
              className="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 shadow-2xs hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
            >
              载入当前服务器版本并丢弃草稿
            </button>
            <button
              type="button"
              onClick={() => save(conflict.fileRevision)}
              disabled={busy}
              className="rounded-lg bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white shadow-2xs hover:bg-amber-700 active:scale-95 disabled:opacity-40"
            >
              强制以草稿内容覆盖当前版本
            </button>
          </div>
        </div>
      )}

      <StatusLine status={status} />
    </section>
  )
}
