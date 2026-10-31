package deal

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config contains the infrastructure settings required to start excel_web_api.
type Config struct {
	Addr                   string `yaml:"addr"`
	RedisAddr              string `yaml:"redis_addr"`
	CoreGRPCAddr           string `yaml:"core_grpc_addr"`
	WorkerGRPCAddr         string `yaml:"worker_grpc_addr"`
	SessionCacheTTLSeconds int    `yaml:"session_cache_ttl_seconds"`
}

// LoadConfig reads and validates web API configuration.
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
		"addr":             config.Addr,
		"redis_addr":       config.RedisAddr,
		"core_grpc_addr":   config.CoreGRPCAddr,
		"worker_grpc_addr": config.WorkerGRPCAddr,
	}
	for name, value := range required {
		if value == "" {
			return Config{}, fmt.Errorf("config %q: missing required field %s", path, name)
		}
	}
	if config.SessionCacheTTLSeconds <= 0 || config.SessionCacheTTLSeconds >= 60 {
		return Config{}, fmt.Errorf("config %q: session_cache_ttl_seconds must be between 1 and 59", path)
	}
	return config, nil
}
