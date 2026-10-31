package base

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigReadsWorkerServiceConfig(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "configs", "worker_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.GRPCAddr != "127.0.0.1:9092" {
		t.Errorf("GRPCAddr = %q, want %q", config.GRPCAddr, "127.0.0.1:9092")
	}
	if config.MySQLDSN != "excel:excel123@tcp(127.0.0.1:3306)/excel_worker?charset=utf8mb4&parseTime=true" {
		t.Errorf("MySQLDSN = %q", config.MySQLDSN)
	}
	if config.RedisAddr != "127.0.0.1:6379" {
		t.Errorf("RedisAddr = %q, want %q", config.RedisAddr, "127.0.0.1:6379")
	}
	if config.AttachDir != "../data/attachments" {
		t.Errorf("AttachDir = %q", config.AttachDir)
	}
	if config.AttachPoolLimitBytes != 5368709120 {
		t.Errorf("AttachPoolLimitBytes = %d", config.AttachPoolLimitBytes)
	}
}

func TestLoadConfigRejectsMissingRequiredField(t *testing.T) {
	path := writeWorkerConfig(t, `
grpc_addr: "127.0.0.1:9092"
mysql_dsn: "excel:excel123@tcp(127.0.0.1:3306)/excel_worker?charset=utf8mb4&parseTime=true"
attach_dir: "../data/attachments"
attach_pool_limit_bytes: 5368709120
`)

	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "redis_addr") {
		t.Fatalf("LoadConfig() error = %v, want missing redis_addr", err)
	}
}

func TestLoadConfigRejectsMalformedYAML(t *testing.T) {
	path := writeWorkerConfig(t, "grpc_addr: [\n")

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want malformed YAML error")
	}
}

func writeWorkerConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "worker_service_config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
