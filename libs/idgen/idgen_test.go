package idgen

import (
	"regexp"
	"testing"
)

func TestNewIsULIDStringAndUnique(t *testing.T) {
	re := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	seen := make(map[string]bool, 64)
	for i := 0; i < 64; i++ {
		id, err := New()
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		if !re.MatchString(id) {
			t.Fatalf("New() 返回非 ULID 字符串: %q", id)
		}
		if seen[id] {
			t.Fatalf("New() 在 64 次生成内重复：%q", id)
		}
		seen[id] = true
	}
}
