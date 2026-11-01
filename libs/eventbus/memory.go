package eventbus

import (
	"context"
	"errors"
	"sync"
)

// MemoryBus is an in-memory EventBus fake for tests and local replacements.
type MemoryBus struct {
	mu            sync.RWMutex
	subscriptions map[*memorySubscription]struct{}
}

// NewMemoryBus creates an empty in-memory EventBus.
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{subscriptions: make(map[*memorySubscription]struct{})}
}

// Publish delivers a copy of event to every matching active subscription.
func (b *MemoryBus) Publish(ctx context.Context, event Event) error {
	if err := event.Validate(); err != nil {
		return err
	}

	b.mu.RLock()
	targets := make([]*memorySubscription, 0, len(b.subscriptions))
	for subscription := range b.subscriptions {
		if subscription.options.Topic == event.Header.Type {
			targets = append(targets, subscription)
		}
	}
	b.mu.RUnlock()

	for _, subscription := range targets {
		if subscription.isClosed() {
			continue
		}
		select {
		case subscription.deliveries <- Delivery{Event: event.Clone()}:
		case <-subscription.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Subscribe registers an in-memory consumer. Reliable group behavior is
// intentionally omitted because exactly-once business behavior belongs to the
// consumer registration layer, not the bus transport.
func (b *MemoryBus) Subscribe(_ context.Context, options SubscribeOptions) (Subscription, error) {
	if options.Mode == "" {
		options.Mode = DeliveryModeReliable
	}
	if options.Topic == "" {
		return nil, errors.New("topic is required")
	}
	if options.Mode != DeliveryModeReliable && options.Mode != DeliveryModePubSub {
		return nil, errors.New("unsupported delivery mode")
	}
	size := options.QueueSize
	if size == 0 {
		size = 128
	}
	subscription := &memorySubscription{
		bus:        b,
		options:    options,
		deliveries: make(chan Delivery, size),
		done:       make(chan struct{}),
	}
	b.mu.Lock()
	b.subscriptions[subscription] = struct{}{}
	b.mu.Unlock()
	return subscription, nil
}

func (b *MemoryBus) remove(subscription *memorySubscription) {
	b.mu.Lock()
	delete(b.subscriptions, subscription)
	b.mu.Unlock()
}

type memorySubscription struct {
	bus        *MemoryBus
	options    SubscribeOptions
	deliveries chan Delivery
	done       chan struct{}
	closeOnce  sync.Once
}

func (s *memorySubscription) Receive(ctx context.Context) (Delivery, error) {
	select {
	case delivery := <-s.deliveries:
		return delivery, nil
	case <-s.done:
		return Delivery{}, errors.New("subscription closed")
	case <-ctx.Done():
		return Delivery{}, ctx.Err()
	}
}

func (s *memorySubscription) Ack(context.Context, Delivery) error {
	if s.isClosed() {
		return errors.New("subscription closed")
	}
	return nil
}

func (s *memorySubscription) Close() error {
	s.closeOnce.Do(func() {
		s.bus.remove(s)
		close(s.done)
	})
	return nil
}

func (s *memorySubscription) isClosed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}
