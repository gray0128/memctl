import { useState } from 'preact/hooks'
import { isRevisionConflict } from '../edits.js'
import { DraftDiff } from './DraftDiff.jsx'

function StatusLine({ status }) {
  if (!status) return null
  const tones = {
    info: 'text-slate-500 dark:text-slate-400',
    ok: 'text-emerald-700 dark:text-emerald-300',
    warn: 'text-amber-700 dark:text-amber-300',
    error: 'text-rose-700 dark:text-rose-300',
  }
  return <p className={`text-sm ${tones[status.tone] || tones.info}`}>{status.message}</p>
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
      setStatus({ tone: 'error', message: '请先填写文件路径。' })
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
      setStatus({ tone: 'info', message: `已载入 ${value}` })
    } catch (error) {
      if (error.status === 404 && error.code === 'NOT_FOUND') {
        // A path that does not exist yet is a new file. Saving uses
        // If-None-Match: * so a concurrent creation is still a version conflict.
        setPath(value)
        setLoaded({ path: value, fileRevision: 'absent', rawContent: '' })
        setDraft('')
        setStatus({ tone: 'info', message: `${value} 尚不存在，保存将新建该文件。` })
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
        message: `已提交 ${result.commit ? result.commit.slice(0, 12) : result.transactionId}`,
      })
    } catch (error) {
      if (isRevisionConflict(error)) {
        try {
          const current = await api.file(loaded.path)
          setConflict({ rawContent: current.rawContent, fileRevision: current.file.fileRevision })
          setStatus({ tone: 'warn', message: '文件已被其他写入改变，草稿已保留。' })
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
    setStatus({ tone: 'info', message: '已载入服务器当前内容，草稿已丢弃。' })
  }

  return (
    <section className="space-y-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-lg font-semibold">原文编辑</h2>
        <span className="text-xs text-slate-500 dark:text-slate-400">
          保存使用读取时的文件修订；版本冲突时保留草稿
        </span>
      </div>

      <div className="flex flex-wrap items-end gap-2">
        <label className="flex flex-1 flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
          文件路径
          <input
            type="text"
            list="memctl-file-list"
            value={path}
            placeholder="例如 notes/alpha.md"
            onInput={(event) => setPath(event.currentTarget.value)}
            className="rounded border border-slate-300 bg-white px-2 py-1 font-mono text-sm text-slate-900 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
          />
        </label>
        <datalist id="memctl-file-list">
          {(files || []).map((file) => (
            <option key={file.relPath} value={file.relPath} />
          ))}
        </datalist>
        <button
          type="button"
          onClick={() => load()}
          disabled={busy}
          className="rounded border border-slate-300 px-3 py-1.5 text-sm disabled:opacity-40 dark:border-slate-700"
        >
          载入
        </button>
      </div>

      {loaded && (
        <div className="space-y-2">
          <textarea
            value={draft}
            rows={16}
            onInput={(event) => setDraft(event.currentTarget.value)}
            spellCheck={false}
            className="w-full rounded border border-slate-300 bg-white p-2 font-mono text-xs text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
          />
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={() => save()}
              disabled={busy || !dirty || api.readOnly}
              className="rounded bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
            >
              保存
            </button>
            <button
              type="button"
              onClick={() => {
                setDraft(loaded.rawContent)
                setConflict(null)
                setStatus(null)
              }}
              disabled={busy || !dirty}
              className="rounded border border-slate-300 px-3 py-1.5 text-sm disabled:opacity-40 dark:border-slate-700"
            >
              放弃更改
            </button>
            {api.readOnly && (
              <span className="text-xs text-slate-500 dark:text-slate-400">只读模式：保存不可用</span>
            )}
            {dirty && !api.readOnly && (
              <span className="text-xs text-amber-700 dark:text-amber-300">有未保存的草稿</span>
            )}
          </div>
        </div>
      )}

      {conflict && (
        <div className="space-y-2 rounded border border-amber-300 bg-amber-50 p-3 dark:border-amber-900 dark:bg-amber-900/20">
          <p className="text-sm text-amber-900 dark:text-amber-100">
            当前内容（服务器）与草稿差异：
          </p>
          <DraftDiff currentText={conflict.rawContent} draftText={draft} />
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={useCurrent}
              className="rounded border border-slate-300 px-3 py-1.5 text-sm dark:border-slate-700"
            >
              载入当前并丢弃草稿
            </button>
            <button
              type="button"
              onClick={() => save(conflict.fileRevision)}
              disabled={busy}
              className="rounded bg-amber-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
            >
              用草稿覆盖当前
            </button>
          </div>
        </div>
      )}

      <StatusLine status={status} />
    </section>
  )
}
