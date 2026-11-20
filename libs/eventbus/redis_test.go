package eventbus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"excel-libs/eventbus"
)

func TestReliableSubscriptionRecoversMissingGroupDuringPendingRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	event := testEvent("evt-recover-list-pending", "test.recover")
	record, err := recordForEvent(event)
	if err != nil {
		t.Fatalf("recordForEvent() error = %v", err)
	}
	bridge := newFakeRedisBridge(record)
	bus := eventbus.NewRedis(bridge, eventbus.RedisConfig{
		StreamPrefix:  "test:events:",
		ChannelPrefix: "test:live:",
	})
	subscription, err := bus.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic:    event.Header.Type,
		Mode:     eventbus.DeliveryModeReliable,
		Group:    "test-group",
		Consumer: "test-consumer",
		Block:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer subscription.Close()

	bridge.dropGroup()
	delivery, err := subscription.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive() after consumer group deletion error = %v", err)
	}
	if delivery.Event.Header.EventID != event.Header.EventID {
		t.Fatalf("delivered event_id = %q, want %q", delivery.Event.Header.EventID, event.Header.EventID)
	}
	if !bridge.groupExists() {
		t.Fatal("consumer group was not recreated through EnsureGroup")
	}
}

func TestReliableSubscriptionRateLimitsRecoveryFailures(t *testing.T) {
	originalLogger := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	event := testEvent("evt-rate-limit", "test.rate-limit")
	bridge := newFakeRedisBridge()
	bus := eventbus.NewRedis(bridge, eventbus.RedisConfig{
		StreamPrefix:  "test:events:",
		ChannelPrefix: "test:live:",
	})
	subscription, err := bus.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic:    event.Header.Type,
		Mode:     eventbus.DeliveryModeReliable,
		Group:    "test-rate-limit-group",
		Consumer: "test-rate-limit-consumer",
		Block:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer subscription.Close()

	bridge.setEnsureError(errors.New("redis unavailable"))
	bridge.dropGroup()
	bridge.resetEnsureCalls()
	firstAttemptAt := time.Now()
	if _, err := subscription.Receive(ctx); err == nil {
		t.Fatal("Receive() error = nil, want observable recovery failure")
	}
	if calls := bridge.ensureCallCount(); calls != 1 {
		t.Fatalf("EnsureGroup attempts after first failure = %d, want 1", calls)
	}

	retryDeadline := time.Now().Add(4 * time.Second)
	for bridge.ensureCallCount() < 2 {
		if time.Now().After(retryDeadline) {
			t.Fatal("EnsureGroup was not retried after the recovery backoff")
		}
		if _, err := subscription.Receive(ctx); err == nil {
			t.Fatal("Receive() error = nil, want observable recovery failure")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if retryInterval := time.Since(firstAttemptAt); retryInterval < 750*time.Millisecond {
		t.Fatalf("EnsureGroup retry interval = %s, want rate-limited delay", retryInterval)
	}
	if calls := bridge.ensureCallCount(); calls > 3 {
		t.Fatalf("EnsureGroup attempts before second scheduled retry = %d, want no busy loop", calls)
	}
	for _, want := range []string{
		"test-rate-limit-group",
		"test:events:test.rate-limit",
		"test-rate-limit-consumer",
		"redis unavailable",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("recovery failure log = %q, want field value %q", logs.String(), want)
		}
	}
}

func TestReliableSubscriptionRecoversMissingGroupDuringClaim(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	event := testEvent("evt-recover-claim", "test.recover-claim")
	record, err := recordForEvent(event)
	if err != nil {
		t.Fatalf("recordForEvent() error = %v", err)
	}
	bridge := newFakeRedisBridge(record)
	bridge.pending = []eventbus.RedisPending{{ID: record.ID}}
	bridge.dropGroupAfterListPending = true
	bus := eventbus.NewRedis(bridge, eventbus.RedisConfig{
		StreamPrefix:  "test:events:",
		ChannelPrefix: "test:live:",
	})
	subscription, err := bus.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic:    event.Header.Type,
		Mode:     eventbus.DeliveryModeReliable,
		Group:    "test-claim-group",
		Consumer: "test-claim-consumer",
		Block:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer subscription.Close()

	delivery, err := subscription.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive() after group deletion before claim error = %v", err)
	}
	if delivery.Event.Header.EventID != event.Header.EventID {
		t.Fatalf("delivered event_id = %q, want %q", delivery.Event.Header.EventID, event.Header.EventID)
	}
	if !bridge.groupExists() {
		t.Fatal("consumer group was not recreated after claim observed NOGROUP")
	}
}

