package bus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"excel-libs/eventbus"
	"excel_core_service/base"
)

// Outbox writes events in the caller's business transaction and exposes dead
// letters for operational queries.
type Outbox struct {
	store *base.BusStore
}

// NewOutbox creates the business-facing outbox writer.
func NewOutbox(store *base.BusStore) *Outbox {
	return &Outbox{store: store}
}

// Enqueue inserts an event into the same core MySQL transaction as business data.
func (o *Outbox) Enqueue(ctx context.Context, tx *sql.Tx, event eventbus.Event) error {
	if o == nil || o.store == nil {
		return errors.New("outbox store is required")
	}
	return o.store.Enqueue(ctx, tx, event)
}

// DeadLetters returns events that exhausted consumer retries.
func (o *Outbox) DeadLetters(ctx context.Context, limit int) ([]base.DeadLetter, error) {
	if o == nil || o.store == nil {
		return nil, errors.New("outbox store is required")
	}
	return o.store.DeadLetters(ctx, limit)
}

// ForwarderConfig controls outbox polling and publish retry behavior.
type ForwarderConfig struct {
	BatchSize int
	Interval  time.Duration
	Retry     eventbus.RetryPolicy
}

// Forwarder claims unpublished outbox rows and publishes them after commit.
type Forwarder struct {
	store  *base.BusStore
	bus    eventbus.EventBus
	config ForwarderConfig

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewForwarder creates an outbox forwarder that can run once or in the background.
func NewForwarder(store *base.BusStore, broker eventbus.EventBus, config ForwarderConfig) *Forwarder {
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.Interval <= 0 {
		config.Interval = 100 * time.Millisecond
	}
	if err := config.Retry.Validate(); err != nil {
		config.Retry = eventbus.DefaultRetryPolicy()
	}
	return &Forwarder{
		store:  store,
		bus:    broker,
		config: config,
	}
}

// ForwardOnce claims and publishes one available batch.
func (f *Forwarder) ForwardOnce(ctx context.Context) (int, error) {
	if f == nil || f.store == nil {
		return 0, errors.New("forwarder store is required")
	}
	if f.bus == nil {
		return 0, errors.New("forwarder event bus is required")
	}

	tx, err := f.store.BeginTx(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	items, err := f.store.ClaimUnpublished(ctx, tx, f.config.BatchSize, now)
	if err != nil {
		return 0, err
	}
	published := 0
	var publishErrors []error
	for _, item := range items {
		if err := f.bus.Publish(ctx, item.Event); err != nil {
			attempt := item.PublishAttempts + 1
			dead := attempt >= f.config.Retry.MaxAttempts
			nextAttemptAt := now.Add(f.config.Retry.Delay(attempt))
			if markErr := f.store.MarkPublishFailure(
				ctx,
				tx,
				item,
				err,
				nextAttemptAt,
				dead,
				now,
			); markErr != nil {
				publishErrors = append(publishErrors, fmt.Errorf("mark publish failure for %s: %w", item.Event.Header.EventID, markErr))
				continue
			}
			publishErrors = append(publishErrors, fmt.Errorf("publish event %s: %w", item.Event.Header.EventID, err))
			continue
		}
		if err := f.store.MarkPublished(ctx, tx, item.Event.Header.EventID, now); err != nil {
			publishErrors = append(publishErrors, err)
			continue
		}
		published++
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit outbox forwarding: %w", err)
	}
	return published, errors.Join(publishErrors...)
}

// Start runs the forwarder until the context is canceled or Stop is called.
func (f *Forwarder) Start(ctx context.Context) error {
	f.mu.Lock()
	if f.cancel != nil {
		f.mu.Unlock()
		return errors.New("forwarder already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	f.done = make(chan struct{})
	done := f.done
	f.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(f.config.Interval)
		defer ticker.Stop()
		for {
			_, _ = f.ForwardOnce(runCtx)
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

// Stop terminates background forwarding and waits for the current poll.
func (f *Forwarder) Stop(ctx context.Context) error {
	f.mu.Lock()
	cancel := f.cancel
	done := f.done
	f.cancel = nil
	f.done = nil
	f.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
