package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// AppConfig represents the application configuration
type AppConfig struct {
	DB struct {
		User     string
		Name     string
		Password string
		Host     string
		Port     string
	}
	Grpc struct {
		Port             string
		EnableReflection bool
	}
	PollingPublisher struct {
		BatchSize int32
	}
	Logging struct {
		Enabled bool
		Level   string // e.g., "debug", "info", "warn", "error"
	}
	Nats struct {
		Port           int
		MaxPayload     int32
		MaxConnections int
		StoreDir       string
		Cluster        NatsClusterConfig
	}
}

type NatsClusterConfig struct {
	Name     string
	Host     string
	Port     int
	Routes   []string
	Username string
	Password string
	Enabled  bool
	Timeout  time.Duration
}

func LoadConfig() (*AppConfig, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("../config")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Custom environment variable substitution
	for _, key := range viper.AllKeys() {
		value := viper.GetString(key)
		viper.Set(key, substituteEnvVars(value))
	}

	var config AppConfig
	err := viper.Unmarshal(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

func substituteEnvVars(value string) string {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		parts := strings.SplitN(value[2:len(value)-1], ":", 2)
		envVar := parts[0]
		defaultValue := ""
		if len(parts) > 1 {
			defaultValue = parts[1]
		}

		if envValue := os.Getenv(envVar); envValue != "" {
			return envValue
		}
		return defaultValue
	}
	return value
}
