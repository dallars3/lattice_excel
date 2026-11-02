// redis/eventbridge.go：web_api 侧 go-redis → 共享 EventBus 的 RedisBridge 适配器。
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

// EventBridge adapts go-redis to the shared eventbus.RedisBridge contract.
type EventBridge struct {
	client *goredis.Client
}

var _ eventbus.RedisBridge = (*EventBridge)(nil)

// NewEventBridge creates a Redis Streams and PubSub bridge.
func NewEventBridge(client *goredis.Client) *EventBridge {
	return &EventBridge{client: client}
}

// XAdd appends one encoded event to a stream.
func (bridge *EventBridge) XAdd(
	ctx context.Context,
	stream string,
	payload []byte,
) (string, error) {
	return bridge.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{"payload": payload},
	}).Result()
}

// Publish broadcasts one encoded event on a PubSub channel.
func (bridge *EventBridge) Publish(ctx context.Context, channel string, payload []byte) error {
	return bridge.client.Publish(ctx, channel, payload).Err()
}

// EnsureGroup creates a stream consumer group when needed.
func (bridge *EventBridge) EnsureGroup(ctx context.Context, stream string, group string) error {
	err := bridge.client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

// ReadGroup reads new entries assigned to a consumer group.
func (bridge *EventBridge) ReadGroup(
	ctx context.Context,
	stream string,
	group string,
	consumer string,
	count int64,
	block time.Duration,
) ([]eventbus.RedisRecord, error) {
	streams, err := bridge.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
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
	records := make([]eventbus.RedisRecord, 0)
	for _, item := range streams {
		for _, message := range item.Messages {
			payload, payloadErr := payloadFromValues(message.Values)
			if payloadErr != nil {
				return nil, payloadErr
			}
			records = append(records, eventbus.RedisRecord{ID: message.ID, Payload: payload})
		}
	}
	return records, nil
}

// ListPending returns stream entries idle longer than minIdle.
func (bridge *EventBridge) ListPending(
	ctx context.Context,
	stream string,
	group string,
	minIdle time.Duration,
	count int64,
) ([]eventbus.RedisPending, error) {
	pending, err := bridge.client.XPendingExt(ctx, &goredis.XPendingExtArgs{
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

// Claim transfers idle pending entries to another consumer.
func (bridge *EventBridge) Claim(
	ctx context.Context,
	stream string,
	group string,
	consumer string,
	minIdle time.Duration,
	ids []string,
) ([]eventbus.RedisRecord, error) {
	messages, err := bridge.client.XClaim(ctx, &goredis.XClaimArgs{
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
		payload, payloadErr := payloadFromValues(message.Values)
		if payloadErr != nil {
			return nil, payloadErr
		}
		records = append(records, eventbus.RedisRecord{ID: message.ID, Payload: payload})
	}
	return records, nil
}

// XAck acknowledges consumed stream entries.
func (bridge *EventBridge) XAck(
	ctx context.Context,
	stream string,
	group string,
	ids ...string,
) error {
	return bridge.client.XAck(ctx, stream, group, ids...).Err()
}

// PubSub opens one transient channel source.
func (bridge *EventBridge) PubSub(
	ctx context.Context,
	channel string,
) (eventbus.RedisPubSubSource, error) {
	source := bridge.client.Subscribe(ctx, channel)
	if err := source.Ping(ctx); err != nil {
		_ = source.Close()
		return nil, fmt.Errorf("confirm pubsub subscription: %w", err)
	}
	return newPubSubSource(source), nil
}

type webPubSubSource struct {
	source *goredis.PubSub
}

func newPubSubSource(source *goredis.PubSub) *webPubSubSource {
	return &webPubSubSource{source: source}
}

func (source *webPubSubSource) Receive(ctx context.Context) ([]byte, error) {
	message, err := source.source.ReceiveMessage(ctx)
	if err != nil {
		return nil, err
	}
	return []byte(message.Payload), nil
}

func (source *webPubSubSource) Close() error {
	return source.source.Close()
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
