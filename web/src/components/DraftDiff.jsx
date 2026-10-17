import { diffLines } from '../edits.js'

const ROW_STYLES = {
  context: 'text-slate-600 dark:text-slate-400',
  removed: 'bg-rose-50/80 text-rose-900 border-l-2 border-rose-500 dark:bg-rose-950/40 dark:text-rose-200',
  added: 'bg-emerald-50/80 text-emerald-900 border-l-2 border-emerald-500 dark:bg-emerald-950/40 dark:text-emerald-200',
}

const ROW_PREFIX = { context: ' ', removed: '-', added: '+' }

// DraftDiff renders the line diff between the server's current content and an
// unsaved draft. It is only shown after a 412 REVISION_MISMATCH, so the
// operator can decide whether to reload or to reapply the draft; memory text is
// rendered as untrusted text. See docs/04-ui-and-cli-specification.md section 4.
export function DraftDiff({ currentText, draftText }) {
  const rows = diffLines(currentText, draftText)
  if (rows.length === 0) {
    return <p className="text-xs text-slate-500 dark:text-slate-400">当前内容与草稿完全一致。</p>
  }
  return (
    <div className="overflow-x-auto rounded-lg border border-slate-200/80 bg-slate-50/50 shadow-2xs dark:border-slate-800 dark:bg-slate-950">
      <table className="w-full border-collapse text-left font-mono text-[11px] leading-relaxed">
        <tbody>
          {rows.map((row, index) => (
            <tr key={index} className={`transition-colors ${ROW_STYLES[row.type] || ROW_STYLES.context}`}>
              <td className="w-7 select-none border-r border-slate-200/80 px-2 py-0.5 text-center text-slate-400 dark:border-slate-800 dark:text-slate-600">
                {ROW_PREFIX[row.type]}
              </td>
              <td className="whitespace-pre-wrap break-all px-3 py-0.5">{row.text || ' '}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
