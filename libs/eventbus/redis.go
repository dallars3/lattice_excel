package eventbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	consumerGroupRecoveryInitialDelay = time.Second
	consumerGroupRecoveryMaxDelay     = time.Minute
)

var consumerGroupRecoveryPolicy = RetryPolicy{
	Initial:     consumerGroupRecoveryInitialDelay,
	Max:         consumerGroupRecoveryMaxDelay,
	MaxAttempts: 1,
}

// RedisRecord is a transport-neutral Streams or PubSub record.
type RedisRecord struct {
	ID      string
	Payload []byte
}

// RedisPending is one entry returned by XPENDING.
type RedisPending struct {
	ID   string
	Idle time.Duration
}

// RedisPubSubSource is a transport-neutral PubSub receiver.
type RedisPubSubSource interface {
	Receive(context.Context) ([]byte, error)
	Close() error
}

// RedisBridge isolates the shared Redis EventBus from a concrete client. The
// service redis packages provide the go-redis adapter.
type RedisBridge interface {
	XAdd(context.Context, string, []byte) (string, error)
	Publish(context.Context, string, []byte) error
	EnsureGroup(context.Context, string, string) error
	ReadGroup(context.Context, string, string, string, int64, time.Duration) ([]RedisRecord, error)
	ListPending(context.Context, string, string, time.Duration, int64) ([]RedisPending, error)
	Claim(context.Context, string, string, string, time.Duration, []string) ([]RedisRecord, error)
	XAck(context.Context, string, string, ...string) error
	PubSub(context.Context, string) (RedisPubSubSource, error)
}

// RedisConfig configures key prefixes and pending-message recovery.
type RedisConfig struct {
	StreamPrefix   string
	ChannelPrefix  string
	PendingMinIdle time.Duration
}

// RedisBus implements EventBus with Redis Streams plus PubSub.
type RedisBus struct {
	bridge RedisBridge
	config RedisConfig
}

// NewRedis creates the default reliable Streams and transient PubSub bus.
func NewRedis(bridge RedisBridge, config RedisConfig) *RedisBus {
	if config.StreamPrefix == "" {
		config.StreamPrefix = "excel:events:"
	}
	if config.ChannelPrefix == "" {
		config.ChannelPrefix = "excel:live:"
	}
	if config.PendingMinIdle <= 0 {
		config.PendingMinIdle = 30 * time.Second
	}
	config.StreamPrefix = ensurePrefixSuffix(config.StreamPrefix)
	config.ChannelPrefix = ensurePrefixSuffix(config.ChannelPrefix)
	return &RedisBus{bridge: bridge, config: config}
}

// Publish appends to Streams first, then broadcasts on PubSub.
func (b *RedisBus) Publish(ctx context.Context, event Event) error {
	if b == nil || b.bridge == nil {
		return errors.New("redis event bus bridge is required")
	}
	payload, err := marshalEvent(event)
	if err != nil {
		return err
	}
	if _, err := b.bridge.XAdd(ctx, b.config.StreamPrefix+event.Header.Type, payload); err != nil {
		return fmt.Errorf("append event stream: %w", err)
	}
	if err := b.bridge.Publish(ctx, b.config.ChannelPrefix+event.Header.Type, payload); err != nil {
		return fmt.Errorf("publish transient event: %w", err)
	}
	return nil
}

// Subscribe creates a reliable consumer-group or transient PubSub subscription.
func (b *RedisBus) Subscribe(ctx context.Context, options SubscribeOptions) (Subscription, error) {
	if b == nil || b.bridge == nil {
		return nil, errors.New("redis event bus bridge is required")
	}
	if options.Topic == "" {
		return nil, errors.New("topic is required")
	}
	if options.Mode == "" {
		options.Mode = DeliveryModeReliable
	}
	if options.Count <= 0 {
		options.Count = 1
	}
	if options.Block <= 0 {
		options.Block = time.Second
	}
	if options.PendingMinIdle <= 0 {
		options.PendingMinIdle = b.config.PendingMinIdle
	}

	stream := b.config.StreamPrefix + options.Topic
	subscription := &redisSubscription{
		bus:     b,
		options: options,
		stream:  stream,
		done:    make(chan struct{}),
	}
	switch options.Mode {
	case DeliveryModeReliable:
		if options.Group == "" || options.Consumer == "" {
			return nil, errors.New("reliable subscription requires group and consumer")
		}
		if err := b.bridge.EnsureGroup(ctx, stream, options.Group); err != nil {
			return nil, fmt.Errorf("ensure consumer group: %w", err)
		}
	case DeliveryModePubSub:
		source, err := b.bridge.PubSub(ctx, b.config.ChannelPrefix+options.Topic)
		if err != nil {
			return nil, fmt.Errorf("subscribe pubsub: %w", err)
		}
		subscription.pubSub = source
	default:
		return nil, fmt.Errorf("unsupported delivery mode %q", options.Mode)
	}
	return subscription, nil
}

type redisSubscription struct {
	bus     *RedisBus
	options SubscribeOptions
	stream  string
	pubSub  RedisPubSubSource

	mu        sync.Mutex
	queue     []RedisRecord
	closeOnce sync.Once
	done      chan struct{}
	closeErr  error

	recoveryMu       sync.Mutex
	recoveryFailures int
	nextRecoveryAt   time.Time
}

