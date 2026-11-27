package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/storage"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/internal/httpserver"
	"github.com/tesserix/helivanta/internal/modules/practitioner"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/logging"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if err := run(); err != nil {
		slog.Error("professional evidence worker stopped", "err", err)
		os.Exit(1)
	}
}

func loadWorkerConfig() (config.Config, error) {
	cfg := config.Load()
	cfg.DocumentWorkerDatabaseURL = os.Getenv("PRACTITIONER_EVIDENCE_WORKER_DATABASE_URL")
	if strings.TrimSpace(cfg.DocumentWorkerDatabaseURL) == "" || cfg.RequireDocumentWorker() != nil {
		return config.Config{}, errors.New("professional evidence worker requires independent database credentials, private storage, scanner and Temporal configuration")
	}
	return cfg, nil
}

func run() error {
	cfg, err := loadWorkerConfig()
	if err != nil {
		return err
	}
	slog.SetDefault(logging.New(cfg.LogLevel))
	slog.Info("starting professional evidence worker", "version", version, "commit", commit)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	db, err := tenantdb.OpenRuntime(ctx, cfg.DocumentWorkerDatabaseURL)
	if err != nil {
		return errors.New("private professional evidence database unavailable")
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("professional evidence database close unavailable")
		}
	}()
	cloud, err := storage.NewClient(ctx)
	if err != nil {
		return errors.New("private professional evidence storage identity unavailable")
	}
	defer func() {
		if err := cloud.Close(); err != nil {
			slog.Error("professional evidence storage close unavailable")
		}
	}()
	files, err := attachments.NewGCS(cloud, cfg.DocumentQuarantineBucket, cfg.DocumentReleasedBucket)
	if err != nil {
		return err
	}
	scanner, err := attachments.NewClamAV(cfg.ClamAVAddress, 90*time.Second, attachments.MaxFileBytes)
	if err != nil {
		return err
	}
	processor, err := practitioner.NewEvidenceProcessor(ctx, db, files, scanner)
	if err != nil {
		return err
	}
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, err := client.DialContext(startup, client.Options{HostPort: cfg.TemporalAddress, Namespace: cfg.TemporalNamespace, Logger: slog.Default()})
	if err != nil {
		return errors.New("private professional evidence workflow client unavailable")
	}
	defer c.Close()
	w := worker.New(c, practitioner.EvidenceTaskQueue, worker.Options{MaxConcurrentActivityExecutionSize: 2, WorkerStopTimeout: 90 * time.Second})
	w.RegisterWorkflowWithOptions(practitioner.EvidenceWorkflow, workflow.RegisterOptions{Name: practitioner.EvidenceWorkflowName})
	w.RegisterActivityWithOptions(processor.ProcessActivity, activity.RegisterOptions{Name: practitioner.EvidenceActivityName})
	if err := w.Start(); err != nil {
		return errors.New("private professional evidence workflow worker unavailable")
	}
	defer w.Stop()
	cancel()
	gin.SetMode(gin.ReleaseMode)
	gin.DefaultErrorWriter = logging.NewRedactingWriter(gin.DefaultErrorWriter)
	probes := httpserver.New([]httpserver.ReadyCheck{
		{Name: "postgres", Check: db.PingContext},
		{Name: "temporal", Check: func(ctx context.Context) error {
			_, err := c.CheckHealth(ctx, &client.CheckHealthRequest{})
			return err
		}},
		{Name: "scanner", Check: scanner.Ready},
	}, nil)
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: http.TimeoutHandler(probes.Engine, 5*time.Second, "readiness deadline exceeded"), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err := <-errorsCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("professional evidence worker health server unavailable")
		}
	}
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	return srv.Shutdown(shutdown)
}
