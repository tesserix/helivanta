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

	"github.com/tesserix/hms/internal/bootstrap"
	"github.com/tesserix/hms/internal/config"
	"github.com/tesserix/hms/internal/httpserver"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := tenantdb.Open(cfg.AppDatabaseURL, cfg.AdminDatabaseURL)
	if err != nil {
		return err
	}

	registry, err := bootstrap.NewRegistry()
	if err != nil {
		return err
	}

	migs := bootstrap.PlatformMigrations()
	for _, m := range registry.All() {
		migs = append(migs, m.Migrations()...)
	}
	if err := db.Migrate(ctx, migs); err != nil {
		return err
	}
	if bad, err := db.LintRLS(ctx); err != nil {
		return err
	} else if len(bad) > 0 {
		return fmt.Errorf("tables missing forced RLS: %v", bad)
	}

	bus, err := events.NewBus(cfg.NATSURL)
	if err != nil {
		return err
	}
	defer bus.Close()

	verifier, err := authn.NewGIPVerifier(ctx, cfg.GIPProjectID, cfg.IsDev())
	if err != nil {
		return err
	}

	minter, err := authn.NewGIPMinter(ctx, cfg.GIPProjectID, cfg.IsDev())
	if err != nil {
		return err
	}

	fga, err := authz.NewClient(ctx, cfg.OpenFGAURL, cfg.OpenFGAStore)
	if err != nil {
		return err
	}
	if err := platform.Reconcile(ctx, registry, db, fga); err != nil {
		return err
	}

	srv := httpserver.New(
		[]httpserver.ReadyCheck{
			{Name: "postgres", Check: db.PingContext},
			{Name: "nats", Check: bus.Ping},
			{Name: "openfga", Check: fga.Ping},
		},
		requestid.Middleware(),
	)
	deps := platform.Deps{
		DB:     db,
		Bus:    bus,
		Authz:  fga,
		Roles:  fga,
		Tokens: minter,
		Reconcile: func(ctx context.Context, tenantID string) error {
			return platform.ReconcileTenant(ctx, registry, fga, tenantID)
		},
	}
	api := platform.NewRouter(srv.Engine.Group("/v1",
		authn.Middleware(verifier),
		authz.Middleware(fga),
	))
	for _, m := range registry.All() {
		m.Routes(api, deps)
		if err := bus.StartConsumers(ctx, db, m.Consumers(deps)); err != nil {
			return err
		}
	}
	go bus.RunDispatcher(ctx, db)

	httpSrv := &http.Server{Addr: ":" + cfg.Port, Handler: srv.Engine, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	slog.Info("api listening", "port", cfg.Port)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
