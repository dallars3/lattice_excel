// bus/consumer.go：core 侧事件消费注册、幂等、退避与死信。消费幂等基于
// excel_bus_outbox 的 event_id 唯一键与 sent→acked/dead 状态迁移。
package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"excel-libs/eventbus"
	"excel_core_service/base"
)

// Handler 处理一条已通过幂等判定的事件；返回 error 表示本次尝试失败。
type Handler func(context.Context, eventbus.Event) error

// ConsumerConfig 控制每个事件的消费重试预算。
type ConsumerConfig struct {
	Retry eventbus.RetryPolicy
}

// Consumer 注册 core 侧处理器，并按 event_id 状态机保证仅生效一次。
type Consumer struct {
	store  *base.BusStore
	broker eventbus.EventBus
	config ConsumerConfig

	mu            sync.Mutex
	registrations []consumerRegistration
	active        []activeConsumerSubscription
	started       bool
	cancel        context.CancelFunc
	wait          sync.WaitGroup
}

type consumerRegistration struct {
	options eventbus.SubscribeOptions
	handler Handler
}

type activeConsumerSubscription struct {
	registration consumerRegistration
	subscription eventbus.Subscription
}

// NewConsumer 构造 core 事件消费者注册中心。
func NewConsumer(store *base.BusStore, broker eventbus.EventBus, config ConsumerConfig) *Consumer {
	if err := config.Retry.Validate(); err != nil {
		config.Retry = eventbus.DefaultRetryPolicy()
	}
	return &Consumer{
		store:  store,
		broker: broker,
		config: config,
	}
}

// Register 在 Start 前添加一个处理器。
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
	c.registrations = append(c.registrations, consumerRegistration{options: options, handler: handler})
	return nil
}

// Start 打开订阅并启动各自消费循环。
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

	active := make([]activeConsumerSubscription, 0, len(c.registrations))
	for _, registration := range c.registrations {
		subscription, err := c.broker.Subscribe(ctx, registration.options)
		if err != nil {
			for _, opened := range active {
				_ = opened.subscription.Close()
			}
			return fmt.Errorf("subscribe %s: %w", registration.options.Topic, err)
		}
		active = append(active, activeConsumerSubscription{
			registration: registration,
			subscription: subscription,
		})
	}

	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.active = active
	c.started = true
	for _, subscription := range active {
		c.wait.Add(1)
		go c.run(runCtx, subscription)
	}
	return nil
}

// Stop 取消所有消费循环并关闭订阅。
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
	for _, subscription := range active {
		_ = subscription.subscription.Close()
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

func (c *Consumer) run(ctx context.Context, active activeConsumerSubscription) {
	defer c.wait.Done()
	for {
		delivery, err := active.subscription.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, context.DeadlineExceeded) || !waitForCoreRetry(ctx, 100*time.Millisecond) {
				return
			}
			continue
		}
		if err := c.processDelivery(ctx, active, delivery); err != nil {
			if !waitForCoreRetry(ctx, 100*time.Millisecond) {
				return
			}
		}
	}
}

func (c *Consumer) processDelivery(
	ctx context.Context,
	active activeConsumerSubscription,
	delivery eventbus.Delivery,
) error {
	event := delivery.Event
	state, err := c.store.ConsumeState(ctx, event.Header.EventID)
	if err != nil {
		return err
	}
	switch {
	case !state.EventExists || state.Status == "acked" || state.Status == "dead":
		return active.subscription.Ack(ctx, delivery)
	case state.Status != "sent" || (state.NextConsumeAt.Valid && state.NextConsumeAt.Time.After(time.Now().UTC())):
		return errors.New("outbox event not ready for consumption")
	}

	item := base.OutboxItem{Event: event}
	for attempt := 1; attempt <= c.config.Retry.MaxAttempts; attempt++ {
		handlerErr := active.registration.handler(ctx, event)
		if handlerErr == nil {
			if err := c.store.Ack(ctx, event.Header.EventID, time.Now().UTC()); err != nil {
				return err
			}
			return active.subscription.Ack(ctx, delivery)
		}
		dead := attempt >= c.config.Retry.MaxAttempts
		nextAttemptAt := time.Now().UTC().Add(c.config.Retry.Delay(attempt))
		if _, err := c.store.MarkConsumeFailure(
			ctx,
			item,
			handlerErr,
			nextAttemptAt,
			dead,
			time.Now().UTC(),
		); err != nil {
			return err
		}
		if dead {
			return active.subscription.Ack(ctx, delivery)
		}
		if !waitForCoreRetry(ctx, c.config.Retry.Delay(attempt)) {
			return ctx.Err()
		}
	}
	return nil
}

func waitForCoreRetry(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
