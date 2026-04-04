package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/reptation/stamper/backend/internal/approval"
	"github.com/reptation/stamper/backend/internal/config"
	"github.com/reptation/stamper/backend/internal/httpapi"
	"github.com/reptation/stamper/backend/internal/logging"
	"github.com/reptation/stamper/backend/internal/policy"
	"github.com/reptation/stamper/backend/internal/storage"
)

func main() {
	logger, logConfig, err := logging.New("stamperd")
	if err != nil {
		logging.NewFallback("stamperd").Error("logging initialization failed",
			"component", "startup",
			"error", err,
		)
		os.Exit(1)
	}

	if err := run(logger, logConfig); err != nil {
		logger.Error("service terminated",
			"component", "startup",
			"error", err,
		)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, logConfig logging.Config) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger.Info("configuration loaded",
		"component", "startup",
		"http_addr", cfg.HTTPAddr,
		"proxy_http_addr", cfg.ProxyHTTPAddr,
		"stamper_base_url", cfg.StamperBaseURL,
		"policy_bundle_path", cfg.PolicyBundlePath,
		"db_path", cfg.DBPath,
		"approval_token_ttl_seconds", int(cfg.ApprovalTokenTTL.Seconds()),
		"log_level", logConfig.Level,
		"log_format", logConfig.Format,
	)

	store, err := storage.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	server := httpapi.NewServer(store, logger)
	server.SetApprovalTokenStore(approval.NewStore(cfg.ApprovalTokenTTL))

	bundle, err := policy.LoadBundle(cfg.PolicyBundlePath)
	if err != nil {
		return fmt.Errorf("startup policy bundle load failed: %w", err)
	}
	server.SetPolicyBundle(bundle)
	logger.Info("policy bundle loaded",
		"component", "startup",
		"policy_bundle_path", cfg.PolicyBundlePath,
		"policy_bundle_version", bundle.Version,
		"policy_count", len(bundle.Policies),
	)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening",
			"component", "startup",
			"listen_addr", cfg.HTTPAddr,
		)
		errCh <- httpServer.ListenAndServe()
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			logger.Info("server stopped",
				"component", "startup",
			)
			return nil
		}
		return fmt.Errorf("serve http: %w", err)
	case <-signalCtx.Done():
		logger.Info("shutdown signal received",
			"component", "startup",
		)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown http server: %w", err)
		}

		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			logger.Info("server stopped",
				"component", "startup",
			)
			return nil
		}
		return err
	}
}
