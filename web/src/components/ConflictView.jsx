import { useEffect, useState } from 'preact/hooks'
import {
  buildResolvedFiles,
  canResolve,
  conflictPhaseLabel,
  initialChoices,
  sideText,
  supportedConflictFiles,
  unsupportedConflictFiles,
} from '../conflicts.js'

const KIND_LABELS = {
  text: '文本冲突',
  delete_modify: '删除与修改冲突',
  binary: '二进制冲突',
  unsupported: '不支持的冲突类型',
}

function shortHash(hash) {
  return hash ? hash.slice(0, 12) : '未知'
}

// SidePanel renders one of the three captured sides as untrusted text. An
// absent side is a deletion, not an empty file, per
// docs/02-domain-models-and-contracts.md section 2.2.
function SidePanel({ label, side }) {
  const text = sideText(side)
  return (
    <div className="min-w-0">
      <div className="text-xs font-medium text-slate-500 dark:text-slate-400">{label}</div>
      {text === null ? (
        <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
          {side && side.binary ? '二进制内容，不显示文本' : '不存在（删除）'}
        </p>
      ) : (
        <pre className="mt-1 max-h-40 overflow-auto rounded border border-slate-200 bg-slate-50 p-2 text-xs whitespace-pre-wrap break-all dark:border-slate-800 dark:bg-slate-950">
          {text === '' ? '（空文件）' : text}
        </pre>
      )}
    </div>
  )
}

// ConflictFileCard shows one conflicted path with its three-way context and the
// merge decision. The operator can keep one side, edit the text or delete the
// path; the result is sent as one resolvedFiles entry.
function ConflictFileCard({ file, choice, disabled, onPut, onDelete, onEdit }) {
  const deleting = choice && choice.action === 'delete'
  return (
    <li className="space-y-3 rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="font-mono text-sm break-all">{file.path}</span>
        <span className="text-xs text-slate-500 dark:text-slate-400">
          {KIND_LABELS[file.kind] || file.kind}
        </span>
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <SidePanel label="共同祖先（base）" side={file.base} />
        <SidePanel label="远端基线（onto）" side={file.onto} />
        <SidePanel label="本地重放（replayed）" side={file.replayed} />
      </div>

      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          disabled={disabled}
          onClick={() => onPut(file.path, file.replayed)}
          className="rounded border border-slate-300 px-2 py-1 text-xs disabled:opacity-40 dark:border-slate-700"
        >
          采用本地重放
        </button>
        <button
          type="button"
          disabled={disabled || sideText(file.onto) === null}
          onClick={() => onPut(file.path, file.onto)}
          className="rounded border border-slate-300 px-2 py-1 text-xs disabled:opacity-40 dark:border-slate-700"
        >
          采用远端基线
        </button>
        <button
          type="button"
          disabled={disabled}
          onClick={() => onPut(file.path, file.base)}
          className="rounded border border-slate-300 px-2 py-1 text-xs disabled:opacity-40 dark:border-slate-700"
        >
          采用共同祖先
        </button>
        <button
          type="button"
          disabled={disabled}
          onClick={() => onDelete(file.path)}
          className={`rounded border px-2 py-1 text-xs disabled:opacity-40 ${
            deleting
              ? 'border-rose-600 bg-rose-600 font-medium text-white'
              : 'border-rose-300 text-rose-700 dark:border-rose-900 dark:text-rose-300'
          }`}
        >
          删除该文件
        </button>
      </div>

      {deleting ? (
        <p className="text-xs text-rose-700 dark:text-rose-300">本阶段将删除 {file.path}。</p>
      ) : (
        <label className="flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
          合并结果（可直接编辑）
          <textarea
            rows={4}
            value={choice ? choice.rawContent : ''}
            disabled={disabled}
            onInput={(event) => onEdit(file.path, event.currentTarget.value)}
            className="rounded border border-slate-300 bg-white p-2 font-mono text-xs text-slate-900 disabled:opacity-60 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
          />
        </label>
      )}
    </li>
  )
}

