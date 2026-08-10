package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Blankon-Developer/be-pawfund/internal/api"
	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/cache"
	"github.com/Blankon-Developer/be-pawfund/internal/config"
	appmiddleware "github.com/Blankon-Developer/be-pawfund/internal/middleware"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
	"github.com/google/uuid"
)

type Application struct {
	DB                *sql.DB
	Cache             *cache.CacheClient
	AuthHandler       *api.AuthHandler
	UploadHandler     *api.UploadHandler
	SupporterHandler  *api.SupporterHandler
	FundraiserHandler *api.FundraiserHandler
	Authenticate      func(http.Handler) http.Handler
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Application, error) {
	db, err := repository.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	var cacheClient *cache.CacheClient
	fail := func(startupErr error) (*Application, error) {
		var cleanupErrors []error
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

	supporterRepository := repository.NewPostgresSupporterRepository(db)
	fundraiserRepository := repository.NewPostgresFundraiserRepository(db)
	authRepository := repository.NewPostgresAuthRepository(db)

	supporterService := service.NewSupporterService(supporterRepository, uuid.NewV7)
	fundraiserService := service.NewFundraiserService(fundraiserRepository, uuid.NewV7, objectDeleter)
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

	supporterHandler := api.NewSupporterHandler(supporterService, urlBuilder, logger)
	fundraiserHandler := api.NewFundraiserHandler(fundraiserService, urlBuilder, logger)
	authHandler := api.NewAuthHandler(authService, urlBuilder, logger)
	uploadHandler := api.NewUploadHandler(uploadService, logger)

	return &Application{
		DB:                db,
		Cache:             cacheClient,
		AuthHandler:       authHandler,
		UploadHandler:     uploadHandler,
		SupporterHandler:  supporterHandler,
		FundraiserHandler: fundraiserHandler,
		Authenticate:      appmiddleware.Authenticate(jwtManager, logger),
	}, nil
}

func (a *Application) Close() error {
	var closeErrors []error
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
