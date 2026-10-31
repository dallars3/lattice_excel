package deal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigReadsWebAPIConfig(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "configs", "web_api_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.Addr != ":8080" {
		t.Errorf("Addr = %q, want %q", config.Addr, ":8080")
	}
	if config.RedisAddr != "127.0.0.1:6379" {
		t.Errorf("RedisAddr = %q, want %q", config.RedisAddr, "127.0.0.1:6379")
	}
	if config.CoreGRPCAddr != "127.0.0.1:9091" {
		t.Errorf("CoreGRPCAddr = %q", config.CoreGRPCAddr)
	}
	if config.WorkerGRPCAddr != "127.0.0.1:9092" {
		t.Errorf("WorkerGRPCAddr = %q", config.WorkerGRPCAddr)
	}
	if config.SessionCacheTTLSeconds != 55 {
		t.Errorf("SessionCacheTTLSeconds = %d", config.SessionCacheTTLSeconds)
	}
}

func TestLoadConfigRejectsMissingRequiredField(t *testing.T) {
	path := writeWebAPIConfig(t, `
addr: ":8080"
core_grpc_addr: "127.0.0.1:9091"
worker_grpc_addr: "127.0.0.1:9092"
session_cache_ttl_seconds: 55
`)

	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "redis_addr") {
		t.Fatalf("LoadConfig() error = %v, want missing redis_addr", err)
	}
}

func TestLoadConfigRejectsMalformedYAML(t *testing.T) {
	path := writeWebAPIConfig(t, "addr: [\n")

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want malformed YAML error")
	}
}

func writeWebAPIConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "web_api_config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
