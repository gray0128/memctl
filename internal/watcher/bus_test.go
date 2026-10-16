package watcher

import (
	"testing"
	"time"
)

// TestBusBoundedQueueSignalsResync verifies that a subscriber with a full
// buffer is told to resynchronize instead of making the bus grow without bound.
func TestBusBoundedQueueSignalsResync(t *testing.T) {
	bus := NewBus(1)
	sub := bus.Subscribe()
	defer sub.Close()

	bus.Publish(Invalidation{Reason: ReasonVaultUpdated, ChangedFiles: []string{"a.md"}})
	bus.Publish(Invalidation{Reason: ReasonVaultUpdated, ChangedFiles: []string{"b.md"}})
	bus.Publish(Invalidation{Reason: ReasonVaultUpdated, ChangedFiles: []string{"c.md"}})

	select {
	case hint := <-sub.Hints():
		if len(hint.ChangedFiles) != 1 || hint.ChangedFiles[0] != "a.md" {
			t.Fatalf("unexpected first hint: %+v", hint)
		}
	default:
		t.Fatal("the first published hint should be buffered")
	}

	select {
	case <-sub.Resync():
	default:
		t.Fatal("dropping a hint for a full subscriber should signal resync")
	}

	select {
	case extra := <-sub.Hints():
		t.Fatalf("hints beyond the bound should be dropped, got %+v", extra)
	default:
	}
}

// TestBusFanOutAndClose verifies every subscriber receives a hint until it is
// closed.
func TestBusFanOutAndClose(t *testing.T) {
	bus := NewBus(2)
	first := bus.Subscribe()
	second := bus.Subscribe()

	bus.Publish(Invalidation{Reason: ReasonVaultUpdated, ChangedFiles: []string{"a.md"}})
	for name, sub := range map[string]*Subscription{"first": first, "second": second} {
		select {
		case hint := <-sub.Hints():
			if hint.Reason != ReasonVaultUpdated {
				t.Fatalf("%s received %q", name, hint.Reason)
			}
		default:
			t.Fatalf("%s did not receive the hint", name)
		}
	}

	first.Close()
	bus.Publish(Invalidation{Reason: ReasonResyncRequired})
	select {
	case hint := <-second.Hints():
		if hint.Reason != ReasonResyncRequired {
			t.Fatalf("second received %q", hint.Reason)
		}
	default:
		t.Fatal("the open subscriber should still receive hints")
	}
	select {
	case hint := <-first.Hints():
		t.Fatalf("a closed subscription should not receive hints, got %+v", hint)
	default:
	}
}

// TestPublishFillsEmptyReason verifies the default event name.
func TestPublishFillsEmptyReason(t *testing.T) {
	bus := NewBus(1)
	sub := bus.Subscribe()
	defer sub.Close()
	bus.Publish(Invalidation{ChangedFiles: []string{"a.md"}})
	select {
	case hint := <-sub.Hints():
		if hint.Reason != ReasonVaultUpdated {
			t.Fatalf("empty reason should default to %q, got %q", ReasonVaultUpdated, hint.Reason)
		}
	case <-time.After(time.Second):
		t.Fatal("hint was not delivered")
	}
}
