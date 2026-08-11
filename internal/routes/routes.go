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

	router.Route("/v1", func(r chi.Router) {
		r.Post("/auth/message", application.AuthHandler.HandleCreateMessage)
		r.Post("/auth/verify", application.AuthHandler.HandleVerify)
		r.Get("/fundraiser/{address}", application.FundraiserHandler.HandleGetPublicProfile)

		r.Group(func(r chi.Router) {
			r.Use(application.Authenticate)

			r.Get("/auth/me", application.AuthHandler.HandleGetMe)
			r.Post("/uploads/profile-image/presign", application.UploadHandler.HandlePresignProfileImage)
			r.Post("/uploads/campaign-image/presign", application.UploadHandler.HandlePresignCampaignImage)
			r.Post("/register/supporter", application.SupporterHandler.HandleRegisterSupporter)
			r.Post("/register/fundraiser", application.FundraiserHandler.HandleRegisterFundraiser)
			r.Get("/supporter/profile", application.SupporterHandler.HandleGetProfile)
			r.Get("/fundraiser/profile", application.FundraiserHandler.HandleGetProfile)
			r.Put("/fundraiser/profile", application.FundraiserHandler.HandleReplaceProfile)
			r.Delete("/fundraiser/profile", application.FundraiserHandler.HandleDeleteProfile)
			r.Post("/fundraiser/campaigns", application.CampaignHandler.HandleCreateCampaign)
			r.Get("/fundraiser/campaigns/{id}", application.CampaignHandler.HandleGetMyCampaignDetail)
		})
	})

	return router
}
