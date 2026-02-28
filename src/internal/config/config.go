package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Auth     AuthConfig     `yaml:"auth"`
	Database DatabaseConfig `yaml:"database"`
	Logging  LoggingConfig  `yaml:"logging"`
	Upstream UpstreamConfig `yaml:"upstream"`
}

type ServerConfig struct {
	Host                string `yaml:"host"`
	Port                int    `yaml:"port"`
	DefaultTimeoutSec   int    `yaml:"default_timeout_sec"`
	UserAgent           string `yaml:"user_agent"`
	PublicBaseURL       string `yaml:"public_base_url"`
	EnableJSONResponses bool   `yaml:"enable_json_responses"`
	IngestOnStream      bool   `yaml:"ingest_on_stream"`
	IngestOnStar        bool   `yaml:"ingest_on_star"`
}

type AuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

type UpstreamConfig struct {
	InstancesURL      string `yaml:"instances_url"`
	InstancesFile     string `yaml:"instances_file"`
	ClientHeader      string `yaml:"client_header"`
	Source            string `yaml:"source"`
	StreamQuality     string `yaml:"stream_quality"`
	TimeoutSec        int    `yaml:"timeout_sec"`
	ProbeIntervalSec  int    `yaml:"probe_interval_sec"`
	AttemptTimeoutSec int    `yaml:"attempt_timeout_sec"`
	FallbackAttempts  int    `yaml:"fallback_attempts"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			Host:                "0.0.0.0",
			Port:                8080,
			DefaultTimeoutSec:   20,
			UserAgent:           "Dhwani/0.1",
			PublicBaseURL:       "",
			EnableJSONResponses: true,
			IngestOnStream:      false,
			IngestOnStar:        true,
		},
		Auth: AuthConfig{
			Username: "dhwani",
			Password: "dhwani",
		},
		Database: DatabaseConfig{Path: "./data/dhwani.db"},
		Logging:  LoggingConfig{Level: "info"},
		Upstream: UpstreamConfig{
			InstancesURL:      "https://monochrome.tf/instances.json",
			ClientHeader:      "BiniLossless/v3.4",
			Source:            "tidal",
			StreamQuality:     "LOSSLESS",
			TimeoutSec:        20,
			ProbeIntervalSec:  10800,
			AttemptTimeoutSec: 6,
			FallbackAttempts:  2,
		},
	}
}

func Load() (Config, error) {
	cfg := Default()
	applyEnvOverrides(&cfg)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

func (c Config) Validate() error {
	if c.Auth.Username == "" || c.Auth.Password == "" {
		return errors.New("auth.username and auth.password are required")
	}
	if c.Server.Port <= 0 {
		return errors.New("server.port must be > 0")
	}
	if strings.TrimSpace(c.Upstream.InstancesURL) == "" && strings.TrimSpace(c.Upstream.InstancesFile) == "" {
		return errors.New("at least one upstream source is required: upstream.instances_url or upstream.instances_file")
	}
	return nil
}

func applyEnvOverrides(c *Config) {
	if v := os.Getenv("DHWANI_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Server.Port = n
		}
	}
	if v := os.Getenv("DHWANI_HOST"); v != "" {
		c.Server.Host = v
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_ENABLE_JSON_RESPONSES")); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Server.EnableJSONResponses = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_INGEST_ON_STREAM")); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Server.IngestOnStream = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_INGEST_ON_STAR")); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Server.IngestOnStar = b
		}
	}
	if v := os.Getenv("DHWANI_USERNAME"); v != "" {
		c.Auth.Username = v
	}
	if v := os.Getenv("DHWANI_PASSWORD"); v != "" {
		c.Auth.Password = v
	}
	if v := os.Getenv("DHWANI_DB_PATH"); v != "" {
		c.Database.Path = v
	}
	if v := os.Getenv("DHWANI_LOG_LEVEL"); v != "" {
		c.Logging.Level = strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_STREAM_QUALITY")); v != "" {
		c.Upstream.StreamQuality = v
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_INSTANCES_URL")); v != "" {
		c.Upstream.InstancesURL = v
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_INSTANCES_FILE")); v != "" {
		c.Upstream.InstancesFile = v
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_CLIENT_HEADER")); v != "" {
		c.Upstream.ClientHeader = v
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_SOURCE")); v != "" {
		c.Upstream.Source = v
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_UPSTREAM_TIMEOUT_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Upstream.TimeoutSec = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_UPSTREAM_PROBE_INTERVAL_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Upstream.ProbeIntervalSec = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_PROVIDER_ATTEMPT_TIMEOUT_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Upstream.AttemptTimeoutSec = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("DHWANI_PROVIDER_FALLBACK_ATTEMPTS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Upstream.FallbackAttempts = n
		}
	}
}
