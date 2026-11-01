// redis/eventbridge.go：core 侧 go-redis → 共享 EventBus 的 RedisBridge 适配器。
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

// EventBridge 将 go-redis 适配为协议无关的 eventbus.RedisBridge。
type EventBridge struct {
	client *goredis.Client
}

// NewEventBridge 创建 Redis Streams 与 PubSub 桥接。
func NewEventBridge(client *goredis.Client) *EventBridge {
	return &EventBridge{client: client}
}

// XAdd 追加一条编码事件到 stream。
func (b *EventBridge) XAdd(ctx context.Context, stream string, payload []byte) (string, error) {
	return b.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{"payload": payload},
	}).Result()
}

// Publish 在 PubSub channel 上广播一条编码事件。
func (b *EventBridge) Publish(ctx context.Context, channel string, payload []byte) error {
	return b.client.Publish(ctx, channel, payload).Err()
}

// EnsureGroup 从 stream 起始位置创建消费组（已存在则忽略）。
func (b *EventBridge) EnsureGroup(ctx context.Context, stream string, group string) error {
	err := b.client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

// ReadGroup 用 XREADGROUP 读取新条目。
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

// ListPending 用 XPENDING 找出空闲超时的未确认条目（Redis 5 无 XAUTOCLAIM）。
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

// Claim 用 XCLAIM 接管空闲未确认条目。
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
		payload, payloadErr := payloadFromValues(message.Values)
		if payloadErr != nil {
			return nil, payloadErr
		}
		records = append(records, eventbus.RedisRecord{ID: message.ID, Payload: payload})
	}
	return records, nil
}

// XAck 确认已消费的 stream 条目。
func (b *EventBridge) XAck(ctx context.Context, stream string, group string, ids ...string) error {
	return b.client.XAck(ctx, stream, group, ids...).Err()
}

// PubSub 订阅瞬时 channel。
func (b *EventBridge) PubSub(ctx context.Context, channel string) (eventbus.RedisPubSubSource, error) {
	return newPubSubSource(b.client.Subscribe(ctx, channel)), nil
}

type corePubSubSource struct {
	source *goredis.PubSub
}

func newPubSubSource(source *goredis.PubSub) *corePubSubSource {
	return &corePubSubSource{source: source}
}

func (s *corePubSubSource) Receive(ctx context.Context) ([]byte, error) {
	message, err := s.source.ReceiveMessage(ctx)
	if err != nil {
		return nil, err
	}
	return []byte(message.Payload), nil
}

func (s *corePubSubSource) Close() error {
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
