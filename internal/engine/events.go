package engine

import (
	"sync"
)

const (
	TopicRunUpdated      = "run.updated"
	TopicJobUpdated      = "job.updated"
	TopicArtifactUpdated = "artifact.updated"
	TopicRunLog          = "run.log"
)

type Event struct {
	Type  string `json:"type"`
	RunID string `json:"runId,omitempty"`
	JobID string `json:"jobId,omitempty"`
	Data  any    `json:"data,omitempty"`
}

type subscriber struct {
	ch chan Event
}

type Bus struct {
	mu      sync.RWMutex
	subs    map[*subscriber]struct{}
	closed  bool
	dropped uint64
}

func NewBus() *Bus {
	return &Bus{subs: map[*subscriber]struct{}{}}
}

func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	s := &subscriber{ch: make(chan Event, buffer)}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(s.ch)
		return s.ch, func() {}
	}
	b.subs[s] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.subs[s]; ok {
				delete(b.subs, s)
				close(s.ch)
			}
			b.mu.Unlock()
		})
	}
	return s.ch, cancel
}

func (b *Bus) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for s := range b.subs {
		select {
		case s.ch <- ev:
		default:
			b.dropped++
		}
	}
}

func (b *Bus) Dropped() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.dropped
}

func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for s := range b.subs {
		delete(b.subs, s)
		close(s.ch)
	}
}
