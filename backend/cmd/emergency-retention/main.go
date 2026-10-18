// Command emergency-retention purges bounded batches of expired caller envelopes.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/internal/modules/emergency"
	"github.com/tesserix/helivanta/pkg/logging"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error("emergency contact retention failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("emergency-retention", flag.ContinueOnError)
	batch := flags.Int("batch-size", 200, "contacts per transaction (1..1000)")
	batches := flags.Int("max-batches", 10, "maximum transactions (1..100)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *batch < 1 || *batch > 1000 || *batches < 1 || *batches > 100 || flags.NArg() != 0 {
		return fmt.Errorf("batch-size must be 1..1000 and max-batches 1..100, without positional arguments")
	}
	cfg := config.Load()
	slog.SetDefault(logging.New(cfg.LogLevel))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	db, err := tenantdb.OpenWithSystem(cfg.AppDatabaseURL, cfg.AppDatabaseURL, cfg.SystemDatabaseURL)
	if err != nil {
		return fmt.Errorf("open retention database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("close retention database", "err", err)
		}
	}()
	total := 0
	for range *batches {
		n, err := emergency.PurgeExpiredContacts(ctx, db, *batch)
		if err != nil {
			return err
		}
		total += n
		if n < *batch {
			slog.Info("emergency contact retention complete", "purged", total)
			return nil
		}
	}
	slog.Warn("emergency contact retention batch ceiling reached; backlog may remain", "purged", total)
	return nil
}
