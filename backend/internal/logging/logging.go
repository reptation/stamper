package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DefaultLevel    = "info"
	DefaultFormat   = "json"
	FormatJSON      = "json"
	FormatPretty    = "pretty"
	RequestIDHeader = "X-Request-ID"
)

type Config struct {
	Level  string
	Format string
}

type requestIDContextKey struct{}

func New(service string) (*slog.Logger, Config, error) {
	cfg, err := loadConfigFromEnv()
	if err != nil {
		return nil, Config{}, err
	}

	logger, err := NewWithConfig(service, cfg, os.Stderr)
	if err != nil {
		return nil, Config{}, err
	}

	return logger, cfg, nil
}

func NewWithConfig(service string, cfg Config, writer io.Writer) (*slog.Logger, error) {
	normalized, level, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	if writer == nil {
		writer = os.Stderr
	}

	options := &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceAttr,
	}

	var handler slog.Handler
	switch normalized.Format {
	case FormatPretty:
		handler = slog.NewTextHandler(writer, options)
	case FormatJSON:
		handler = slog.NewJSONHandler(writer, options)
	default:
		return nil, fmt.Errorf("unsupported log format %q", normalized.Format)
	}

	return slog.New(handler).With("service", service), nil
}

func NewFallback(service string) *slog.Logger {
	logger, err := NewWithConfig(service, Config{
		Level:  DefaultLevel,
		Format: DefaultFormat,
	}, os.Stderr)
	if err != nil {
		return slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", service)
	}

	return logger
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(RequestIDHeader))
		if requestID == "" {
			requestID = NewRequestID()
		}

		w.Header().Set(RequestIDHeader, requestID)
		r.Header.Set(RequestIDHeader, requestID)

		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func WithContext(logger *slog.Logger, ctx context.Context) *slog.Logger {
	if logger == nil {
		return nil
	}

	if requestID := RequestID(ctx); requestID != "" {
		return logger.With("request_id", requestID)
	}

	return logger
}

func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}

	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

func PropagateRequestID(req *http.Request, ctx context.Context) {
	if req == nil {
		return
	}

	if requestID := RequestID(ctx); requestID != "" {
		req.Header.Set(RequestIDHeader, requestID)
	}
}

func NewRequestID() string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Sprintf("req_%d", time.Now().UTC().UnixNano())
	}

	return "req_" + hex.EncodeToString(random[:])
}

func loadConfigFromEnv() (Config, error) {
	normalized, _, err := normalizeConfig(Config{
		Level:  os.Getenv("LOG_LEVEL"),
		Format: os.Getenv("LOG_FORMAT"),
	})
	if err != nil {
		return Config{}, err
	}

	return normalized, nil
}

func normalizeConfig(cfg Config) (Config, slog.Level, error) {
	normalized := Config{
		Level:  strings.ToLower(strings.TrimSpace(cfg.Level)),
		Format: strings.ToLower(strings.TrimSpace(cfg.Format)),
	}
	if normalized.Level == "" {
		normalized.Level = DefaultLevel
	}
	if normalized.Format == "" {
		normalized.Format = DefaultFormat
	}

	level, err := parseLevel(normalized.Level)
	if err != nil {
		return Config{}, 0, err
	}

	if normalized.Format != FormatJSON && normalized.Format != FormatPretty {
		return Config{}, 0, fmt.Errorf("LOG_FORMAT must be one of %q or %q", FormatPretty, FormatJSON)
	}

	return normalized, level, nil
}

func parseLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("LOG_LEVEL must be one of %q, %q, %q, or %q", "debug", "info", "warn", "error")
	}
}

func replaceAttr(_ []string, attr slog.Attr) slog.Attr {
	switch attr.Key {
	case slog.TimeKey:
		t, ok := attr.Value.Any().(time.Time)
		if !ok {
			return slog.String("timestamp", attr.Value.String())
		}
		return slog.String("timestamp", t.UTC().Format(time.RFC3339Nano))
	case slog.LevelKey:
		switch level := attr.Value.Any().(type) {
		case slog.Level:
			return slog.String("level", strings.ToLower(level.String()))
		case slog.Leveler:
			return slog.String("level", strings.ToLower(level.Level().String()))
		default:
			return slog.String("level", strings.ToLower(attr.Value.String()))
		}
	case slog.MessageKey:
		attr.Key = "message"
		return attr
	default:
		return attr
	}
}
