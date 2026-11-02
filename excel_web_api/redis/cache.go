// redis/cache.go：会话/鉴权短缓存。
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	sharev1 "excel/api/share/v1"
)

const (
	// CacheTTL keeps both session and authorization snapshots below the
	// one-minute revocation window.
	CacheTTL = 55 * time.Second

	sessionCachePrefix = "excel:session:"
	accessCachePrefix  = "excel:ac:"
	accessIndexPrefix  = "excel:ac:index:"
)

// Cache stores short-lived session and Base authorization decisions.
type Cache struct {
	client *goredis.Client
	logger *slog.Logger

	sessionFlights flightGroup[*commonv1.Principal]
	accessFlights  flightGroup[*sharev1.CheckAccessResponse]
}

// NewCache creates the gateway cache. A nil logger uses slog.Default().
func NewCache(client *goredis.Client, loggers ...*slog.Logger) *Cache {
	logger := slog.Default()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	return &Cache{client: client, logger: logger}
}

// Session returns a cached Principal or loads it once for concurrent callers.
// Redis failures degrade to the loader instead of failing the HTTP request.
func (cache *Cache) Session(
	ctx context.Context,
	token string,
	load func(context.Context) (*commonv1.Principal, error),
) (*commonv1.Principal, error) {
	if cache == nil || cache.client == nil {
		return load(ctx)
	}

	key := sessionCachePrefix + token
	payload, err := cache.client.Get(ctx, key).Bytes()
	switch {
	case err == nil:
		var principal commonv1.Principal
		if unmarshalErr := proto.Unmarshal(payload, &principal); unmarshalErr == nil &&
			principal.GetUserId() != "" {
			return &principal, nil
		}
		cache.warn("invalid session cache entry; using ExchangeSession", err)
	case errors.Is(err, goredis.Nil):
	default:
		cache.warn("session cache unavailable; using ExchangeSession", err)
	}

	return cache.sessionFlights.Do(ctx, key, func() (*commonv1.Principal, error) {
		principal, loadErr := load(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		if principal == nil || principal.GetUserId() == "" {
			return nil, errors.New("session loader returned an empty principal")
		}
		encoded, marshalErr := proto.Marshal(principal)
		if marshalErr != nil {
			cache.warn("marshal session cache entry failed", marshalErr)
			return principal, nil
		}
		if setErr := cache.client.Set(ctx, key, encoded, CacheTTL).Err(); setErr != nil {
			cache.warn("write session cache failed; request continues", setErr)
		}
		return principal, nil
	})
}

// DeleteSession removes one cached opaque ticket immediately.
func (cache *Cache) DeleteSession(ctx context.Context, token string) error {
	if cache == nil || cache.client == nil {
		return nil
	}
	if err := cache.client.Del(ctx, sessionCachePrefix+token).Err(); err != nil {
		cache.warn("delete session cache failed; TTL fallback remains", err)
		return err
	}
	return nil
}

// Access returns a cached CheckAccess result or loads it once per key.
// Redis failures degrade to core.share.CheckAccess rather than allowing blindly.
func (cache *Cache) Access(
	ctx context.Context,
	userID string,
	baseID string,
	action sharev1.Action,
	load func(context.Context) (*sharev1.CheckAccessResponse, error),
) (*sharev1.CheckAccessResponse, error) {
	if cache == nil || cache.client == nil {
		return load(ctx)
	}

	key := accessCacheKey(userID, baseID, action)
	payload, err := cache.client.Get(ctx, key).Bytes()
	switch {
	case err == nil:
		var cached accessCacheValue
		if unmarshalErr := json.Unmarshal(payload, &cached); unmarshalErr == nil {
			return cached.response(), nil
		}
		cache.warn("invalid authorization cache entry; using CheckAccess", err)
	case errors.Is(err, goredis.Nil):
	default:
		cache.warn("authorization cache unavailable; using CheckAccess", err)
	}

	return cache.accessFlights.Do(ctx, key, func() (*sharev1.CheckAccessResponse, error) {
		decision, loadErr := load(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		if decision == nil {
			return nil, errors.New("authorization loader returned an empty decision")
		}
		encoded, marshalErr := json.Marshal(accessCacheValueFrom(decision))
		if marshalErr != nil {
			cache.warn("marshal authorization cache entry failed", marshalErr)
			return decision, nil
		}
		pipe := cache.client.TxPipeline()
		pipe.Set(ctx, key, encoded, CacheTTL)
		indexKey := accessIndexPrefix + baseID
		pipe.SAdd(ctx, indexKey, key)
		pipe.Expire(ctx, indexKey, CacheTTL)
		if _, execErr := pipe.Exec(ctx); execErr != nil {
			cache.warn("write authorization cache failed; request continues", execErr)
		}
		return decision, nil
	})
}

// InvalidateAccessByBase deletes every cached action decision for one Base.
func (cache *Cache) InvalidateAccessByBase(ctx context.Context, baseID string) (int, error) {
	if cache == nil || cache.client == nil {
		return 0, nil
	}

	keys := make(map[string]struct{})
	indexKey := accessIndexPrefix + baseID
	indexed, err := cache.client.SMembers(ctx, indexKey).Result()
	if err != nil && !errors.Is(err, goredis.Nil) {
		cache.warn("read authorization cache index failed; scanning keys", err)
	}
	for _, key := range indexed {
		keys[key] = struct{}{}
	}

	pattern := accessCachePrefix + "*:" + escapeRedisGlob(baseID) + ":*"
	var cursor uint64
	for {
		found, nextCursor, scanErr := cache.client.Scan(ctx, cursor, pattern, 100).Result()
		if scanErr != nil {
			return 0, fmt.Errorf("scan authorization cache for base %s: %w", baseID, scanErr)
		}
		for _, key := range found {
			keys[key] = struct{}{}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if len(keys) > 0 {
		values := make([]string, 0, len(keys))
		for key := range keys {
			values = append(values, key)
		}
		for start := 0; start < len(values); start += 256 {
			end := start + 256
			if end > len(values) {
				end = len(values)
			}
			if err := cache.client.Del(ctx, values[start:end]...).Err(); err != nil {
				return 0, fmt.Errorf("delete authorization cache for base %s: %w", baseID, err)
			}
		}
	}
	if err := cache.client.Del(ctx, indexKey).Err(); err != nil {
		return len(keys), fmt.Errorf("delete authorization cache index for base %s: %w", baseID, err)
	}
	return len(keys), nil
}

func accessCacheKey(userID, baseID string, action sharev1.Action) string {
	return accessCachePrefix + userID + ":" + baseID + ":" + action.String()
}

type accessCacheValue struct {
	Allow    bool             `json:"allow"`
	Role     sharev1.Role     `json:"role"`
	Internal bool             `json:"internal"`
	RowScope sharev1.RowScope `json:"row_scope"`
}

func accessCacheValueFrom(response *sharev1.CheckAccessResponse) accessCacheValue {
	return accessCacheValue{
		Allow:    response.GetAllow(),
		Role:     response.GetRole(),
		Internal: response.GetInternal(),
		RowScope: response.GetRowScope(),
	}
}

func (value accessCacheValue) response() *sharev1.CheckAccessResponse {
	return &sharev1.CheckAccessResponse{
		Allow:    value.Allow,
		Role:     value.Role,
		Internal: value.Internal,
		RowScope: value.RowScope,
	}
}

func (cache *Cache) warn(message string, err error) {
	if cache == nil || cache.logger == nil {
		return
	}
	cache.logger.Warn(message, "error", err)
}

func escapeRedisGlob(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`*`, `\*`,
		`?`, `\?`,
		`[`, `\[`,
		`]`, `\]`,
	)
	return replacer.Replace(value)
}

type flightCall[T any] struct {
	done  chan struct{}
	value T
	err   error
}

type flightGroup[T any] struct {
	mu    sync.Mutex
	calls map[string]*flightCall[T]
}

func (group *flightGroup[T]) Do(
	ctx context.Context,
	key string,
	load func() (T, error),
) (T, error) {
	group.mu.Lock()
	if group.calls == nil {
		group.calls = make(map[string]*flightCall[T])
	}
	if call, ok := group.calls[key]; ok {
		group.mu.Unlock()
		select {
		case <-call.done:
			return call.value, call.err
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
	call := &flightCall[T]{done: make(chan struct{})}
	group.calls[key] = call
	group.mu.Unlock()

	call.value, call.err = load()
	close(call.done)

	group.mu.Lock()
	delete(group.calls, key)
	group.mu.Unlock()
	return call.value, call.err
}
