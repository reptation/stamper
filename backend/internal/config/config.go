package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr         = ":8080"
	defaultProxyHTTPAddr    = ":8081"
	defaultPolicyBundlePath = "policies/dev.json"
	defaultDBPath           = "stamper.db"
	defaultApprovalTokenTTL = 60 * time.Second
)

type Config struct {
	HTTPAddr         string
	ProxyHTTPAddr    string
	StamperBaseURL   string
	PolicyBundlePath string
	DBPath           string
	ApprovalTokenTTL time.Duration
}

func Load() (Config, error) {
	httpAddr := getenv("STAMPER_HTTP_ADDR", defaultHTTPAddr)
	approvalTokenTTL, err := getenvDurationSeconds("STAMPER_APPROVAL_TOKEN_TTL_SECONDS", defaultApprovalTokenTTL)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTPAddr:         httpAddr,
		ProxyHTTPAddr:    getenv("STAMPER_PROXY_HTTP_ADDR", defaultProxyHTTPAddr),
		StamperBaseURL:   getenv("STAMPER_BASE_URL", apiBaseURL(httpAddr)),
		PolicyBundlePath: getenv("STAMPER_POLICY_BUNDLE_PATH", defaultPolicyBundlePath),
		DBPath:           getenv("STAMPER_DB_PATH", defaultDBPath),
		ApprovalTokenTTL: approvalTokenTTL,
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
	if cfg.ProxyHTTPAddr == "" {
		return Config{}, fmt.Errorf("proxy http address must not be empty")
	}
	if cfg.StamperBaseURL == "" {
		return Config{}, fmt.Errorf("stamper base url must not be empty")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func getenvDurationSeconds(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer number of seconds", key)
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", key)
	}

	return time.Duration(seconds) * time.Second, nil
}

func apiBaseURL(httpAddr string) string {
	if strings.HasPrefix(httpAddr, "http://") || strings.HasPrefix(httpAddr, "https://") {
		return strings.TrimRight(httpAddr, "/")
	}

	if strings.HasPrefix(httpAddr, ":") {
		return "http://127.0.0.1" + httpAddr
	}

	host, port, err := net.SplitHostPort(httpAddr)
	if err == nil {
		if host == "" {
			host = "127.0.0.1"
		}
		return "http://" + net.JoinHostPort(host, port)
	}

	return "http://" + httpAddr
}
