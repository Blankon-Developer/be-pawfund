package routes

import (
	"log/slog"
	"net/http"

	"github.com/Blankon-Developer/be-pawfund/internal/app"
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
	appmiddleware "github.com/Blankon-Developer/be-pawfund/internal/middleware"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func Setup(application *app.Application, logger *slog.Logger) http.Handler {
	router := chi.NewRouter()
	router.Use(chimiddleware.RequestID)
	router.Use(appmiddleware.Recover(logger))

	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		if err := httpx.WriteSuccess(
			w,
			http.StatusOK,
			"HEALTHY",
			"Service is healthy.",
			map[string]string{"service": "pawfund-api"},
		); err != nil {
			logger.Error("write health response", "error", err)
		}
	})

	router.Post("/v1/auth/message", application.AuthHandler.HandleCreateMessage)
	router.Post("/v1/auth/verify", application.AuthHandler.HandleVerify)

	router.With(application.Authenticate).Get("/v1/auth/me", application.AuthHandler.HandleGetMe)
	router.With(application.Authenticate).Post(
		"/v1/register/supporter",
		application.SupporterHandler.HandleRegisterSupporter,
	)
	router.With(application.Authenticate).Post(
		"/v1/register/fundraiser",
		application.FundraiserHandler.HandleRegisterFundraiser,
	)

	return router
}
