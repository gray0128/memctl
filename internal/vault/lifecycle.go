package vault

import (
	"time"
)

// Time-limited entries
//
// docs/10-memory-lifecycle.md defines three content classes. A time-limited
// entry carries the optional `until: YYYY-MM-DD` metadata value; once that date
// has passed the entry is hidden from every read surface but stays in the file
// and in Git history. Nothing here deletes anything: the hide is a read-time
// filter, and deletion is a separate explicit action.

// DateLayout is the documented YYYY-MM-DD layout shared by `added` and `until`.
const DateLayout = "2006-01-02"

// EntryExpired reports whether an entry with the given `until` value is expired
// at now. An empty or invalid value never expires, so a missing `until` keeps
// the documented "no expiry" default. The whole day of `until` is still visible:
// the entry expires at the start of the next local day.
func EntryExpired(until string, now time.Time) bool {
	if until == "" {
		return false
	}
	deadline, err := time.ParseInLocation(DateLayout, until, now.Location())
	if err != nil {
		return false
	}
	return !now.Before(deadline.AddDate(0, 0, 1))
}

// HideExpiredLine replaces the fact text of one expired bullet with
// ExpiredFactPlaceholder while keeping the trailing metadata region byte for
// byte, including `until`. The result is only rendered in a read result; it is
// never written back, so hiding does not modify the repository.
func HideExpiredLine(rawLine string) string {
	body, ok := bulletBody(rawLine)
	if !ok {
		return rawLine
	}
	_, suffix := SplitFactAndMetadata(body)
	return "- " + ExpiredFactPlaceholder + suffix
}

// ExpiredFactPlaceholder marks an expired time-limited entry in a full-file
// read. It is deliberately not a fact: a model must not treat a hidden entry as
// current content.
const ExpiredFactPlaceholder = "[已过期：条目已到期并从读取中隐藏；完整内容见 Git 历史]"
