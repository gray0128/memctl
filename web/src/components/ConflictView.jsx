import { useEffect, useRef, useState } from 'preact/hooks'
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
  text: '文本内容冲突',
  delete_modify: '文件删除与修改冲突',
  binary: '二进制文件冲突',
  unsupported: '不支持的特殊冲突类型',
}

function shortHash(hash) {
  return hash ? hash.slice(0, 10) : '未知'
}

// SidePanel renders one of the three captured sides as untrusted text.
function SidePanel({ label, side, tone = 'default' }) {
  const text = sideText(side)
  const TONES = {
    base: 'border-slate-200/80 bg-slate-50/70 text-slate-600 dark:border-slate-800 dark:bg-slate-900/60 dark:text-slate-400',
    onto: 'border-sky-200/70 bg-sky-50/50 text-sky-800 dark:border-sky-900/50 dark:bg-sky-950/30 dark:text-sky-300',
    replayed: 'border-indigo-200/70 bg-indigo-50/50 text-indigo-800 dark:border-indigo-900/50 dark:bg-indigo-950/30 dark:text-indigo-300',
  }

  return (
    <div className="flex flex-col min-w-0">
      <div className="flex items-center justify-between pb-1">
        <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
          {label}
        </span>
      </div>
      {text === null ? (
        <div className="flex h-32 items-center justify-center rounded-lg border border-dashed border-slate-200 bg-slate-50/40 p-3 text-xs text-slate-400 dark:border-slate-800 dark:bg-slate-900/30">
          {side && side.binary ? '二进制文件，不显示文本' : '此侧不存在（已删除）'}
        </div>
      ) : (
        <pre className={`h-36 overflow-auto rounded-lg border p-2.5 font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-all shadow-inner ${TONES[tone] || TONES.base}`}>
          {text === '' ? '（空文件内容）' : text}
        </pre>
      )}
    </div>
  )
}

// ConflictFileCard shows one conflicted path with its three-way context and the merge decision.
function ConflictFileCard({ file, choice, disabled, onPut, onDelete, onEdit }) {
  const deleting = choice && choice.action === 'delete'

  return (
    <li className="space-y-4 rounded-xl border border-slate-200/80 bg-white p-4 shadow-2xs dark:border-slate-800/80 dark:bg-slate-900/80">
      <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between border-b border-slate-100 pb-3 dark:border-slate-800/60">
        <div className="flex items-center gap-2">
          <span className="font-mono text-sm font-bold text-slate-900 dark:text-slate-100 break-all">
            {file.path}
          </span>
        </div>
        <span className="inline-flex items-center rounded-md bg-amber-50 px-2 py-0.5 text-xs font-semibold text-amber-700 dark:bg-amber-950/40 dark:text-amber-300">
          {KIND_LABELS[file.kind] || file.kind}
        </span>
      </div>

      {/* 3-Way Context Viewports */}
      <div className="grid gap-3 sm:grid-cols-3">
        <SidePanel label="共同祖先 (Base)" side={file.base} tone="base" />
        <SidePanel label="远端基线 (Onto)" side={file.onto} tone="onto" />
        <SidePanel label="本地重放 (Replayed)" side={file.replayed} tone="replayed" />
      </div>

      {/* Decision Quick Actions */}
      <div className="flex flex-wrap items-center gap-2 pt-1">
        <span className="text-xs font-medium text-slate-400 mr-1">快捷决策:</span>
        <button
          type="button"
          disabled={disabled}
          onClick={() => onPut(file.path, file.replayed)}
          className="rounded-lg border border-indigo-200/80 bg-indigo-50/60 px-2.5 py-1 text-xs font-semibold text-indigo-700 shadow-2xs hover:bg-indigo-100 active:scale-95 disabled:opacity-40 dark:border-indigo-900/60 dark:bg-indigo-950/40 dark:text-indigo-300"
        >
          采用本地重放
        </button>
        <button
          type="button"
          disabled={disabled || sideText(file.onto) === null}
          onClick={() => onPut(file.path, file.onto)}
          className="rounded-lg border border-sky-200/80 bg-sky-50/60 px-2.5 py-1 text-xs font-semibold text-sky-700 shadow-2xs hover:bg-sky-100 active:scale-95 disabled:opacity-40 dark:border-sky-900/60 dark:bg-sky-950/40 dark:text-sky-300"
        >
          采用远端基线
        </button>
        <button
          type="button"
          disabled={disabled}
          onClick={() => onPut(file.path, file.base)}
          className="rounded-lg border border-slate-200/80 bg-slate-50/80 px-2.5 py-1 text-xs font-medium text-slate-700 shadow-2xs hover:bg-slate-100 active:scale-95 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-800/60 dark:text-slate-300"
        >
          采用共同祖先
        </button>
        <button
          type="button"
          disabled={disabled}
          onClick={() => onDelete(file.path)}
          className={`rounded-lg border px-2.5 py-1 text-xs font-semibold shadow-2xs transition-all active:scale-95 disabled:opacity-40 ${
            deleting
              ? 'border-rose-600 bg-rose-600 text-white'
              : 'border-rose-200/80 bg-rose-50/60 text-rose-700 hover:bg-rose-100 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-300'
          }`}
        >
          {deleting ? '已标记为删除' : '删除该文件'}
        </button>
      </div>

      {/* Merge Resolution Editor */}
      {deleting ? (
        <div className="rounded-lg bg-rose-50/60 px-3 py-2 text-xs font-medium text-rose-700 dark:bg-rose-950/30 dark:text-rose-300">
          该文件在解决后将被原子删除。
        </div>
      ) : (
        <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-600 dark:text-slate-300">
          <div className="flex items-center justify-between">
            <span>最终合并内容预览（可直接编辑微调）</span>
            <span className="font-mono text-[11px] font-normal text-slate-400">
              {choice && choice.rawContent ? `${choice.rawContent.length} 字符` : '空'}
            </span>
          </div>
          <textarea
            rows={5}
            value={choice ? choice.rawContent : ''}
            disabled={disabled}
            onInput={(event) => onEdit(file.path, event.currentTarget.value)}
            className="w-full rounded-xl border border-slate-200/80 bg-white p-3 font-mono text-xs text-slate-900 shadow-inner outline-none transition-all focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
          />
        </label>
      )}
    </li>
  )
}

