package broker

import (
	"context"
	"sync"

	"github.com/sangjinsu/orbis/internal/protocol"
)

type Broker struct {
	mu          sync.Mutex
	subscribers map[string]map[chan protocol.RuntimeEvent]struct{}
}

func New() *Broker {
	return &Broker{subscribers: map[string]map[chan protocol.RuntimeEvent]struct{}{}}
}

func (b *Broker) Subscribe(ctx context.Context, sessionID string) (<-chan protocol.RuntimeEvent, func()) {
	ch := make(chan protocol.RuntimeEvent, 16)
	b.mu.Lock()
	if b.subscribers[sessionID] == nil {
		b.subscribers[sessionID] = map[chan protocol.RuntimeEvent]struct{}{}
	}
	b.subscribers[sessionID][ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if subs := b.subscribers[sessionID]; subs != nil {
				delete(subs, ch)
				if len(subs) == 0 {
					delete(b.subscribers, sessionID)
				}
			}
			close(ch)
		})
	}

	go func() {
		<-ctx.Done()
		unsubscribe()
	}()

	return ch, unsubscribe
}

func (b *Broker) Publish(event protocol.RuntimeEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers[event.SessionID] {
		select {
		case ch <- event:
		default:
		}
	}
}
