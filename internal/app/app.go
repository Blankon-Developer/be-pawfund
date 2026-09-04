package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/config"
	"github.com/Blankon-Developer/be-pawfund/internal/http/handler"
	appmiddleware "github.com/Blankon-Developer/be-pawfund/internal/http/middleware"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/cache"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/database"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/queue"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/storage"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/google/uuid"
)

type Application struct {
	DB                 *sql.DB
	Cache              *cache.CacheClient
	Queue              *queue.QueueClient
	AuthHandler        *handler.AuthHandler
	UploadHandler      *handler.UploadHandler
	SupporterHandler   *handler.SupporterHandler
	FundraiserHandler  *handler.FundraiserHandler
	CampaignHandler    *handler.CampaignHandler
	Authenticate       func(http.Handler) http.Handler
	CORSAllowedOrigins []string
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Application, error) {
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	var cacheClient *cache.CacheClient
	var queueClient *queue.QueueClient
	fail := func(startupErr error) (*Application, error) {
		var cleanupErrors []error
		if queueClient != nil {
			if err := queueClient.Close(); err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
		}
		if cacheClient != nil {
			if err := cacheClient.Close(); err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
		}
		if err := db.Close(); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("app: close database after startup failure: %w", err))
		}
		return nil, errors.Join(append([]error{startupErr}, cleanupErrors...)...)
	}

	jwtManager, err := auth.NewJWTManager(cfg.JWTSecret)
	if err != nil {
		return fail(fmt.Errorf("app: initialize JWT manager: %w", err))
	}

	urlBuilder, err := storage.NewPublicURLBuilder(cfg.StoragePublicBaseURL)
	if err != nil {
		return fail(fmt.Errorf("app: initialize public URL builder: %w", err))
	}
	putPresigner, err := storage.NewPutPresigner(storage.PresignerConfig{
		Endpoint:  cfg.StorageEndpoint,
		AccessKey: cfg.StorageAccessKey,
		SecretKey: cfg.StorageSecretKey,
		Bucket:    cfg.StorageBucket,
		Region:    cfg.StorageRegion,
		TTL:       cfg.StoragePresignTTL,
	})
	if err != nil {
		return fail(fmt.Errorf("app: initialize storage presigner: %w", err))
	}
	objectDeleter, err := storage.NewObjectDeleter(storage.PresignerConfig{
		Endpoint:  cfg.StorageEndpoint,
		AccessKey: cfg.StorageAccessKey,
		SecretKey: cfg.StorageSecretKey,
		Bucket:    cfg.StorageBucket,
		Region:    cfg.StorageRegion,
	})
	if err != nil {
		return fail(fmt.Errorf("app: initialize storage object deleter: %w", err))
	}

	cacheClient, err = cache.Open(ctx, cache.Config{URL: cfg.CacheURL, KeyPrefix: cfg.CacheKeyPrefix})
	if err != nil {
		return fail(fmt.Errorf("app: initialize cache: %w", err))
	}

	queueClient, err = queue.Open(ctx, queue.Config{URL: cfg.QueueURL, Logger: logger})
	if err != nil {
		return fail(fmt.Errorf("app: initialize queue: %w", err))
	}

	supporterRepository := database.NewPostgresSupporterRepository(db)
	fundraiserRepository := database.NewPostgresFundraiserRepository(db)
	campaignRepository := database.NewPostgresCampaignRepository(db)
	authRepository := database.NewPostgresAuthRepository(db)

	supporterService := service.NewSupporterService(supporterRepository, uuid.NewV7, objectDeleter)
	fundraiserService := service.NewFundraiserService(fundraiserRepository, uuid.NewV7, objectDeleter)
	campaignService := service.NewCampaignService(campaignRepository, uuid.NewV7)
	authService := service.NewAuthService(
		cacheClient,
		authRepository,
		jwtManager,
		service.AuthConfig{
			Domain:         cfg.SIWEDomain,
			URI:            cfg.SIWEURI,
			ChainID:        cfg.SIWEChainID,
			MessageTTL:     cfg.SIWEMessageTTL,
			AccessTokenTTL: cfg.JWTTTL,
		},
	)
	uploadService := service.NewUploadService(putPresigner, uuid.NewV7)

	supporterHandler := handler.NewSupporterHandler(supporterService, urlBuilder, logger)
	fundraiserHandler := handler.NewFundraiserHandler(fundraiserService, urlBuilder, logger)
	campaignHandler := handler.NewCampaignHandler(campaignService, urlBuilder, logger)
	authHandler := handler.NewAuthHandler(authService, urlBuilder, cfg.SIWEChainID, logger)
	uploadHandler := handler.NewUploadHandler(uploadService, logger)

	return &Application{
		DB:                 db,
		Cache:              cacheClient,
		Queue:              queueClient,
		AuthHandler:        authHandler,
		UploadHandler:      uploadHandler,
		SupporterHandler:   supporterHandler,
		FundraiserHandler:  fundraiserHandler,
		CampaignHandler:    campaignHandler,
		Authenticate:       appmiddleware.Authenticate(jwtManager, logger),
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
	}, nil
}

func (a *Application) Close() error {
	var closeErrors []error
	if a.Queue != nil {
		if err := a.Queue.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("app: close queue: %w", err))
		}
	}
	if a.Cache != nil {
		if err := a.Cache.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("app: close cache: %w", err))
		}
	}
	if a.DB != nil {
		if err := a.DB.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("app: close database: %w", err))
		}
	}
	return errors.Join(closeErrors...)
}
