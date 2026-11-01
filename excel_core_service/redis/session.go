package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
)

const (
	// SessionTTL is the public session lifetime required by the login contract.
	SessionTTL    = 30 * 24 * time.Hour
	LoginLockTTL  = 15 * time.Minute
	loginFailures = 5
	sessionKey    = "excel:iam:session:"
	loginCountKey = "excel:iam:login:failures:"
	loginLockKey  = "excel:iam:login:lock:"
)

// ErrSessionNotFound reports an absent or expired session token.
var ErrSessionNotFound = errors.New("session not found")

// SessionStore persists opaque session tickets and their Principal.
type SessionStore struct {
	client *goredis.Client
}

// NewSessionStore creates a Redis-backed IAM session store.
func NewSessionStore(client *goredis.Client) *SessionStore {
	return &SessionStore{client: client}
}

// Save writes a session Principal with the required TTL.
func (store *SessionStore) Save(ctx context.Context, token string, principal *commonv1.Principal) error {
	payload, err := proto.Marshal(principal)
	if err != nil {
		return fmt.Errorf("marshal session principal: %w", err)
	}
	if err := store.client.Set(ctx, sessionKey+token, payload, SessionTTL).Err(); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// Get returns the Principal bound to token.
func (store *SessionStore) Get(ctx context.Context, token string) (*commonv1.Principal, error) {
	payload, err := store.client.Get(ctx, sessionKey+token).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	var principal commonv1.Principal
	if err := proto.Unmarshal(payload, &principal); err != nil {
		return nil, fmt.Errorf("unmarshal session principal: %w", err)
	}
	return &principal, nil
}

// Delete invalidates one token without affecting other sessions.
func (store *SessionStore) Delete(ctx context.Context, token string) error {
	if err := store.client.Del(ctx, sessionKey+token).Err(); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// LoginLocked reports whether username+IP is inside its 15-minute lock window.
func (store *SessionStore) LoginLocked(ctx context.Context, username, ip string) (bool, error) {
	key := loginSubject(username, ip)
	locked, err := store.client.Exists(ctx, loginLockKey+key).Result()
	if err != nil {
		return false, fmt.Errorf("check login lock: %w", err)
	}
	return locked == 1, nil
}

// RecordLoginFailure increments the consecutive failure counter and applies the lock at five.
func (store *SessionStore) RecordLoginFailure(ctx context.Context, username, ip string) (bool, error) {
	key := loginSubject(username, ip)
	const script = `
		local count = redis.call('INCR', KEYS[1])
		if count == 1 then
			redis.call('EXPIRE', KEYS[1], ARGV[1])
		end
		if count >= tonumber(ARGV[2]) then
			redis.call('SET', KEYS[2], '1', 'EX', ARGV[1])
			redis.call('DEL', KEYS[1])
			return 1
		end
		return 0`
	result, err := store.client.Eval(
		ctx,
		script,
		[]string{loginCountKey + key, loginLockKey + key},
		int(LoginLockTTL/time.Second),
		loginFailures,
	).Int()
	if err != nil {
		return false, fmt.Errorf("record login failure: %w", err)
	}
	return result == 1, nil
}

// ClearLoginFailures resets the consecutive counter after a successful login.
func (store *SessionStore) ClearLoginFailures(ctx context.Context, username, ip string) error {
	key := loginSubject(username, ip)
	if err := store.client.Del(ctx, loginCountKey+key).Err(); err != nil {
		return fmt.Errorf("clear login failures: %w", err)
	}
	return nil
}

func loginSubject(username, ip string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(username) + "\x00" + ip))
	return hex.EncodeToString(sum[:])
}
