package main

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/internal/httpserver"
	"github.com/tesserix/helivanta/internal/modules/emergency" //nolint:depguard // composition root
	"github.com/tesserix/helivanta/pkg/logging"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"gorm.io/gorm"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil {
		slog.Error("emergency workflow worker stopped", "error", err)
		os.Exit(1)
	}
}
func run(ctx context.Context) error {
	cfg := config.Load()
	if err := cfg.RequireEmergencyWorker(); err != nil {
		return err
	}
	slog.SetDefault(logging.New(cfg.LogLevel))
	slog.Info("emergency workflow worker starting", "version", version, "commit", commit)
	db, err := tenantdb.OpenRuntime(ctx, cfg.EmergencyWorkerDatabaseURL)
	if err != nil {
		return errors.New("emergency workflow database unavailable")
	}
	defer func() { _ = db.Close() }()
	processor, err := emergency.NewDispatchProcessor(ctx, db)
	if err != nil {
		return err
	}
	if err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		pool, err := tx.DB()
		if err != nil {
			return err
		}
		pool.SetMaxOpenConns(4)
		pool.SetMaxIdleConns(2)
		pool.SetConnMaxLifetime(15 * time.Minute)
		return nil
	}); err != nil {
		return errors.New("emergency database pool unavailable")
	}
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, err := client.DialContext(startup, client.Options{HostPort: cfg.TemporalAddress, Namespace: cfg.TemporalNamespace, Logger: slog.Default()})
	if err != nil {
		return errors.New("emergency workflow client unavailable")
	}
	defer c.Close()
	fatal := make(chan error, 1)
	w := worker.New(c, emergency.DispatchTaskQueue, worker.Options{MaxConcurrentActivityExecutionSize: 4, WorkerStopTimeout: 20 * time.Second, OnFatalError: func(error) {
		select {
		case fatal <- errors.New("emergency workflow poller stopped"):
		default:
		}
	}})
	w.RegisterWorkflowWithOptions(emergency.DispatchWorkflow, workflow.RegisterOptions{Name: emergency.DispatchWorkflowName})
	w.RegisterActivityWithOptions(processor.OfferActivity, activity.RegisterOptions{Name: emergency.DispatchOfferActivity})
	w.RegisterActivityWithOptions(processor.RefreshActivity, activity.RegisterOptions{Name: emergency.DispatchRefreshActivity})
	w.RegisterActivityWithOptions(processor.ExpireActivity, activity.RegisterOptions{Name: emergency.DispatchExpireActivity})
	w.RegisterActivityWithOptions(processor.ReceivingExpireActivity, activity.RegisterOptions{Name: emergency.DispatchReceivingExpireActivity})
	if err := w.Start(); err != nil {
		return errors.New("emergency workflow worker unavailable")
	}
	defer w.Stop()
	cancel()
	gin.SetMode(gin.ReleaseMode)
	gin.DefaultErrorWriter = logging.NewRedactingWriter(gin.DefaultErrorWriter)
	probes := httpserver.New([]httpserver.ReadyCheck{{Name: "postgres", Check: db.PingContext}, {Name: "temporal", Check: func(ctx context.Context) error {
		_, err := c.CheckHealth(ctx, &client.CheckHealthRequest{})
		return err
	}}}, nil)
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: http.TimeoutHandler(probes.Engine, 5*time.Second, "readiness deadline exceeded"), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.ListenAndServe() }()
	var failure error
	select {
	case <-ctx.Done():
	case failure = <-fatal:
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			failure = errors.New("emergency worker health server unavailable")
		}
	}
	shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := srv.Shutdown(shutdown); err != nil {
		return errors.New("emergency worker shutdown deadline exceeded")
	}
	return failure
}
