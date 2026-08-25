package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"queue-manager/internal/config"
	"queue-manager/internal/db"
	"queue-manager/internal/handlers"
	appmw "queue-manager/internal/middleware"
	"queue-manager/internal/worker"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()

	client, database, err := db.Connect(cfg.MongoURI, cfg.MongoDBName)
	if err != nil {
		slog.Error("mongo connect failed", "err", err)
		os.Exit(1)
	}

	indexCtx, indexCancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := db.EnsureIndexes(indexCtx, database); err != nil {
		indexCancel()
		slog.Error("ensure indexes failed", "err", err)
		os.Exit(1)
	}
	promoted, err := db.EnsureSuperAdmin(indexCtx, database)
	indexCancel()
	if err != nil {
		slog.Error("ensure super admin failed", "err", err)
		os.Exit(1)
	}
	if promoted {
		slog.Warn("no super admin existed — promoted the oldest admin account to super admin")
	}
	slog.Info("mongo connected and indexes ensured", "db", cfg.MongoDBName)

	srv := &handlers.Server{DB: database, Config: cfg}

	manager := worker.NewManager(database)
	bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := worker.Bootstrap(bootstrapCtx, database, manager); err != nil {
		bootstrapCancel()
		slog.Error("worker bootstrap failed", "err", err)
		os.Exit(1)
	}
	bootstrapCancel()
	srv.SetLifecycle(manager)
	slog.Info("worker started for active transmission channels")

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	r := chi.NewRouter()
	r.Use(appmw.Logging)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Post("/ingest/*", srv.Ingest)

	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/login", srv.Login)
		r.Post("/2fa/verify", srv.Verify2FA)
		r.Post("/logout", srv.Logout)

		r.Group(func(r chi.Router) {
			r.Use(appmw.RequireAuthAllowMustSetup2FA(cfg.JWTSecret))
			r.Get("/me", srv.Me)
			r.Post("/2fa/setup", srv.Setup2FA)
			r.Post("/2fa/setup/confirm", srv.ConfirmSetup2FA)
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(appmw.RequireAuth(cfg.JWTSecret))

		r.Route("/api/products", func(r chi.Router) {
			r.Get("/", srv.ListProducts)
			r.Post("/", srv.CreateProduct)
			r.Get("/{id}", srv.GetProduct)
			r.Put("/{id}", srv.UpdateProduct)
			r.Delete("/{id}", srv.DeleteProduct)
		})

		r.Route("/api/templates", func(r chi.Router) {
			r.Get("/", srv.ListTemplates)
			r.Post("/", srv.CreateTemplate)
			r.Get("/{id}", srv.GetTemplate)
			r.Put("/{id}", srv.UpdateTemplate)
			r.Delete("/{id}", srv.DeleteTemplate)
			r.Patch("/{id}/activate", srv.ActivateTemplate)
			r.Patch("/{id}/deactivate", srv.DeactivateTemplate)
		})

		r.Route("/api/requests", func(r chi.Router) {
			r.Get("/", srv.ListRequests)
			r.Get("/{id}", srv.GetRequest)
			r.Post("/{id}/retry", srv.RetryRequest)
		})

		r.Route("/api/admins", func(r chi.Router) {
			r.Get("/", srv.ListAdmins)
			r.Post("/", srv.CreateAdmin)
			r.Patch("/{id}/disable-2fa", srv.DisableAdmin2FA)
		})
	})

	// no-cache: http.FileServer/ServeFile set Last-Modified but no Cache-Control,
	// so browsers apply their own heuristic caching and can keep serving an
	// edited HTML/JS/CSS file indefinitely. Forcing revalidation on every
	// request keeps the console frontend from going stale after a deploy.
	noCache := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache")
			h(w, r)
		}
	}

	r.Handle("/static/*", http.StripPrefix("/static/", noCache(http.FileServer(http.Dir(filepath.Join(cfg.WebDir, "static"))).ServeHTTP)))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard.html", http.StatusFound)
	})
	for _, page := range []string{"login.html", "dashboard.html", "products.html", "templates.html", "requests.html", "admins.html", "2fa-setup.html", "guide.html"} {
		page := page
		r.Get("/"+page, noCache(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filepath.Join(cfg.WebDir, page))
		}))
	}

	httpServer := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	go func() {
		slog.Info("starting server", "port", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped unexpectedly", "err", err)
			os.Exit(1)
		}
	}()

	<-shutdownCtx.Done()
	slog.Info("shutting down")

	httpCtx, httpCancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = httpServer.Shutdown(httpCtx)
	httpCancel()

	manager.StopAll()
	_ = client.Disconnect(context.Background())

	slog.Info("shutdown complete")
}
