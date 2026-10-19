package vault

import "time"

// HiddenPeriodDays is the documented hidden period from
// docs/10-memory-lifecycle.md section 3.3: an expired entry stays in the file
// for this many days before an explicit delete is allowed. Hiding is immediate;
// deletion is never automatic.
const HiddenPeriodDays = 30

// DeletableAt returns the earliest local date on which an expired entry may be
// deleted, and false when the entry has no valid `until` or has not expired
// yet. The period starts the day after `until`, so an entry that expires on the
// 1st becomes deletable on the 31st.
func DeletableAt(until string, now time.Time) (time.Time, bool) {
	if until == "" || !EntryExpired(until, now) {
		return time.Time{}, false
	}
	deadline, err := time.ParseInLocation(DateLayout, until, now.Location())
	if err != nil {
		return time.Time{}, false
	}
	return deadline.AddDate(0, 0, 1+HiddenPeriodDays), true
}

// DeletionDue reports whether an expired entry has completed its hidden period
// and may now be deleted by an explicit action.
func DeletionDue(until string, now time.Time) bool {
	deletableAt, ok := DeletableAt(until, now)
	if !ok {
		return false
	}
	return !now.Before(deletableAt)
}

// DeletionBlockedReason explains why an expired entry cannot be deleted yet, or
// returns an empty string when it can. The message is user-facing and names the
// date so an operator can retry deliberately instead of guessing.
func DeletionBlockedReason(until string, now time.Time) string {
	if until == "" {
		return "the entry has no until metadata and never expires"
	}
	if !EntryExpired(until, now) {
		return "the entry has not expired yet"
	}
	deletableAt, ok := DeletableAt(until, now)
	if !ok {
		return "the entry's until value is not a valid date"
	}
	if now.Before(deletableAt) {
		return "the entry is in its hidden period until " + deletableAt.Format(DateLayout)
	}
	return ""
}