func (s *redisSubscription) Receive(ctx context.Context) (Delivery, error) {
	if s.options.Mode == DeliveryModePubSub {
		return s.receivePubSub(ctx)
	}
	return s.receiveStream(ctx)
}

func (s *redisSubscription) receivePubSub(ctx context.Context) (Delivery, error) {
	payload, err := s.pubSub.Receive(ctx)
	if err != nil {
		return Delivery{}, err
	}
	event, err := unmarshalEvent(payload)
	if err != nil {
		return Delivery{}, err
	}
	return Delivery{Event: event}, nil
}

func (s *redisSubscription) receiveStream(ctx context.Context) (Delivery, error) {
	for {
		delivery, err := s.receiveStreamAttempt(ctx)
		if !isConsumerGroupMissing(err) {
			return delivery, err
		}
		if recoverErr := s.recoverConsumerGroup(ctx, err); recoverErr != nil {
			return Delivery{}, recoverErr
		}
	}
}

func (s *redisSubscription) receiveStreamAttempt(ctx context.Context) (Delivery, error) {
	s.mu.Lock()
	if len(s.queue) > 0 {
		record := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		return s.deliveryFromRecord(record)
	}
	s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return Delivery{}, err
	}
	pending, err := s.bus.bridge.ListPending(
		ctx,
		s.stream,
		s.options.Group,
		s.options.PendingMinIdle,
		s.options.Count,
	)
	if err != nil {
		return Delivery{}, fmt.Errorf("list pending stream entries: %w", err)
	}
	if len(pending) > 0 {
		ids := make([]string, 0, len(pending))
		for _, item := range pending {
			ids = append(ids, item.ID)
		}
		records, err := s.bus.bridge.Claim(
			ctx,
			s.stream,
			s.options.Group,
			s.options.Consumer,
			s.options.PendingMinIdle,
			ids,
		)
		if err != nil {
			return Delivery{}, fmt.Errorf("claim pending stream entries: %w", err)
		}
		if len(records) > 0 {
			s.enqueue(records[1:])
			return s.deliveryFromRecord(records[0])
		}
	}

	records, err := s.bus.bridge.ReadGroup(
		ctx,
		s.stream,
		s.options.Group,
		s.options.Consumer,
		s.options.Count,
		s.options.Block,
	)
	if err != nil {
		return Delivery{}, fmt.Errorf("read consumer group: %w", err)
	}
	if len(records) == 0 {
		return Delivery{}, context.DeadlineExceeded
	}
	s.enqueue(records[1:])
	return s.deliveryFromRecord(records[0])
}

func isConsumerGroupMissing(err error) bool {
	return err != nil && strings.Contains(err.Error(), "NOGROUP")
}

func (s *redisSubscription) recoverConsumerGroup(ctx context.Context, cause error) error {
	now := time.Now()
	s.recoveryMu.Lock()
	if now.Before(s.nextRecoveryAt) {
		wait := time.Until(s.nextRecoveryAt)
		s.recoveryMu.Unlock()
		return fmt.Errorf(
			"consumer group %q recovery on stream %q is rate limited for %s: %w",
			s.options.Group,
			s.stream,
			wait.Round(time.Millisecond),
			cause,
		)
	}
	s.recoveryFailures++
	attempt := s.recoveryFailures
	retryDelay := consumerGroupRecoveryPolicy.Delay(attempt)
	s.nextRecoveryAt = now.Add(retryDelay)
	s.recoveryMu.Unlock()

	if err := s.bus.bridge.EnsureGroup(ctx, s.stream, s.options.Group); err != nil {
		slog.Error(
			"eventbus: recover Redis consumer group",
			"stream", s.stream,
			"group", s.options.Group,
			"topic", s.options.Topic,
			"consumer", s.options.Consumer,
			"attempt", attempt,
			"retry_in", retryDelay,
			"error", err,
		)
		return fmt.Errorf(
			"recover consumer group %q on stream %q after %d attempt(s), retry in %s: %w",
			s.options.Group,
			s.stream,
			attempt,
			retryDelay,
			err,
		)
	}

	s.recoveryMu.Lock()
	s.recoveryFailures = 0
	s.nextRecoveryAt = time.Time{}
	s.recoveryMu.Unlock()
	return nil
}

func (s *redisSubscription) deliveryFromRecord(record RedisRecord) (Delivery, error) {
	event, err := unmarshalEvent(record.Payload)
	if err != nil {
		return Delivery{}, err
	}
	return Delivery{Event: event, RecordID: record.ID}, nil
}

func (s *redisSubscription) enqueue(records []RedisRecord) {
	if len(records) == 0 {
		return
	}
	s.mu.Lock()
	s.queue = append(s.queue, records...)
	s.mu.Unlock()
}

func (s *redisSubscription) Ack(ctx context.Context, delivery Delivery) error {
	if s.options.Mode == DeliveryModePubSub {
		return nil
	}
	if delivery.RecordID == "" {
		return errors.New("stream delivery record id is required")
	}
	if err := s.bus.bridge.XAck(ctx, s.stream, s.options.Group, delivery.RecordID); err != nil {
		return fmt.Errorf("ack stream entry: %w", err)
	}
	return nil
}

func (s *redisSubscription) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.pubSub != nil {
			s.closeErr = s.pubSub.Close()
		}
	})
	return s.closeErr
}

func ensurePrefixSuffix(value string) string {
	if !strings.HasSuffix(value, ":") {
		return value + ":"
	}
	return value
}
