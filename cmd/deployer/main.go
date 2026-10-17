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

	"github.com/Blankon-Developer/be-pawfund/internal/app"
	"github.com/Blankon-Developer/be-pawfund/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("deployer stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadDeployer()
	if err != nil {
		return err
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()

	deployer, err := app.NewDeployer(startupCtx, cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := deployer.Close(); err != nil {
			logger.Error("close deployer", "error", err)
		}
	}()

	shutdownSignal, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/health", deployer.HealthHandler())

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           healthMux,
		IdleTimeout:       time.Minute,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("deployer health server started", "address", cfg.HTTPAddr)
		serverErrors <- server.ListenAndServe()
	}()

	consumerErrors := make(chan error, 1)
	go func() {
		logger.Info("campaign deployment consumer started", "queue", app.CampaignDeploymentTopology().Queue)
		consumerErrors <- deployer.Run(shutdownSignal)
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve health: %w", err)
	case err := <-consumerErrors:
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("consume campaign deployments: %w", err)
		}
		return nil
	case <-shutdownSignal.Done():
		logger.Info("deployer shutting down")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown health server: %w", err)
	}

	if err := <-consumerErrors; err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("stop campaign deployment consumer: %w", err)
	}

	if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("stop health server: %w", err)
	}

	return nil
}
