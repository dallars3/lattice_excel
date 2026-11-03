package idgen

import (
	"regexp"
	"sync"
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

func TestNewIsSafeForConcurrentGoroutines(t *testing.T) {
	const workers = 64
	const rounds = 20
	seen := make(map[string]bool, workers*rounds)
	var once sync.Once
	guard := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-guard
			for range rounds {
				id, err := New()
				if err != nil {
					t.Errorf("New() error = %v", err)
					return
				}
				mu.Lock()
				if seen[id] {
					once.Do(func() { t.Errorf("New() 并发下重复：%q", id) })
				}
				seen[id] = true
				mu.Unlock()
			}
		}()
	}
	close(guard)
	wg.Wait()
}
