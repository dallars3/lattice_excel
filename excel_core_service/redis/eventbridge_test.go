// redis/eventbridge_test.go：证明 core 侧桥接实现 eventbus.RedisBridge，
// 可驱动共享 RedisBus 做可靠 Streams 发布与消费组读取。
package redis_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"excel-libs/eventbus"
	redisclient "excel_core_service/redis"
	goredis "github.com/redis/go-redis/v9"
)

func TestCoreEventBridgeDrivesSharedRedisBus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := openTestClient(ctx)
	if err != nil {
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	unique := time.Now().UnixNano()
	streamKey := fmt.Sprintf("test:core:%d:", unique) + "test.bridge"
	group := fmt.Sprintf("core-test-%d", unique)
	t.Cleanup(func() {
		_ = client.Del(context.Background(), streamKey).Err()
	})

	bus := eventbus.NewRedis(redisclient.NewEventBridge(client), eventbus.RedisConfig{
		StreamPrefix: fmt.Sprintf("test:core:%d:", unique),
		ChannelPrefix: fmt.Sprintf("test:core-pub:%d:", unique),
	})
	subscription, err := bus.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic:    "test.bridge",
		Mode:     eventbus.DeliveryModeReliable,
		Group:    group,
		Consumer: "core-test",
		Count:    1,
		Block:    100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer subscription.Close()

	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    fmt.Sprintf("evt_%d", unique),
			Type:       "test.bridge",
			Version:    1,
			BaseID:     "base_bridge",
			Seq:        3,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "excel_core_service.test",
		},
		Payload: []byte(`{"ok":true}`),
	}
	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	delivery, err := subscription.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if delivery.Event.Header.EventID != event.Header.EventID {
		t.Fatalf("event_id = %q, want %q", delivery.Event.Header.EventID, event.Header.EventID)
	}
	if err := subscription.Ack(ctx, delivery); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
}

func openTestClient(ctx context.Context) (*goredis.Client, error) {
	return redisclient.Open(ctx, "127.0.0.1:6379")
}
