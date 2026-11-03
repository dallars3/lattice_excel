package base

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config contains the infrastructure settings required to start excel_worker_service.
type Config struct {
	GRPCAddr             string `yaml:"grpc_addr"`
	CoreGRPCAddr         string `yaml:"core_grpc_addr"`
	MySQLDSN             string `yaml:"mysql_dsn"`
	RedisAddr            string `yaml:"redis_addr"`
	AttachDir            string `yaml:"attach_dir"`
	AttachPoolLimitBytes int64  `yaml:"attach_pool_limit_bytes"`
}

// LoadConfig reads and validates worker service configuration.
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
		"grpc_addr":  config.GRPCAddr,
		"core_grpc_addr": config.CoreGRPCAddr,
		"mysql_dsn":  config.MySQLDSN,
		"redis_addr": config.RedisAddr,
		"attach_dir": config.AttachDir,
	}
	for name, value := range required {
		if value == "" {
			return Config{}, fmt.Errorf("config %q: missing required field %s", path, name)
		}
	}
	if config.AttachPoolLimitBytes <= 0 {
		return Config{}, fmt.Errorf("config %q: missing required field attach_pool_limit_bytes", path)
	}
	return config, nil
}
