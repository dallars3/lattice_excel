package base

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config contains the infrastructure settings required to start excel_core_service.
type Config struct {
	GRPCAddr             string `yaml:"grpc_addr"`
	MySQLDSN             string `yaml:"mysql_dsn"`
	RedisAddr            string `yaml:"redis_addr"`
	PermissionConfigPath string `yaml:"permission_config_path"`
	AdminToken           string `yaml:"admin_token"`
}

// LoadConfig reads and validates core service configuration.
func LoadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer file.Close()

	var config Config
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", path, err)
	}

	required := map[string]string{
		"grpc_addr":              config.GRPCAddr,
		"mysql_dsn":              config.MySQLDSN,
		"redis_addr":             config.RedisAddr,
		"permission_config_path": config.PermissionConfigPath,
	}
	for name, value := range required {
		if value == "" {
			return Config{}, fmt.Errorf("config %q: missing required field %s", path, name)
		}
	}
	return config, nil
}
