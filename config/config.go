package config

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/spf13/viper"
)

// AppConfig represents the application configuration
type AppConfig struct {
	Postgres DBConfig
	// Boundaries []Boundary `mapstructure:"boundaries"`
	Boundaries string
	boundaries []Boundary
	Grpc       struct {
		Port             string
		EnableReflection bool
		ConnectionTimeout time.Duration
		KeepAliveTime     time.Duration
		KeepAliveTimeout  time.Duration
		MaxConcurrentStreams uint32
	}
	PollingPublisher struct {
		BatchSize uint32
	}
	Logging struct {
		Enabled bool
		Level   string // e.g., "debug", "info", "warn", "error"
	}
	Nats struct {
		ServerName     string
		Port           int
		MaxPayload     int32
		MaxConnections int
		StoreDir       string
		Cluster        NatsClusterConfig
	}
	// Prod bool
	Auth struct {
		AdminUsername string
		AdminPassword string
	}
	Admin struct {
		Port     string
		Boundary string
	}
}

type DBConfig struct {
	User     string
	Name     string
	Password string
	Host     string
	Port     string
	Schemas  string
	SSLMode  string
	// Write pool configuration (optimized for write operations)
	WriteMaxOpenConns  int
	WriteMaxIdleConns  int
	WriteConnMaxIdleTime time.Duration
	WriteConnMaxLifetime time.Duration
	// Read pool configuration (optimized for read operations)
	ReadMaxOpenConns  int
	ReadMaxIdleConns  int
	ReadConnMaxIdleTime time.Duration
	ReadConnMaxLifetime time.Duration
	// Admin pool configuration (optimized for admin operations)
	AdminMaxOpenConns    int
	AdminMaxIdleConns    int
	AdminConnMaxIdleTime time.Duration
	AdminConnMaxLifetime time.Duration
}

type BoundaryToPostgresSchemaMapping struct {
	Schema   string
	Boundary string
}

type Boundary struct {
	Name        string
	Description string
}

func (p *DBConfig) GetSchemaMapping() map[string]BoundaryToPostgresSchemaMapping {
	var schmaMaps = strings.Split(p.Schemas, ",")
	var mappings = make(map[string]BoundaryToPostgresSchemaMapping, len(schmaMaps))

	for _, schema := range schmaMaps {
		var mapped = strings.Split(schema, ":")
		if len(mapped) != 2 {
			panic("Invalid schema mapping " + schema)
		}
		mappings[mapped[0]] = BoundaryToPostgresSchemaMapping{
			Boundary: strings.TrimSpace(mapped[0]),
			Schema:   strings.TrimSpace(mapped[1]),
		}
	}
	return mappings
}

type NatsClusterConfig struct {
	Name     string
	Host     string
	Port     int
	Routes   string
	Username string
	Password string
	Enabled  bool
	Timeout  time.Duration
}

func (c *NatsClusterConfig) GetRoutes() []string {
	return strings.Split(c.Routes, ",")
}

//go:embed config.yaml
var configData []byte

func LoadConfig() (AppConfig, error) {
	viper.SetConfigType("yaml")

	if err := viper.ReadConfig(bytes.NewReader(configData)); err != nil {
		return AppConfig{}, fmt.Errorf("failed to read config data: %w", err)
	}

	// Correct environment variable substitution
	for _, key := range viper.AllKeys() {
		value := viper.Get(key)
		if s, ok := value.(string); ok {
			substituted := substituteEnvVars(s)
			viper.Set(key, substituted)
		}
	}

	var config AppConfig

	if err := viper.Unmarshal(&config); err != nil {
		return AppConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := config.ParseBoundaries(); err != nil {
		return AppConfig{}, fmt.Errorf("failed to parse boundaries: %w", err)
	}
	fmt.Printf("boundaries are %+v\n", config.boundaries)

	err := validateConfig(config)
	if err != nil {
		return AppConfig{}, err
	}
	return config, nil
}

func (c *AppConfig) ParseBoundaries() error {
	if c.Boundaries == "" {
		return fmt.Errorf("No boudaries defined") // No boundaries defined
	}

	fmt.Printf("boundaries are %s\n", c.Boundaries)
	return json.Unmarshal([]byte(c.Boundaries), &c.boundaries)
}

func (c *AppConfig) GetBoundaries() *[]Boundary {
	return &c.boundaries
}

func validateConfig(config AppConfig) error {
	isAdminBoundaryDefined := false
	for _, boundary := range config.boundaries {
		if boundary.Name == config.Admin.Boundary {
			isAdminBoundaryDefined = true
		}
	}
	if !isAdminBoundaryDefined {
		return fmt.Errorf("admin boundary not defined")

	}
	return nil
}

func substituteEnvVars(value string) string {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		// Extract content between ${ and }
		content := value[2 : len(value)-1]

		// Find the position of the first colon that's not within backticks
		var colonPos int = -1
		inBackticks := false
		for i, char := range content {
			if char == '`' {
				inBackticks = !inBackticks
			} else if char == ':' && !inBackticks {
				colonPos = i
				break
			}
		}

		content = strings.ReplaceAll(content, "`", "")
		envVar := content
		defaultValue := ""
		if colonPos != -1 {
			envVar = content[:colonPos]
			defaultValue = content[colonPos+1:]
		}

		if envValue := os.Getenv(envVar); envValue != "" {
			return envValue
		}
		return defaultValue
	}
	return value
}
