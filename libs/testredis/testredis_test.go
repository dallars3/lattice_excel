package testredis_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"excel-libs/testredis"
	goredis "github.com/redis/go-redis/v9"
)

func TestResolveDefaultsToDedicatedTestLogicalDB(t *testing.T) {
	t.Setenv(testredis.TestRedisAddrEnv, "")
	t.Setenv(testredis.TestRedisDBEnv, "")

	target, err := testredis.Resolve("127.0.0.1:6379")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if target.Addr != "127.0.0.1:6379" {
		t.Fatalf("Resolve().Addr = %q, want live address reused with isolated DB", target.Addr)
	}
	if target.DB != testredis.DefaultTestDB {
		t.Fatalf("Resolve().DB = %d, want %d", target.DB, testredis.DefaultTestDB)
	}
	if target.DB == 0 {
		t.Fatal("Resolve().DB = 0, want dedicated non-live logical database")
	}
}

func TestResolveRejectsLiveStackRedisTarget(t *testing.T) {
	t.Setenv(testredis.TestRedisAddrEnv, "127.0.0.1:6379")
	t.Setenv(testredis.TestRedisDBEnv, "0")

	_, err := testredis.Resolve("127.0.0.1:6379")
	if err == nil {
		t.Fatal("Resolve() = nil error, want hard failure for live-stack Redis target")
	}
	for _, want := range []string{
		"测试 Redis 未隔离",
		testredis.TestRedisDBEnv,
		testredis.TestRedisAddrEnv,
		"redis_addr",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Resolve() error = %v, want %q in isolation guidance", err, want)
		}
	}
}

func TestFlushTestDBOnlyClearsIsolatedLogicalDB(t *testing.T) {
	if testing.Short() {
		t.Skip("需要本机 Redis（127.0.0.1:6379）")
	}
	t.Setenv(testredis.TestRedisAddrEnv, "")
	t.Setenv(testredis.TestRedisDBEnv, "")

	const liveAddr = "127.0.0.1:6379"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// The live DB 0 sentinel is a read-only safety witness for this guard test.
	liveClient := goredis.NewClient(&goredis.Options{Addr: liveAddr, DB: testredis.LiveStackDB})
	if err := liveClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping live Redis: %v", err)
	}
	suffix := fmt.Sprintf("testredis-guard-%d", time.Now().UnixNano())
	liveKey := suffix + ":live"
	testKey := suffix + ":test"
	t.Cleanup(func() {
		_ = liveClient.Del(context.Background(), liveKey).Err()
		_ = liveClient.Close()
	})
	if err := liveClient.Set(ctx, liveKey, "keep", 0).Err(); err != nil {
		t.Fatalf("seed live DB sentinel: %v", err)
	}

	client, err := testredis.Open(ctx, liveAddr)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if client.Options().DB != testredis.DefaultTestDB {
		t.Fatalf("test Redis DB = %d, want %d", client.Options().DB, testredis.DefaultTestDB)
	}
	if err := client.Set(ctx, testKey, "clear-me", 0).Err(); err != nil {
		t.Fatalf("seed isolated DB key: %v", err)
	}

	if err := testredis.FlushTestDB(ctx, client, liveAddr, "guard test"); err != nil {
		t.Fatalf("FlushTestDB() error = %v", err)
	}
	if exists, err := client.Exists(ctx, testKey).Result(); err != nil || exists != 0 {
		t.Fatalf("isolated DB key exists = %d, err = %v; want cleared", exists, err)
	}
	if exists, err := liveClient.Exists(ctx, liveKey).Result(); err != nil || exists != 1 {
		t.Fatalf("live DB sentinel exists = %d, err = %v; want preserved", exists, err)
	}
}
