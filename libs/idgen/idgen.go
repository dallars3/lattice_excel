// Package idgen 生成业务资源 ID：ULID 字符串（时间有序 + 随机后缀），
// 用于用户/企业/Base 等业务主键；history/outbox/seq 等内部追加表不使用本包。
package idgen

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// New 返回一个新的 ULID 字符串（26 字符 Crockford Base32）。
// 单调性由 seq 保证：同一毫秒内生成多次也不会重复。
var entropy = ulid.Monotonic(rand.Reader, 0)

// New 生成新 ULID。
func New() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), entropy)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
