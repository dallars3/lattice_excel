package redis_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	sharev1 "excel/api/share/v1"
	redisclient "excel_web_api/redis"
)

func TestAuthorizationCacheTTLAndLinkClosedInvalidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	unique := time.Now().UnixNano()
	userID := fmt.Sprintf("user_%d", unique)
	baseID := fmt.Sprintf("base_%d", unique)
	shareBaseID := fmt.Sprintf("base_share_%d", unique)
	action := sharev1.Action_ACTION_VIEW
	secondAction := sharev1.Action_ACTION_EXPORT
	key := fmt.Sprintf("excel:ac:%s:%s:%s", userID, baseID, action.String())
	secondKey := fmt.Sprintf("excel:ac:%s:%s:%s", userID, baseID, secondAction.String())
	shareKey := fmt.Sprintf("excel:ac:%s:%s:%s", userID, shareBaseID, action.String())
	streamPrefix := fmt.Sprintf("test:web-events:%d:", unique)
	t.Cleanup(func() {
		_ = client.Del(context.Background(), key).Err()
		_ = client.Del(context.Background(), secondKey).Err()
		_ = client.Del(context.Background(), shareKey).Err()
		_ = client.Del(
			context.Background(),
			"excel:ac:index:"+baseID,
			"excel:ac:index:"+shareBaseID,
		).Err()
		_ = client.Del(context.Background(), streamPrefix+"link.closed").Err()
		_ = client.Del(context.Background(), streamPrefix+"share.changed").Err()
	})

	cache := redisclient.NewCache(client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	loadCalls := 0
	decision, err := cache.Access(
		ctx,
		userID,
		baseID,
		action,
		func(context.Context) (*sharev1.CheckAccessResponse, error) {
			loadCalls++
			return &sharev1.CheckAccessResponse{Allow: true}, nil
		},
	)
	if err != nil {
		t.Fatalf("Access(): %v", err)
	}
	if !decision.GetAllow() || loadCalls != 1 {
		t.Fatalf("decision = %v load calls = %d, want allow and one load", decision, loadCalls)
	}
	if _, err := cache.Access(
		ctx,
		userID,
		baseID,
		secondAction,
		func(context.Context) (*sharev1.CheckAccessResponse, error) {
			return &sharev1.CheckAccessResponse{Allow: true}, nil
		},
	); err != nil {
		t.Fatalf("Access(second action): %v", err)
	}

	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("TTL(%s): %v", key, err)
	}
	if ttl <= 0 || ttl >= 60*time.Second {
		t.Fatalf("TTL(%s) = %s, want 0 < ttl < 60s", key, ttl)
	}
	cachedDecision, err := cache.Access(
		ctx,
		userID,
		baseID,
		action,
		func(context.Context) (*sharev1.CheckAccessResponse, error) {
			t.Fatal("cached authorization decision triggered a second load")
			return nil, nil
		},
	)
	if err != nil || !cachedDecision.GetAllow() {
		t.Fatalf("cached Access() = %v, %v", cachedDecision, err)
	}
	if _, err := cache.Access(
		ctx,
		userID,
		shareBaseID,
		action,
		func(context.Context) (*sharev1.CheckAccessResponse, error) {
			return &sharev1.CheckAccessResponse{Allow: true}, nil
		},
	); err != nil {
		t.Fatalf("Access(share.changed fixture): %v", err)
	}

	bus := eventbus.NewRedis(
		redisclient.NewEventBridge(client),
		eventbus.RedisConfig{
			StreamPrefix:  streamPrefix,
			ChannelPrefix: fmt.Sprintf("test:web-live:%d:", unique),
		},
	)
	invalidator := redisclient.NewAuthInvalidator(
		cache,
		bus,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err := invalidator.Start(ctx); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	t.Cleanup(func() { _ = invalidator.Close() })

	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    fmt.Sprintf("evt_%d", unique),
			Type:       "link.closed",
			Version:    1,
			BaseID:     baseID,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "excel_web_api.test",
		},
		Payload: json.RawMessage(`{"base_id":"` + baseID + `"}`),
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := bus.Publish(ctx, event); err != nil {
			t.Fatalf("Publish(link.closed): %v", err)
		}
		exists, err := client.Exists(ctx, key, secondKey).Result()
		if err != nil {
			t.Fatalf("Exists(%s): %v", key, err)
		}
		if exists == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache keys for %s still exist after link.closed", baseID)
		}
		time.Sleep(25 * time.Millisecond)
	}

	changedEvent := event
	changedEvent.Header.EventID = fmt.Sprintf("evt_changed_%d", unique)
	changedEvent.Header.Type = "share.changed"
	changedEvent.Header.BaseID = shareBaseID
	changedEvent.Payload = json.RawMessage(`{"base_id":"` + shareBaseID + `"}`)
	deadline = time.Now().Add(3 * time.Second)
	for {
		if err := bus.Publish(ctx, changedEvent); err != nil {
			t.Fatalf("Publish(share.changed): %v", err)
		}
		exists, err := client.Exists(ctx, shareKey).Result()
		if err != nil {
			t.Fatalf("Exists(%s): %v", shareKey, err)
		}
		if exists == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache key %s still exists after share.changed", shareKey)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestSessionCacheTTLAndHit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	token := fmt.Sprintf("session_%d", time.Now().UnixNano())
	key := "excel:session:" + token
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	cache := redisclient.NewCache(client)
	loadCalls := 0
	load := func(context.Context) (*commonv1.Principal, error) {
		loadCalls++
		return &commonv1.Principal{UserId: "user_1", Internal: true}, nil
	}
	for call := 0; call < 2; call++ {
		principal, err := cache.Session(ctx, token, load)
		if err != nil {
			t.Fatalf("Session() call %d: %v", call, err)
		}
		if principal.GetUserId() != "user_1" {
			t.Fatalf("Session() principal = %v", principal)
		}
	}
	if loadCalls != 1 {
		t.Fatalf("session loader calls = %d, want one", loadCalls)
	}
	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("TTL(%s): %v", key, err)
	}
	if ttl <= 0 || ttl >= 60*time.Second {
		t.Fatalf("TTL(%s) = %s, want 0 < ttl < 60s", key, ttl)
	}
}

