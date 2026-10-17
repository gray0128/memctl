// formatTimestamp renders the API's ISO-8601 sync timestamp in the browser's
// local time. The contract keeps timestamps in UTC; the status bar is a local
// console, so a raw `...Z` string both confuses the timezone and overflows the
// narrow layout. See docs/04-ui-and-cli-specification.md section 4.

function pad(value) {
  return String(value).padStart(2, '0')
}

export function formatTimestamp(value) {
  if (!value) return '未知'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return String(value)
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}
