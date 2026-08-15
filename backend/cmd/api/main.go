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

	"github.com/gin-gonic/gin"

	"github.com/tesserix/hms/internal/bootstrap"
	"github.com/tesserix/hms/internal/config"
	"github.com/tesserix/hms/internal/httpserver"
	"github.com/tesserix/hms/internal/modules/iam"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/logging"
	"github.com/tesserix/hms/pkg/ratelimit"
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
	// Before anything else logs: until this runs, slog.Default() is the
	// unconfigured text handler on stderr and nothing is redacted.
	slog.SetDefault(logging.New(cfg.LogLevel))
	// Logged once at boot because HMS_ENV silently gates production safety
	// checks (see Config.IsDev) — a prod process accidentally started with
	// HMS_ENV=dev would otherwise disable them with no signal anywhere.
	slog.Info("resolved environment", "env", cfg.Env, "is_dev", cfg.IsDev())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := tenantdb.Open(cfg.AppDatabaseURL, cfg.AdminDatabaseURL)
	if err != nil {
		return err
	}

	// Constructed once, before the registry, and handed to BOTH
	// bootstrap.NewRegistry (which threads it into iam.New) and
	// authn.Middleware below: the module's sign-out/revoke handlers and
	// its broadcast consumer must invalidate the SAME cache instance the
	// authentication path reads on every request. Two separately
	// constructed checkers would compile and pass most tests while the
	// middleware's cache silently never gets invalidated (#781).
	revocationChecker := iam.NewRevocationChecker(db)

	registry, err := bootstrap.NewRegistry(revocationChecker)
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

	revoker, err := authn.NewGIPRevoker(ctx, cfg.GIPProjectID, cfg.IsDev())
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

	// gin.Recovery() does not route through slog — gin builds its own
	// log.New(gin.DefaultErrorWriter, ...) rather than using log.Default(),
	// so slog.SetDefault's redirection of the standard library's log package
	// never reaches it, and a handler panic writes straight to os.Stderr with
	// whatever identifier triggered it still in the clear. This package's
	// thesis is that everything the process emits is screened, so this is
	// the one place gin's own writer has to be wrapped explicitly to keep
	// that true.
	gin.DefaultErrorWriter = logging.NewRedactingWriter(gin.DefaultErrorWriter)

	srv := httpserver.New(
		[]httpserver.ReadyCheck{
			{Name: "postgres", Check: db.PingContext},
			{Name: "nats", Check: bus.Ping},
			{Name: "openfga", Check: fga.Ping},
		},
		requestid.Middleware(),
	)
	deps := platform.Deps{
		DB:           db,
		Bus:          bus,
		Authz:        fga,
		Roles:        fga,
		Tokens:       minter,
		TokenRevoker: revoker,
		Reconcile: func(ctx context.Context, tenantID string) error {
			return platform.ReconcileTenant(ctx, registry, fga, tenantID)
		},
	}

	// The chain itself lives in bootstrap.V1Chain, not inline here, so
	// internal/archtest can build its harness from the SAME function that
	// serves production traffic. Inline, the limiter could be deleted from
	// this list and every test would still pass, because the placement
	// test built its own equivalent chain. See V1Chain's comment for the
	// order and why it is load-bearing;
	// internal/archtest.TestThrottledRequestMakesNoOpenFGACall pins it.
	limiter := ratelimit.NewMemory(10_000)
	api := platform.NewRouter(
		srv.Engine.Group("/v1", bootstrap.V1Chain(verifier, revocationChecker, limiter, cfg, fga)...),
		fga,
	)
	for _, m := range registry.All() {
		m.Routes(api, deps)
		if err := bus.StartConsumers(ctx, db, m.Consumers(deps)); err != nil {
			return err
		}
		if err := bus.StartBroadcasts(ctx, m.Broadcasts(deps)); err != nil {
			return err
		}
	}
	go bus.RunDispatcher(ctx, db)
	// A separate loop and ticker from the dispatcher's 500ms tick (#835
	// Task 2, spec D3): pruning belongs on its own hourly cadence, not
	// coupled to publish throughput.
	go bus.RunPruner(ctx, db)

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