// ConflictView is the conflict-resolution view. It loads the active snapshot
// from GET /api/v1/conflict, edits one stage's resolved files and submits them
// through POST /api/v1/conflict:resolve, or aborts the owned replay through
// POST /api/v1/conflict:abort. Both routes reuse the CLI resolve state machine,
// so a resolve may return the next stage instead of finishing. Unsupported
// conflict types are never merged here and point at manual resolution.
// See docs/04-ui-and-cli-specification.md section 4.
export function ConflictView({ api, refreshKey, onChanged }) {
  const [conflict, setConflict] = useState(undefined)
  const [choices, setChoices] = useState({})
  const [error, setError] = useState(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState(null)
  const [actionError, setActionError] = useState(null)
  const [confirmAbort, setConfirmAbort] = useState(false)

  useEffect(() => {
    let cancelled = false
    setError(null)
    api
      .conflict()
      .then((result) => {
        if (cancelled) return
        setConflict(result)
        setChoices(result ? initialChoices(result) : {})
      })
      .catch((requestError) => {
        if (!cancelled) setError(requestError)
      })
    return () => {
      cancelled = true
    }
  }, [api, refreshKey])

  function setChoice(path, next) {
    setChoices((current) => ({ ...current, [path]: next }))
  }

  function putSide(path, side) {
    const text = sideText(side)
    if (text === null) {
      setActionError(new Error('该侧不存在或不支持文本合并，请改用删除或人工处理。'))
      return
    }
    setChoice(path, { action: 'put', rawContent: text })
  }

  async function resolve() {
    if (!canResolve(conflict, choices)) return
    setBusy(true)
    setActionError(null)
    setNotice(null)
    try {
      const result = await api.resolveConflict({
        conflictId: conflict.conflictId,
        baseRevision: conflict.baseRevision,
        resolvedFiles: buildResolvedFiles(conflict, choices),
      })
      if (result.conflict) {
        setConflict(result.conflict)
        setChoices(initialChoices(result.conflict))
        setNotice({ tone: 'warn', text: '本阶段已完成，重放进入下一提交后再次冲突，请继续处理新的三方快照。' })
      } else {
        setConflict(null)
        setChoices({})
        setNotice({
          tone: result.remoteState === 'synced' ? 'ok' : 'info',
          text: `冲突已解决，远端状态 ${result.remoteState}。`,
        })
      }
      onChanged?.()
    } catch (requestError) {
      if (requestError.code === 'CONFLICT_STALE') {
        setConflict(null)
        setChoices({})
        setNotice({ tone: 'warn', text: '该冲突阶段已变化，已丢弃旧快照；请重新同步后读取新的冲突。' })
      } else {
        setActionError(requestError)
      }
    } finally {
      setBusy(false)
    }
  }

  async function abort() {
    setBusy(true)
    setActionError(null)
    setNotice(null)
    try {
      const result = await api.abortConflict({ conflictId: conflict.conflictId })
      setConflict(null)
      setChoices({})
      setConfirmAbort(false)
      setNotice({ tone: 'info', text: `已中止解决过程并恢复原始 HEAD，远端状态 ${result.remoteState}。` })
      onChanged?.()
    } catch (requestError) {
      setActionError(requestError)
    } finally {
      setBusy(false)
    }
  }

  if (error) {
    return <p className="text-sm text-rose-700 dark:text-rose-300">无法读取冲突状态：{error.message}</p>
  }
  if (conflict === undefined) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">正在读取冲突状态…</p>
  }
  if (conflict === null) {
    return (
      <section className="space-y-3">
        <h1 className="text-xl font-semibold">冲突解决</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400">当前没有待解决的冲突。</p>
      </section>
    )
  }

  const unsupported = unsupportedConflictFiles(conflict)
  const supported = supportedConflictFiles(conflict)
  const ready = canResolve(conflict, choices)

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-xl font-semibold">冲突解决</h1>
        <span className="text-xs text-slate-500 dark:text-slate-400">
          阶段 {conflict.phase} · 触发提交 {shortHash(conflict.replayCommit)} · 上游{' '}
          {shortHash(conflict.upstreamHead)}
        </span>
      </div>
      <p className="text-sm text-slate-600 dark:text-slate-300">{conflictPhaseLabel(conflict.phase)}</p>

      {unsupported.length > 0 && (
        <div className="rounded border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-900/20 dark:text-amber-100">
          <p>以下路径属于不支持的冲突类型，请在 memctl 外人工处理后再重新同步：</p>
          <ul className="mt-1 list-inside list-disc text-xs">
            {unsupported.map((file) => (
              <li key={file.path} className="font-mono break-all">
                {file.path}（{KIND_LABELS[file.kind] || file.kind}）
              </li>
            ))}
          </ul>
        </div>
      )}

      {supported.length === 0 ? (
        <p className="text-sm text-slate-500 dark:text-slate-400">本阶段没有可在 UI 解决的冲突文件。</p>
      ) : (
        <ul className="space-y-3">
          {supported.map((file) => (
            <ConflictFileCard
              key={file.path}
              file={file}
              choice={choices[file.path]}
              disabled={busy || api.readOnly}
              onPut={putSide}
              onDelete={(path) => setChoice(path, { action: 'delete', rawContent: '' })}
              onEdit={(path, value) => setChoice(path, { action: 'put', rawContent: value })}
            />
          ))}
        </ul>
      )}

      {notice && (
        <p
          className={`text-sm ${
            notice.tone === 'ok'
              ? 'text-emerald-700 dark:text-emerald-300'
              : notice.tone === 'warn'
                ? 'text-amber-700 dark:text-amber-300'
                : 'text-slate-500 dark:text-slate-400'
          }`}
        >
          {notice.text}
        </p>
      )}
      {actionError && (
        <p className="text-sm text-rose-700 dark:text-rose-300">
          冲突操作失败：{actionError.message}
          {actionError.code ? `（${actionError.code}）` : ''}
        </p>
      )}

      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          disabled={!ready || busy || api.readOnly}
          onClick={resolve}
          className="rounded bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
        >
          继续解决
        </button>
        {confirmAbort ? (
          <>
            <button
              type="button"
              disabled={busy || api.readOnly}
              onClick={abort}
              className="rounded bg-rose-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
            >
              确认中止
            </button>
            <button
              type="button"
              onClick={() => setConfirmAbort(false)}
              className="rounded border border-slate-300 px-3 py-1.5 text-sm dark:border-slate-700"
            >
              取消
            </button>
          </>
        ) : (
          <button
            type="button"
            disabled={busy || api.readOnly}
            onClick={() => setConfirmAbort(true)}
            className="rounded border border-rose-300 px-3 py-1.5 text-sm text-rose-700 disabled:opacity-40 dark:border-rose-900 dark:text-rose-300"
          >
            中止解决
          </button>
        )}
      </div>
    </section>
  )
}
