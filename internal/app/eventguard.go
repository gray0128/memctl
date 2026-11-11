package app

import (
	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// The `kind: event` guard of docs/18-agent-memory-review-and-optimization.md
// section 4.4.2 on every write path that rewrites whole files: Apply (write,
// memory_write, PUT /api/v1/files) and ApplyDream. An event entry may move to
// another managed file of the same transaction unchanged; deleting it or
// rewriting its fact text or `added` date needs the explicit force flag, which
// only Apply and DeleteEntry accept.

// requireEventsKept refuses planned changes that drop a `kind: event` entry.
func requireEventsKept(plans []plannedChange) error {
	before := map[string][]byte{}
	after := map[string][]byte{}
	for _, plan := range plans {
		if !expiryApplies(plan.path) {
			continue
		}
		if plan.existedBefore {
			before[plan.path] = plan.before
		}
		if plan.action == ActionPut {
			after[plan.path] = []byte(plan.content)
		}
	}
	return eventProtectedError(vault.EventRemovals(before, after), "pass --force-delete-event (forceDeleteEvent) to delete it deliberately")
}

// requireDreamEventsKept refuses a Dreaming proposal that drops a `kind:
// event` entry. A proposal has no force flag: an event is only removed by a
// deliberate write.
func requireDreamEventsKept(root *vault.Root, plan dream.ApplyPlan) error {
	before := map[string][]byte{}
	after := map[string][]byte{}
	for _, change := range plan.Changes {
		if !expiryApplies(change.Path) {
			continue
		}
		if previous, err := root.ReadFile(change.Path); err == nil {
			before[change.Path] = []byte(previous.RawContent)
		}
		if change.Action != dream.ChangeDelete {
			after[change.Path] = []byte(change.Content)
		}
	}
	return eventProtectedError(vault.EventRemovals(before, after), "proposals never delete events; delete it with memctl write --force-delete-event")
}

// eventProtectedError builds the EVENT_PROTECTED error for the removed events,
// or returns nil when there are none.
func eventProtectedError(removed []string, hint string) error {
	if len(removed) == 0 {
		return nil
	}
	noun := "entries"
	if len(removed) == 1 {
		noun = "entry"
	}
	return memerr.New(memerr.EventProtected,
		"the change deletes or rewrites %d kind: event %s (%s); events are append-only: keep the fact and added date unchanged, or move the entry to another file in the same change, or %s",
		len(removed), noun, joinLimited(removed, 3), hint)
}
