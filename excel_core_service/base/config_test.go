package base

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigReadsCoreServiceConfig(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "configs", "core_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.GRPCAddr != "127.0.0.1:9091" {
		t.Errorf("GRPCAddr = %q, want %q", config.GRPCAddr, "127.0.0.1:9091")
	}
	if config.MySQLDSN != "excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true" {
		t.Errorf("MySQLDSN = %q", config.MySQLDSN)
	}
	if config.RedisAddr != "127.0.0.1:6379" {
		t.Errorf("RedisAddr = %q, want %q", config.RedisAddr, "127.0.0.1:6379")
	}
	if config.PermissionConfigPath != "../excel_permission_config" {
		t.Errorf("PermissionConfigPath = %q", config.PermissionConfigPath)
	}
}

func TestLoadConfigRejectsMissingRequiredField(t *testing.T) {
	path := writeCoreConfig(t, `
grpc_addr: "127.0.0.1:9091"
mysql_dsn: "excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true"
permission_config_path: "../excel_permission_config"
`)

	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "redis_addr") {
		t.Fatalf("LoadConfig() error = %v, want missing redis_addr", err)
	}
}

func TestLoadConfigRejectsMalformedYAML(t *testing.T) {
	path := writeCoreConfig(t, "grpc_addr: [\n")

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want malformed YAML error")
	}
}

func writeCoreConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "core_service_config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
