package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Blankon-Developer/be-pawfund/internal/config"
	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/blockchain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/database"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/queue"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/google/uuid"
)

// Deployer runs the campaign deployment queue consumer.
type Deployer struct {
	DB              *sql.DB
	Queue           *queue.QueueClient
	consumer        *queue.Consumer
	worker          *service.CampaignDeploymentWorker
	closeChain      func() error
	logger          *slog.Logger
}

func NewDeployer(ctx context.Context, cfg config.DeployerConfig, logger *slog.Logger) (*Deployer, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("app: deployer database: %w", err)
	}

	var queueClient *queue.QueueClient
	fail := func(startupErr error) (*Deployer, error) {
		if queueClient != nil {
			if err := queueClient.Close(); err != nil {
				startupErr = errors.Join(startupErr, fmt.Errorf("app: close queue after startup failure: %w", err))
			}
		}
		if err := db.Close(); err != nil {
			startupErr = errors.Join(startupErr, fmt.Errorf("app: close database after startup failure: %w", err))
		}
		return nil, startupErr
	}

	queueClient, err = queue.Open(ctx, queue.Config{URL: cfg.QueueURL, Logger: logger})
	if err != nil {
		return fail(fmt.Errorf("app: deployer queue: %w", err))
	}

	topology := CampaignDeploymentTopology()
	if err := queue.DeclareOn(queueClient, topology); err != nil {
		return fail(fmt.Errorf("app: declare campaign deployment topology: %w", err))
	}

	consumer, err := queue.NewConsumer(queueClient, queue.ConsumerConfig{
		Queue:    topology.Queue,
		Topology: &topology,
		Prefetch: 1,
		Logger:   logger,
	})
	if err != nil {
		return fail(fmt.Errorf("app: deployer consumer: %w", err))
	}

	campaignRepository := database.NewPostgresCampaignRepository(db)

	chainDeployer, err := blockchain.NewConfiguredCampaignDeployer(
		ctx,
		cfg.DeploymentMode,
		cfg.ChainID,
		cfg.ChainRPCURL,
		cfg.CampaignFactoryAddress,
		cfg.DeployerPrivateKey,
		cfg.ConfirmTimeout,
	)
	if err != nil {
		return fail(fmt.Errorf("app: deployer campaign deployer: %w", err))
	}

	logger.Info(
		"deployer chain adapter configured",
		"deploymentMode", cfg.DeploymentMode,
	)

	return &Deployer{
		DB:       db,
		Queue:    queueClient,
		consumer: consumer,
		worker: service.NewCampaignDeploymentWorker(
			cfg.ChainID,
			campaignRepository,
			chainDeployer.Deployer,
			uuid.NewV7,
			logger,
		),
		closeChain: chainDeployer.Close,
		logger:     logger,
	}, nil
}

// HealthHandler reports deployer readiness. It pings the database when configured.
func (d *Deployer) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if d != nil && d.DB != nil {
			if err := d.DB.PingContext(context.Background()); err != nil {
				if writeErr := httpx.WriteError(
					w,
					http.StatusServiceUnavailable,
					"UNHEALTHY",
					"Database is unavailable.",
					nil,
				); writeErr != nil {
					d.logger.Error("write health response", "error", writeErr)
				}
				return
			}
		}

		if err := httpx.WriteSuccess(
			w,
			http.StatusOK,
			"HEALTHY",
			"Service is healthy.",
			map[string]string{"service": "pawfund-deployer"},
		); err != nil {
			d.logger.Error("write health response", "error", err)
		}
	}
}

// Run consumes deployment tasks until ctx is cancelled.
func (d *Deployer) Run(ctx context.Context) error {
	if d == nil || d.consumer == nil || d.worker == nil {
		return fmt.Errorf("app: deployer run: deployer is required")
	}
	return d.consumer.Run(ctx, d.handleDelivery)
}

func (d *Deployer) handleDelivery(ctx context.Context, delivery queue.Delivery) error {
	task, err := DecodeCampaignDeploymentMessage(delivery.Body)
	if err != nil {
		d.logger.Error(
			"campaign deployment message invalid",
			"error", err,
			"routingKey", delivery.RoutingKey,
			"messageId", delivery.MessageID,
		)
		return nil
	}
	if err := d.worker.Handle(ctx, task); err != nil {
		return fmt.Errorf("app: handle campaign deployment %s: %w", task.CampaignID, err)
	}
	return nil
}

func (d *Deployer) Close() error {
	var closeErrors []error
	if d.closeChain != nil {
		if err := d.closeChain(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("app: close chain deployer: %w", err))
		}
	}
	if d.Queue != nil {
		if err := d.Queue.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("app: close queue: %w", err))
		}
	}
	if d.DB != nil {
		if err := d.DB.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("app: close database: %w", err))
		}
	}
	return errors.Join(closeErrors...)
}