func TestCacheSingleflightMergesConcurrentMisses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	unique := time.Now().UnixNano()
	token := fmt.Sprintf("flight_session_%d", unique)
	userID := fmt.Sprintf("flight_user_%d", unique)
	baseID := fmt.Sprintf("flight_base_%d", unique)
	action := sharev1.Action_ACTION_EDIT_ALL
	cache := redisclient.NewCache(client)
	t.Cleanup(func() {
		_ = client.Del(context.Background(), "excel:session:"+token).Err()
		_ = client.Del(
			context.Background(),
			fmt.Sprintf("excel:ac:%s:%s:%s", userID, baseID, action.String()),
		).Err()
		_ = client.Del(context.Background(), "excel:ac:index:"+baseID).Err()
	})

	const callers = 12
	var sessionLoads atomic.Int32
	var authorizationLoads atomic.Int32
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(callers * 2)

	for call := 0; call < callers; call++ {
		go func() {
			defer wait.Done()
			<-start
			principal, err := cache.Session(ctx, token, func(context.Context) (*commonv1.Principal, error) {
				sessionLoads.Add(1)
				time.Sleep(50 * time.Millisecond)
				return &commonv1.Principal{UserId: userID}, nil
			})
			if err != nil || principal.GetUserId() != userID {
				t.Errorf("Session() = %v, %v", principal, err)
			}
		}()
		go func() {
			defer wait.Done()
			<-start
			decision, err := cache.Access(
				ctx,
				userID,
				baseID,
				action,
				func(context.Context) (*sharev1.CheckAccessResponse, error) {
					authorizationLoads.Add(1)
					time.Sleep(50 * time.Millisecond)
					return &sharev1.CheckAccessResponse{Allow: true}, nil
				},
			)
			if err != nil || !decision.GetAllow() {
				t.Errorf("Access() = %v, %v", decision, err)
			}
		}()
	}
	close(start)
	wait.Wait()

	if got := sessionLoads.Load(); got != 1 {
		t.Fatalf("session loader calls = %d, want one", got)
	}
	if got := authorizationLoads.Load(); got != 1 {
		t.Fatalf("authorization loader calls = %d, want one", got)
	}
}
