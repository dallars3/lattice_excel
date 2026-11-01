package bus_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"excel-libs/eventbus"
	"excel_worker_service/base"
	"excel_worker_service/bus"
	workerredis "excel_worker_service/redis"
	goredis "github.com/redis/go-redis/v9"
)

func TestRedisStreamsDuplicateEventIDIsHandledOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db := openWorkerDB(t)
	config, err := base.LoadConfig(filepath.Join("..", "configs", "worker_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	client, err := workerredis.Open(ctx, config.RedisAddr)
	if err != nil {
		t.Fatalf("redis.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	unique := time.Now().UnixNano()
	eventType := fmt.Sprintf("test.duplicate.%d", unique)
	group := fmt.Sprintf("worker-test-%d", unique)
	streamPrefix := fmt.Sprintf("test:stream:%d:", unique)
	channelPrefix := fmt.Sprintf("test:pubsub:%d:", unique)
	streamKey := streamPrefix + eventType
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM excel_bus_consumer_events WHERE event_id = ?`, fmt.Sprintf("evt_%d", unique))
		_ = client.Del(context.Background(), streamKey).Err()
	})

	broker := eventbus.NewRedis(
		workerredis.NewEventBridge(client),
		eventbus.RedisConfig{
			StreamPrefix:   streamPrefix,
			ChannelPrefix:  channelPrefix,
			PendingMinIdle: 100 * time.Millisecond,
		},
	)
	consumer := bus.NewConsumer(db, broker, bus.ConsumerConfig{})
	var handled atomic.Int32
	if err := consumer.Register(eventbus.SubscribeOptions{
		Topic:          eventType,
		Mode:           eventbus.DeliveryModeReliable,
		Group:          group,
		Consumer:       "worker-test",
		Count:          1,
		Block:          100 * time.Millisecond,
		PendingMinIdle: 100 * time.Millisecond,
	}, func(context.Context, eventbus.Event) error {
		handled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		_ = consumer.Stop(stopCtx)
	})

	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    fmt.Sprintf("evt_%d", unique),
			Type:       eventType,
			Version:    1,
			BaseID:     "base_duplicate",
			Seq:        7,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "excel_core_service.comment",
		},
		Payload: json.RawMessage(`{"comment_id":"cmt_duplicate"}`),
	}
	if err := broker.Publish(ctx, event); err != nil {
		t.Fatalf("first Publish() error = %v", err)
	}
	if err := broker.Publish(ctx, event); err != nil {
		t.Fatalf("duplicate Publish() error = %v", err)
	}

	waitForStreamDrain(t, ctx, client, streamKey, group)
	if got := handled.Load(); got != 1 {
		t.Fatalf("handler invocation count = %d, want 1", got)
	}
}

func openWorkerDB(t *testing.T) *sql.DB {
	t.Helper()
	config, err := base.LoadConfig(filepath.Join("..", "configs", "worker_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	db, err := base.OpenDB(config.MySQLDSN)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("MigrateUp() error = %v", err)
	}
	return db
}

func waitForStreamDrain(
	t *testing.T,
	ctx context.Context,
	client *goredis.Client,
	stream string,
	group string,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var diagnostic string
	for time.Now().Before(deadline) {
		last, lastErr := client.XRevRangeN(ctx, stream, "+", "-", 1).Result()
		groups, err := client.XInfoGroups(ctx, stream).Result()
		diagnostic = fmt.Sprintf("last=%v last_err=%v groups=%+v groups_err=%v", last, lastErr, groups, err)
		if lastErr == nil && len(last) == 1 && err == nil {
			for _, info := range groups {
				if info.Name == group && info.LastDeliveredID == last[0].ID && info.Pending == 0 {
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for stream drain: %v", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("stream %s group %s did not drain: %s", stream, group, diagnostic)
}
