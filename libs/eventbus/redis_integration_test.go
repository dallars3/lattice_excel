package eventbus_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"excel-libs/eventbus"
	"excel-libs/testredis"
	goredis "github.com/redis/go-redis/v9"
)

func TestRedisBusRecoversDeletedConsumerGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("需要本机 Redis（127.0.0.1:6379）")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := testredis.Open(ctx, "127.0.0.1:6379")
	if err != nil {
		t.Fatalf("testredis.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	unique := time.Now().UnixNano()
	eventType := fmt.Sprintf("test.self-heal.%d", unique)
	streamPrefix := fmt.Sprintf("test:eventbus-self-heal:%d:", unique)
	channelPrefix := fmt.Sprintf("test:eventbus-self-heal-live:%d:", unique)
	streamKey := streamPrefix + eventType
	group := fmt.Sprintf("test-self-heal-%d", unique)
	t.Cleanup(func() {
		_ = client.Del(context.Background(), streamKey).Err()
	})

	bus := eventbus.NewRedis(newIntegrationRedisBridge(client), eventbus.RedisConfig{
		StreamPrefix:   streamPrefix,
		ChannelPrefix:  channelPrefix,
		PendingMinIdle: 100 * time.Millisecond,
	})
	subscription, err := bus.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic:    eventType,
		Mode:     eventbus.DeliveryModeReliable,
		Group:    group,
		Consumer: "test-self-heal-consumer",
		Count:    1,
		Block:    100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer subscription.Close()

	if deleted, err := client.XGroupDestroy(ctx, streamKey, group).Result(); err != nil {
		t.Fatalf("XGroupDestroy() error = %v", err)
	} else if deleted != 1 {
		t.Fatalf("XGroupDestroy() deleted = %d, want 1", deleted)
	}

	event := testEvent(fmt.Sprintf("evt-self-heal-%d", unique), eventType)
	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	delivery, err := subscription.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive() after deleting consumer group error = %v", err)
	}
	if delivery.Event.Header.EventID != event.Header.EventID {
		t.Fatalf("delivered event_id = %q, want %q", delivery.Event.Header.EventID, event.Header.EventID)
	}
	if err := subscription.Ack(ctx, delivery); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if !redisGroupExists(t, ctx, client, streamKey, group) {
		t.Fatalf("consumer group %q was not recreated on stream %q", group, streamKey)
	}
}

type integrationRedisBridge struct {
	client *goredis.Client
}

func newIntegrationRedisBridge(client *goredis.Client) *integrationRedisBridge {
	return &integrationRedisBridge{client: client}
}

func (b *integrationRedisBridge) XAdd(
	ctx context.Context,
	stream string,
	payload []byte,
) (string, error) {
	return b.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{"payload": payload},
	}).Result()
}

func (b *integrationRedisBridge) Publish(ctx context.Context, channel string, payload []byte) error {
	return b.client.Publish(ctx, channel, payload).Err()
}

func (b *integrationRedisBridge) EnsureGroup(ctx context.Context, stream, group string) error {
	err := b.client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

func (b *integrationRedisBridge) ReadGroup(
	ctx context.Context,
	stream, group, consumer string,
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
			payload, err := integrationPayload(message.Values)
			if err != nil {
				return nil, err
			}
			records = append(records, eventbus.RedisRecord{ID: message.ID, Payload: payload})
		}
	}
	return records, nil
}

func (b *integrationRedisBridge) ListPending(
	ctx context.Context,
	stream, group string,
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

func (b *integrationRedisBridge) Claim(
	ctx context.Context,
	stream, group, consumer string,
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
		payload, err := integrationPayload(message.Values)
		if err != nil {
			return nil, err
		}
		records = append(records, eventbus.RedisRecord{ID: message.ID, Payload: payload})
	}
	return records, nil
}

func (b *integrationRedisBridge) XAck(
	ctx context.Context,
	stream, group string,
	ids ...string,
) error {
	return b.client.XAck(ctx, stream, group, ids...).Err()
}

func (b *integrationRedisBridge) PubSub(
	context.Context,
	string,
) (eventbus.RedisPubSubSource, error) {
	return nil, errors.New("integration bridge pubsub is not used")
}

func integrationPayload(values map[string]interface{}) ([]byte, error) {
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

func redisGroupExists(
	t *testing.T,
	ctx context.Context,
	client *goredis.Client,
	stream, group string,
) bool {
	t.Helper()
	groups, err := client.XInfoGroups(ctx, stream).Result()
	if err != nil {
		t.Fatalf("XInfoGroups() error = %v", err)
	}
	for _, info := range groups {
		if info.Name == group {
			return true
		}
	}
	return false
}
