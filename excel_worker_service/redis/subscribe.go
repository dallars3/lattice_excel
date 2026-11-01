package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"excel-libs/eventbus"
	goredis "github.com/redis/go-redis/v9"
)

// Open connects to Redis and verifies it with PING.
func Open(ctx context.Context, addr string) (*goredis.Client, error) {
	client := goredis.NewClient(&goredis.Options{
		Addr:         addr,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}

// EventBridge adapts go-redis to the shared Redis EventBus implementation.
type EventBridge struct {
	client *goredis.Client
}

// NewEventBridge creates a Redis Streams and PubSub bridge.
func NewEventBridge(client *goredis.Client) *EventBridge {
	return &EventBridge{client: client}
}

// XAdd appends one encoded event to a stream.
func (b *EventBridge) XAdd(ctx context.Context, stream string, payload []byte) (string, error) {
	return b.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{"payload": payload},
	}).Result()
}

// Publish broadcasts one encoded event on a PubSub channel.
func (b *EventBridge) Publish(ctx context.Context, channel string, payload []byte) error {
	if err := b.client.Publish(ctx, channel, payload).Err(); err != nil {
		return err
	}
	return nil
}

// EnsureGroup creates a consumer group at the beginning of the stream.
func (b *EventBridge) EnsureGroup(ctx context.Context, stream string, group string) error {
	err := b.client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

// ReadGroup reads new entries using XREADGROUP.
func (b *EventBridge) ReadGroup(
	ctx context.Context,
	stream string,
	group string,
	consumer string,
	count int64,
	block time.Duration,
) ([]eventbus.RedisRecord, error) {
	streams, err := b.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{stream, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []eventbus.RedisRecord
	for _, item := range streams {
		for _, message := range item.Messages {
			payload, err := payloadFromValues(message.Values)
			if err != nil {
				return nil, err
			}
			records = append(records, eventbus.RedisRecord{ID: message.ID, Payload: payload})
		}
	}
	return records, nil
}

// ListPending finds stale pending entries with XPENDING so Redis 5 can recover
// them without the Redis 6.2 XAUTOCLAIM command.
func (b *EventBridge) ListPending(
	ctx context.Context,
	stream string,
	group string,
	minIdle time.Duration,
	count int64,
) ([]eventbus.RedisPending, error) {
	pending, err := b.client.XPendingExt(ctx, &goredis.XPendingExtArgs{
		Stream: stream,
		Group:  group,
		Start:  "-",
		End:    "+",
		Count:  count,
	}).Result()
	if err != nil {
		return nil, err
	}
	items := make([]eventbus.RedisPending, 0, len(pending))
	for _, item := range pending {
		if item.Idle < minIdle {
			continue
		}
		items = append(items, eventbus.RedisPending{ID: item.ID, Idle: item.Idle})
	}
	return items, nil
}

// Claim transfers stale pending entries with XCLAIM.
func (b *EventBridge) Claim(
	ctx context.Context,
	stream string,
	group string,
	consumer string,
	minIdle time.Duration,
	ids []string,
) ([]eventbus.RedisRecord, error) {
	messages, err := b.client.XClaim(ctx, &goredis.XClaimArgs{
		Stream:   stream,
		Group:    group,
		Consumer: consumer,
		MinIdle:  minIdle,
		Messages: ids,
	}).Result()
	if err != nil {
		return nil, err
	}
	records := make([]eventbus.RedisRecord, 0, len(messages))
	for _, message := range messages {
		payload, err := payloadFromValues(message.Values)
		if err != nil {
			return nil, err
		}
		records = append(records, eventbus.RedisRecord{ID: message.ID, Payload: payload})
	}
	return records, nil
}

// XAck acknowledges consumed stream entries.
func (b *EventBridge) XAck(ctx context.Context, stream string, group string, ids ...string) error {
	return b.client.XAck(ctx, stream, group, ids...).Err()
}

// PubSub subscribes to a transient Redis channel.
func (b *EventBridge) PubSub(ctx context.Context, channel string) (eventbus.RedisPubSubSource, error) {
	source := b.client.Subscribe(ctx, channel)
	return &pubSubSource{source: source}, nil
}

type pubSubSource struct {
	source *goredis.PubSub
}

func (s *pubSubSource) Receive(ctx context.Context) ([]byte, error) {
	message, err := s.source.ReceiveMessage(ctx)
	if err != nil {
		return nil, err
	}
	return []byte(message.Payload), nil
}

func (s *pubSubSource) Close() error {
	return s.source.Close()
}

func payloadFromValues(values map[string]interface{}) ([]byte, error) {
	value, ok := values["payload"]
	if !ok {
		return nil, errors.New("stream entry missing payload")
	}
	switch typed := value.(type) {
	case string:
		return []byte(typed), nil
	case []byte:
		return append([]byte(nil), typed...), nil
	default:
		return nil, fmt.Errorf("stream payload has unsupported type %T", value)
	}
}
