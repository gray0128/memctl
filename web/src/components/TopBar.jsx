const CONNECTION_STYLES = {
  live: 'bg-emerald-100 text-emerald-900 dark:bg-emerald-900/50 dark:text-emerald-100',
  connecting: 'bg-amber-100 text-amber-900 dark:bg-amber-900/50 dark:text-amber-100',
  reconnecting: 'bg-amber-100 text-amber-900 dark:bg-amber-900/50 dark:text-amber-100',
  stopped: 'bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-200',
}

const CONNECTION_LABELS = {
  live: '已连接',
  connecting: '连接中',
  reconnecting: '重连中',
  stopped: '已停止',
}

function Badge({ className, children }) {
  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${className}`}>
      {children}
    </span>
  )
}

function Field({ label, value }) {
  return (
    <div className="flex flex-col">
      <span className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">{label}</span>
      <span className="truncate text-sm font-medium" title={value}>
        {value}
      </span>
    </div>
  )
}

// TopBar shows the shared status required by
// docs/04-ui-and-cli-specification.md section 4: repository path, local and
// remote state, the last successful sync time, the read-only flag and the SSE
// connection state. Unknown ahead/behind counts stay unknown instead of zero.
export function TopBar({ connection, snapshot, readOnly, error }) {
  const sync = snapshot ? snapshot.syncState : null
  const localState = snapshot ? sync.localState : error ? 'unavailable' : 'loading'
  const remoteState = snapshot ? sync.remoteState : error ? 'unavailable' : 'loading'
  const lastSync = snapshot && sync.lastSuccessAt ? sync.lastSuccessAt : '未知'

  return (
    <header className="border-b border-slate-200 bg-white px-6 py-4 dark:border-slate-800 dark:bg-slate-900">
      <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-x-6 gap-y-3">
        <div className="flex items-center gap-2">
          <span className="text-lg font-semibold">memctl</span>
          {readOnly && <Badge className="bg-indigo-100 text-indigo-900 dark:bg-indigo-900/50 dark:text-indigo-100">只读</Badge>}
          <Badge className={CONNECTION_STYLES[connection] || CONNECTION_STYLES.stopped}>
            {CONNECTION_LABELS[connection] || connection}
          </Badge>
        </div>
        <div className="grid flex-1 grid-cols-2 gap-4 sm:grid-cols-4">
          <Field label="仓库" value={snapshot ? snapshot.repoPath : '未知'} />
          <Field label="本地状态" value={localState} />
          <Field label="远端状态" value={remoteState} />
          <Field label="最后同步" value={lastSync} />
        </div>
      </div>
      {error && (
        <p className="mx-auto mt-3 max-w-5xl text-sm text-rose-700 dark:text-rose-300">
          无法读取快照：{error.message}
        </p>
      )}
    </header>
  )
}
