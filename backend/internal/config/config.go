package config

import (
	"fmt"
	"os"
)

const (
	defaultHTTPAddr         = ":8080"
	defaultPolicyBundlePath = "policies/dev.json"
	defaultDBPath           = "stamper.db"
)

type Config struct {
	HTTPAddr         string
	PolicyBundlePath string
	DBPath           string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:         getenv("STAMPER_HTTP_ADDR", defaultHTTPAddr),
		PolicyBundlePath: getenv("STAMPER_POLICY_BUNDLE_PATH", defaultPolicyBundlePath),
		DBPath:           getenv("STAMPER_DB_PATH", defaultDBPath),
	}

	if cfg.HTTPAddr == "" {
		return Config{}, fmt.Errorf("http address must not be empty")
	}

	if cfg.PolicyBundlePath == "" {
		return Config{}, fmt.Errorf("policy bundle path must not be empty")
	}
	if cfg.DBPath == "" {
		return Config{}, fmt.Errorf("db path must not be empty")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
