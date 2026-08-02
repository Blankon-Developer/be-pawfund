package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Blankon-Developer/be-pawfund/internal/api"
	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/config"
	appmiddleware "github.com/Blankon-Developer/be-pawfund/internal/middleware"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
	"github.com/google/uuid"
)

type Application struct {
	DB               *sql.DB
	SupporterHandler *api.SupporterHandler
	Authenticate     func(http.Handler) http.Handler
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Application, error) {
	db, err := repository.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	fail := func(err error) (*Application, error) {
		_ = db.Close()
		return nil, err
	}

	jwtManager, err := auth.NewJWTManager(cfg.JWTSecret)
	if err != nil {
		return fail(fmt.Errorf("app: initialize JWT manager: %w", err))
	}

	urlBuilder, err := storage.NewPublicURLBuilder(cfg.StoragePublicBaseURL)
	if err != nil {
		return fail(fmt.Errorf("app: initialize public URL builder: %w", err))
	}

	supporterRepository := repository.NewPostgresSupporterRepository(db)
	supporterService := service.NewSupporterService(supporterRepository, uuid.NewV7)
	supporterHandler := api.NewSupporterHandler(supporterService, urlBuilder, logger)

	return &Application{
		DB:               db,
		SupporterHandler: supporterHandler,
		Authenticate:     appmiddleware.Authenticate(jwtManager, logger),
	}, nil
}

func (a *Application) Close() error {
	if err := a.DB.Close(); err != nil {
		return fmt.Errorf("app: close database: %w", err)
	}
	return nil
}
