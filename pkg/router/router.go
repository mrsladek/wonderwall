package router

import (
	"github.com/go-chi/chi/v5"
	chi_middleware "github.com/go-chi/chi/v5/middleware"

	"github.com/nais/wonderwall/pkg/config"
	"github.com/nais/wonderwall/pkg/handler"
	"github.com/nais/wonderwall/pkg/middleware"
	"github.com/nais/wonderwall/pkg/router/paths"
)

func New(handler *handler.Handler) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.CorrelationIDHandler)
	r.Use(chi_middleware.Recoverer)
	prometheusMiddleware := middleware.NewPrometheusMiddleware("wonderwall", handler.OpenIDConfig.Provider().Name())

	prefix := config.ParseIngress(handler.Config.Ingress)

	r.Group(func(r chi.Router) {
		r.Use(middleware.LogEntryHandler(handler.OpenIDConfig.Provider().Name()))
		r.Use(prometheusMiddleware.Handler)
		r.Use(chi_middleware.NoCache)

		r.Route(prefix+paths.OAuth2, func(r chi.Router) {
			r.Get(paths.Login, handler.Login)
			r.Get(paths.Callback, handler.Callback)
			r.Get(paths.Logout, handler.Logout)
			r.Get(paths.FrontChannelLogout, handler.FrontChannelLogout)
			r.Get(paths.LogoutCallback, handler.LogoutCallback)
		})
	})

	r.HandleFunc("/*", handler.Default)
	return r
}
