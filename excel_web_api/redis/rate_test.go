package redis_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	webredis "excel_web_api/redis"
)

func TestRateLimiterSlidingWindowRejectsExcessRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	limiter := webredis.NewRateLimiter(client)
	scope := fmt.Sprintf("contract_test_%d", time.Now().UnixNano())
	identity := "192.0.2.10"
	window := time.Second
	t.Cleanup(func() {
		_ = client.Del(context.Background(), webredis.RateLimitKey(scope, identity)).Err()
	})

	for attempt := 1; attempt <= 3; attempt++ {
		result, err := limiter.Allow(ctx, scope, identity, 2, window)
		if err != nil {
			t.Fatalf("Allow(attempt %d): %v", attempt, err)
		}
		wantAllowed := attempt <= 2
		if result.Allowed != wantAllowed {
			t.Fatalf(
				"Allow(attempt %d).Allowed = %t, want %t",
				attempt,
				result.Allowed,
				wantAllowed,
			)
		}
		if !result.Allowed && (result.RetryAfter <= 0 || result.RetryAfter > window) {
			t.Fatalf("retry after = %s, want 0 < duration <= %s", result.RetryAfter, window)
		}
	}

	otherScope, err := limiter.Allow(ctx, scope+"_other", identity, 1, window)
	if err != nil {
		t.Fatalf("Allow(other scope): %v", err)
	}
	if !otherScope.Allowed {
		t.Fatal("another rate-limit scope was incorrectly rejected")
	}
}
