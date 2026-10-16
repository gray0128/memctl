import { diffLines } from '../edits.js'

const ROW_STYLES = {
  context: 'text-slate-600 dark:text-slate-300',
  removed: 'bg-rose-50 text-rose-900 dark:bg-rose-900/30 dark:text-rose-100',
  added: 'bg-emerald-50 text-emerald-900 dark:bg-emerald-900/30 dark:text-emerald-100',
}

const ROW_PREFIX = { context: ' ', removed: '-', added: '+' }

// DraftDiff renders the line diff between the server's current content and an
// unsaved draft. It is only shown after a 412 REVISION_MISMATCH, so the
// operator can decide whether to reload or to reapply the draft; memory text is
// rendered as untrusted text. See docs/04-ui-and-cli-specification.md section 4.
export function DraftDiff({ currentText, draftText }) {
  const rows = diffLines(currentText, draftText)
  if (rows.length === 0) {
    return <p className="text-xs text-slate-500 dark:text-slate-400">当前内容与草稿一致。</p>
  }
  return (
    <div className="overflow-x-auto rounded border border-slate-200 dark:border-slate-800">
      <table className="w-full border-collapse text-left font-mono text-xs">
        <tbody>
          {rows.map((row, index) => (
            <tr key={index} className={ROW_STYLES[row.type]}>
              <td className="w-6 select-none border-r border-slate-200 px-2 align-top dark:border-slate-800">
                {ROW_PREFIX[row.type]}
              </td>
              <td className="whitespace-pre-wrap break-all px-2 align-top">{row.text || ' '}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
