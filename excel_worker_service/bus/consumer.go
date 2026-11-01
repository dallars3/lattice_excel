package bus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"excel-libs/eventbus"
	"excel_worker_service/base"
)

// Handler processes one event after worker-side deduplication.
type Handler func(context.Context, eventbus.Event) error

// ConsumerConfig controls per-event retry before worker-local dead marking.
type ConsumerConfig struct {
	Retry eventbus.RetryPolicy
}

// Consumer registers worker handlers and enforces event_id uniqueness.
type Consumer struct {
	store  *base.ConsumerStore
	broker eventbus.EventBus
	config ConsumerConfig

	mu            sync.Mutex
	registrations []registration
	active        []activeSubscription
	cancel        context.CancelFunc
	started       bool
	wait          sync.WaitGroup
}

type registration struct {
	options eventbus.SubscribeOptions
	handler Handler
}

type activeSubscription struct {
	registration registration
	subscription eventbus.Subscription
}

// NewConsumer constructs a worker consumer registry.
func NewConsumer(db *sql.DB, broker eventbus.EventBus, config ConsumerConfig) *Consumer {
	if err := config.Retry.Validate(); err != nil {
		config.Retry = eventbus.DefaultRetryPolicy()
	}
	return &Consumer{
		store:  base.NewConsumerStore(db),
		broker: broker,
		config: config,
	}
}

// Register adds one handler before Start.
func (c *Consumer) Register(options eventbus.SubscribeOptions, handler Handler) error {
	if options.Topic == "" {
		return errors.New("subscription topic is required")
	}
	if handler == nil {
		return errors.New("event handler is required")
	}
	if options.Mode == "" {
		options.Mode = eventbus.DeliveryModeReliable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return errors.New("consumer already started")
	}
	c.registrations = append(c.registrations, registration{options: options, handler: handler})
	return nil
}

// Start opens all subscriptions and launches consumer loops.
func (c *Consumer) Start(ctx context.Context) error {
	if c == nil || c.broker == nil {
		return errors.New("consumer event bus is required")
	}
	if c.store == nil {
		return errors.New("consumer store is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return errors.New("consumer already started")
	}

	active := make([]activeSubscription, 0, len(c.registrations))
	for _, item := range c.registrations {
		subscription, err := c.broker.Subscribe(ctx, item.options)
		if err != nil {
			for _, opened := range active {
				_ = opened.subscription.Close()
			}
			return fmt.Errorf("subscribe %s: %w", item.options.Topic, err)
		}
		active = append(active, activeSubscription{registration: item, subscription: subscription})
	}

	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.active = active
	c.started = true
	for _, item := range active {
		c.wait.Add(1)
		go c.run(runCtx, item)
	}
	return nil
}

// Stop cancels all consumer loops and closes their subscriptions.
func (c *Consumer) Stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	cancel := c.cancel
	active := c.active
	c.cancel = nil
	c.active = nil
	c.started = false
	c.mu.Unlock()

	cancel()
	for _, item := range active {
		_ = item.subscription.Close()
	}

	done := make(chan struct{})
	go func() {
		c.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Consumer) run(ctx context.Context, active activeSubscription) {
	defer c.wait.Done()
	for {
		delivery, err := active.subscription.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			if !waitForRetry(ctx, 100*time.Millisecond) {
				return
			}
			continue
		}
		if err := c.processDelivery(ctx, active, delivery); err != nil {
			if !waitForRetry(ctx, 100*time.Millisecond) {
				return
			}
		}
	}
}

func (c *Consumer) processDelivery(
	ctx context.Context,
	active activeSubscription,
	delivery eventbus.Delivery,
) error {
	shouldProcess, err := c.store.Reserve(
		ctx,
		delivery.Event.Header.EventID,
		active.registration.options.Consumer,
	)
	if err != nil {
		return err
	}
	if !shouldProcess {
		return active.subscription.Ack(ctx, delivery)
	}

	for attempt := 1; attempt <= c.config.Retry.MaxAttempts; attempt++ {
		handlerErr := active.registration.handler(ctx, delivery.Event)
		if handlerErr == nil {
			if err := c.store.MarkProcessed(ctx, delivery.Event.Header.EventID, attempt); err != nil {
				return err
			}
			return active.subscription.Ack(ctx, delivery)
		}
		if attempt == c.config.Retry.MaxAttempts {
			if err := c.store.MarkDead(ctx, delivery.Event.Header.EventID, attempt, handlerErr); err != nil {
				return err
			}
			return active.subscription.Ack(ctx, delivery)
		}
		if err := c.store.MarkFailed(ctx, delivery.Event.Header.EventID, attempt, handlerErr); err != nil {
			return err
		}
		if !waitForRetry(ctx, c.config.Retry.Delay(attempt)) {
			return ctx.Err()
		}
	}
	return nil
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
