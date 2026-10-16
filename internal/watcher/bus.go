package watcher

import "sync"

// DefaultQueue is the per-subscriber bound of a Bus. A slow subscriber never
// makes the server buffer without bound; it receives one resync.required
// notification instead. See docs/02-domain-models-and-contracts.md section 5.
const DefaultQueue = 64

// Invalidation is one index-invalidation hint. It is not a fact source: a
// client that receives it re-reads the snapshot instead of trusting the
// payload. Revision is only set when the producer already knows it, such as a
// committed write transaction.
type Invalidation struct {
	Reason       string
	ChangedFiles []string
	Revision     string
}

// Sink receives invalidation hints. Publish must never block.
type Sink interface {
	Publish(Invalidation)
}

// Bus is a bounded fan-out of invalidation hints to SSE subscribers. Each
// subscriber owns a bounded buffer. When a subscriber cannot keep up, the bus
// drops the queued hints for that subscriber and signals Resync instead, so
// the server never grows an unbounded queue for a slow client.
type Bus struct {
	mu     sync.Mutex
	buffer int
	subs   map[*Subscription]struct{}
}

// NewBus builds a Bus whose per-subscriber buffer holds buffer hints. A
// non-positive value uses DefaultQueue.
func NewBus(buffer int) *Bus {
	if buffer <= 0 {
		buffer = DefaultQueue
	}
	return &Bus{buffer: buffer, subs: map[*Subscription]struct{}{}}
}

// Publish delivers one hint to every subscriber without blocking.
func (b *Bus) Publish(inv Invalidation) {
	if b == nil {
		return
	}
	if inv.Reason == "" {
		inv.Reason = ReasonVaultUpdated
	}
	b.mu.Lock()
	subs := make([]*Subscription, 0, len(b.subs))
	for sub := range b.subs {
		subs = append(subs, sub)
	}
	b.mu.Unlock()
	for _, sub := range subs {
		sub.deliver(inv)
	}
}

// Subscribe registers one subscriber. The caller must Close it.
func (b *Bus) Subscribe() *Subscription {
	sub := &Subscription{
		bus:    b,
		hints:  make(chan Invalidation, b.buffer),
		resync: make(chan struct{}, 1),
	}
	b.mu.Lock()
	b.subs[sub] = struct{}{}
	b.mu.Unlock()
	return sub
}

// Subscription is one consumer's bounded view of the bus.
type Subscription struct {
	bus    *Bus
	hints  chan Invalidation
	resync chan struct{}
	once   sync.Once
}

// Hints returns the bounded hint channel. It is never closed; the consumer
// stops reading after Close.
func (s *Subscription) Hints() <-chan Invalidation { return s.hints }

// Resync is signaled when hints were dropped for this subscriber. Receiving
// from it means the client must re-read the snapshot instead of assuming it
// saw every change.
func (s *Subscription) Resync() <-chan struct{} { return s.resync }

// Close removes the subscription from its bus.
func (s *Subscription) Close() {
	s.once.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.subs, s)
		s.bus.mu.Unlock()
	})
}

func (s *Subscription) deliver(inv Invalidation) {
	select {
	case s.hints <- inv:
	default:
		// The subscriber is behind. Coalesce any number of drops into one
		// pending resync signal; the queued hints remain hints only.
		select {
		case s.resync <- struct{}{}:
		default:
		}
	}
}
