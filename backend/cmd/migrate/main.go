// Command migrate applies every module's migrations and exits. It exists
// so that seeding, CI and a first-time clone do not need to boot the full
// API (and therefore NATS and OpenFGA) just to create tables — it depends
// only on Postgres.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/pkg/logging"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migrate failed", "err", err)
		os.Exit(1)
	}
	slog.Info("migrations applied")
}

func run() error {
	cfg := config.Load()
	slog.SetDefault(logging.New(cfg.LogLevel))
	ctx := context.Background()

	db, err := tenantdb.Open(cfg.AppDatabaseURL, cfg.AdminDatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	// nil: migrate only walks Migrations() and never serves a request, so
	// it never needs the shared revocation-cache instance iam.New wires
	// into the module for the sign-out/revoke handlers (#781).
	registry, err := bootstrap.NewRegistry(nil, nil)
	if err != nil {
		return fmt.Errorf("build registry: %w", err)
	}

	migs := bootstrap.PlatformMigrations()
	for _, m := range registry.All() {
		migs = append(migs, m.Migrations()...)
	}
	if err := db.Migrate(ctx, migs); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	bad, err := db.LintRLS(ctx)
	if err != nil {
		return fmt.Errorf("lint RLS: %w", err)
	}
	if len(bad) > 0 {
		return fmt.Errorf("tables missing forced RLS: %v", bad)
	}
	return nil
}
