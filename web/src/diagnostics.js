// Diagnostics summary for the read-only doctor view.
//
// The DoctorReport already carries per-severity counts. This module groups the
// issues by severity so the view shows the documented error/warning/info
// levels. It never computes a health score, per
// docs/04-ui-and-cli-specification.md section 4.

export const SEVERITY_ORDER = ['error', 'warning', 'info']

export function summarizeDiagnostics(report) {
  const issues = (report && report.issues) || []
  const counts = { error: 0, warning: 0, info: 0 }
  for (const issue of issues) {
    if (counts[issue.severity] !== undefined) counts[issue.severity] += 1
  }
  const groups = SEVERITY_ORDER.map((severity) => ({
    severity,
    count: counts[severity],
    issues: issues.filter((issue) => issue.severity === severity),
  }))
  return { counts, total: issues.length, groups }
}