export function ConflictView({ api, refreshKey, onChanged }) {
  const [conflict, setConflict] = useState(undefined)
  const [choices, setChoices] = useState({})
  const [error, setError] = useState(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState(null)
  const [actionError, setActionError] = useState(null)
  const [confirmAbort, setConfirmAbort] = useState(false)
  const conflictIdRef = useRef(null)

  useEffect(() => {
    let cancelled = false
    setError(null)
    api
      .conflict()
      .then((result) => {
        if (cancelled) return
        const sameStage = Boolean(result) && conflictIdRef.current === result.conflictId
        conflictIdRef.current = result ? result.conflictId : null
        setConflict(result)
        if (!sameStage) setChoices(result ? initialChoices(result) : {})
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
        conflictIdRef.current = result.conflict.conflictId
        setConflict(result.conflict)
        setChoices(initialChoices(result.conflict))
        setNotice({ tone: 'warn', text: '本阶段已完成，重放进入下一提交后再次冲突，请继续处理新的三方快照。' })
      } else {
        conflictIdRef.current = null
        setConflict(null)
        setChoices({})
        setNotice({
          tone: result.remoteState === 'synced' ? 'ok' : 'info',
          text: `冲突已完全解决，远端同步状态: ${result.remoteState}。`,
        })
      }
      onChanged?.()
    } catch (requestError) {
      if (requestError.code === 'CONFLICT_STALE') {
        conflictIdRef.current = null
        setConflict(null)
        setChoices({})
        setNotice({ tone: 'warn', text: '该冲突阶段已变化，已丢弃旧快照；请重新同步后读取新的冲突。' })
        onChanged?.()
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
    return (
      <div className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-xs font-semibold text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300">
        无法读取冲突状态：{error.message}
      </div>
    )
  }
  if (conflict === undefined) {
    return <p className="text-xs text-slate-500 dark:text-slate-400">正在获取冲突快照…</p>
  }

  const noticeBlock = notice && (
    <div
      className={`rounded-xl border px-4 py-2.5 text-xs font-medium shadow-2xs ${
        notice.tone === 'ok'
          ? 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900/60 dark:bg-emerald-950/40 dark:text-emerald-300'
          : notice.tone === 'warn'
            ? 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-300'
            : 'border-slate-200 bg-slate-50 text-slate-700 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-300'
      }`}
    >
      {notice.text}
    </div>
  )

  if (conflict === null) {
    return (
      <section className="space-y-6">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-slate-900 dark:text-white">冲突解决工作流</h1>
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            Git 多阶段重放冲突合并、三方基线对齐与安全中止
          </p>
        </div>

        <div className="rounded-2xl border border-slate-200/80 bg-white p-12 text-center shadow-2xs dark:border-slate-800/80 dark:bg-slate-900">
          <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-xl bg-emerald-50 text-emerald-600 dark:bg-emerald-950/50 dark:text-emerald-400">
            <svg className="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          <h3 className="mt-4 text-sm font-semibold text-slate-900 dark:text-slate-100">
            当前无待解决冲突
          </h3>
          <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
            工作树与上游远端分支历史干净同步，无挂起的合并阶段
          </p>
        </div>

        {noticeBlock}
      </section>
    )
  }

  const unsupported = unsupportedConflictFiles(conflict)
  const supported = supportedConflictFiles(conflict)
  const ready = canResolve(conflict, choices)

  return (
    <section className="space-y-6">
      {/* Conflict Header */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b border-slate-200/80 pb-4 dark:border-slate-800/80">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-xl font-bold tracking-tight text-slate-900 dark:text-white">冲突解决中</h1>
            <span className="rounded-full bg-amber-50 px-2.5 py-0.5 text-xs font-semibold text-amber-700 dark:bg-amber-950/40 dark:text-amber-300">
              阶段 {conflict.phase}
            </span>
          </div>
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            {conflictPhaseLabel(conflict.phase)}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span className="rounded-lg border border-slate-200/80 bg-white px-2.5 py-1 text-slate-600 shadow-2xs dark:border-slate-800 dark:bg-slate-900 dark:text-slate-300">
            触发提交: <code className="font-mono font-semibold">{shortHash(conflict.replayCommit)}</code>
          </span>
          <span className="rounded-lg border border-slate-200/80 bg-white px-2.5 py-1 text-slate-600 shadow-2xs dark:border-slate-800 dark:bg-slate-900 dark:text-slate-300">
            上游 HEAD: <code className="font-mono font-semibold">{shortHash(conflict.upstreamHead)}</code>
          </span>
        </div>
      </div>

      {unsupported.length > 0 && (
        <div className="rounded-xl border border-amber-300/80 bg-amber-50/90 p-4 shadow-xs dark:border-amber-800/80 dark:bg-amber-950/40">
          <h4 className="text-xs font-bold text-amber-900 dark:text-amber-200">
            检测到不支持由 Web UI 合并的冲突类型，请在本地 Git 环境中人工处理：
          </h4>
          <ul className="mt-2 list-inside list-disc text-xs font-mono text-amber-800 dark:text-amber-300">
            {unsupported.map((file) => (
              <li key={file.path} className="break-all">
                {file.path}（{KIND_LABELS[file.kind] || file.kind}）
              </li>
            ))}
          </ul>
        </div>
      )}

      {supported.length === 0 ? (
        <p className="text-xs text-slate-500 dark:text-slate-400">本阶段没有可在 UI 解决的文本冲突文件。</p>
      ) : (
        <ul className="space-y-4">
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

      {noticeBlock}
      {actionError && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-xs font-semibold text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300">
          冲突操作失败：{actionError.message}
          {actionError.code ? `（${actionError.code}）` : ''}
        </div>
      )}

      {/* Primary Action Footer */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-slate-200/80 pt-4 dark:border-slate-800/80">
        <div className="flex items-center gap-2">
          <button
            type="button"
            disabled={!ready || busy || api.readOnly}
            onClick={resolve}
            className="rounded-lg bg-indigo-600 px-4 py-2 text-xs font-semibold text-white shadow-2xs hover:bg-indigo-700 active:scale-95 disabled:cursor-not-allowed disabled:opacity-40"
          >
            {busy ? '正在提交解决…' : '提交决策并继续解决'}
          </button>
        </div>

        <div>
          {confirmAbort ? (
            <div className="flex items-center gap-2">
              <button
                type="button"
                disabled={busy || api.readOnly}
                onClick={abort}
                className="rounded-lg bg-rose-600 px-3.5 py-1.5 text-xs font-semibold text-white shadow-2xs hover:bg-rose-700 active:scale-95 disabled:opacity-40"
              >
                确认中止并恢复原状态
              </button>
              <button
                type="button"
                onClick={() => setConfirmAbort(false)}
                className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
              >
                取消
              </button>
            </div>
          ) : (
            <button
              type="button"
              disabled={busy || api.readOnly}
              onClick={() => setConfirmAbort(true)}
              className="rounded-lg border border-rose-200/80 px-3 py-1.5 text-xs font-semibold text-rose-600 hover:bg-rose-50 active:scale-95 disabled:opacity-40 dark:border-rose-900/60 dark:text-rose-400 dark:hover:bg-rose-950/40"
            >
              中止当前解决过程
            </button>
          )}
        </div>
      </div>
    </section>
  )
}