func TestReliableSubscriptionRecoversMissingGroupDuringReadGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	event := testEvent("evt-recover-read", "test.recover-read")
	record, err := recordForEvent(event)
	if err != nil {
		t.Fatalf("recordForEvent() error = %v", err)
	}
	bridge := newFakeRedisBridge(record)
	bridge.readGroupErr = noGroupError("test:events:"+event.Header.Type, "test-read-group")
	bus := eventbus.NewRedis(bridge, eventbus.RedisConfig{
		StreamPrefix:  "test:events:",
		ChannelPrefix: "test:live:",
	})
	subscription, err := bus.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic:    event.Header.Type,
		Mode:     eventbus.DeliveryModeReliable,
		Group:    "test-read-group",
		Consumer: "test-read-consumer",
		Block:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer subscription.Close()

	delivery, err := subscription.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive() after group deletion before XREADGROUP error = %v", err)
	}
	if delivery.Event.Header.EventID != event.Header.EventID {
		t.Fatalf("delivered event_id = %q, want %q", delivery.Event.Header.EventID, event.Header.EventID)
	}
	if !bridge.groupExists() {
		t.Fatal("consumer group was not recreated after read observed NOGROUP")
	}
}

type fakeRedisBridge struct {
	mu sync.Mutex

	stream                    string
	group                     string
	groupPresent              bool
	ensureCalls               int
	ensureErr                 error
	pending                   []eventbus.RedisPending
	dropGroupAfterListPending bool
	records                   []eventbus.RedisRecord
	readGroupErr              error
	listPendingErr            error
	claimErr                  error
}

func newFakeRedisBridge(records ...eventbus.RedisRecord) *fakeRedisBridge {
	return &fakeRedisBridge{records: records}
}

func (b *fakeRedisBridge) XAdd(context.Context, string, []byte) (string, error) {
	return "", nil
}

func (b *fakeRedisBridge) Publish(context.Context, string, []byte) error {
	return nil
}

func (b *fakeRedisBridge) EnsureGroup(_ context.Context, stream, group string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ensureCalls++
	if b.ensureErr != nil {
		return b.ensureErr
	}
	b.stream = stream
	b.group = group
	b.groupPresent = true
	return nil
}

func (b *fakeRedisBridge) ReadGroup(
	_ context.Context,
	stream, group, _ string,
	count int64,
	_ time.Duration,
) ([]eventbus.RedisRecord, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.readGroupErr != nil {
		err := b.readGroupErr
		b.readGroupErr = nil
		return nil, err
	}
	if !b.groupPresent {
		return nil, noGroupError(stream, group)
	}
	if len(b.records) == 0 {
		return nil, nil
	}
	if count <= 0 || count > int64(len(b.records)) {
		count = int64(len(b.records))
	}
	records := append([]eventbus.RedisRecord(nil), b.records[:count]...)
	b.records = b.records[count:]
	return records, nil
}

func (b *fakeRedisBridge) ListPending(
	_ context.Context,
	stream, group string,
	_ time.Duration,
	_ int64,
) ([]eventbus.RedisPending, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listPendingErr != nil {
		err := b.listPendingErr
		b.listPendingErr = nil
		return nil, err
	}
	if !b.groupPresent {
		return nil, noGroupError(stream, group)
	}
	pending := append([]eventbus.RedisPending(nil), b.pending...)
	b.pending = nil
	if b.dropGroupAfterListPending {
		b.dropGroupAfterListPending = false
		b.groupPresent = false
	}
	return pending, nil
}

func (b *fakeRedisBridge) Claim(
	_ context.Context,
	stream, group, _ string,
	_ time.Duration,
	_ []string,
) ([]eventbus.RedisRecord, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.claimErr != nil {
		err := b.claimErr
		b.claimErr = nil
		return nil, err
	}
	if !b.groupPresent {
		return nil, noGroupError(stream, group)
	}
	return nil, nil
}

func (b *fakeRedisBridge) XAck(context.Context, string, string, ...string) error {
	return nil
}

func (b *fakeRedisBridge) PubSub(context.Context, string) (eventbus.RedisPubSubSource, error) {
	return nil, errors.New("fake redis bridge has no pubsub source")
}

func (b *fakeRedisBridge) dropGroup() {
	b.mu.Lock()
	b.groupPresent = false
	b.mu.Unlock()
}

func (b *fakeRedisBridge) groupExists() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.groupPresent
}

func (b *fakeRedisBridge) setEnsureError(err error) {
	b.mu.Lock()
	b.ensureErr = err
	b.mu.Unlock()
}

func (b *fakeRedisBridge) resetEnsureCalls() {
	b.mu.Lock()
	b.ensureCalls = 0
	b.mu.Unlock()
}

func (b *fakeRedisBridge) ensureCallCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ensureCalls
}

func noGroupError(stream, group string) error {
	return fmt.Errorf("NOGROUP No such consumer group %q for stream %q", group, stream)
}

func testEvent(eventID, eventType string) eventbus.Event {
	return eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       eventType,
			Version:    1,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "excel-libs.eventbus.test",
		},
		Payload: json.RawMessage(`{"ok":true}`),
	}
}

func recordForEvent(event eventbus.Event) (eventbus.RedisRecord, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return eventbus.RedisRecord{}, err
	}
	return eventbus.RedisRecord{ID: "1-0", Payload: payload}, nil
}
