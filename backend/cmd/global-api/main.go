package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/pkg/logging"
)

func main() {
	slog.SetDefault(logging.New("info"))
	if err := run(); err != nil {
		slog.Error("global API stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := globalapi.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	handler, err := globalapi.New(cfg)
	if err != nil {
		return err
	}
	port := os.Getenv("GLOBAL_API_PORT")
	if port == "" {
		port = "8081"
	}
	server := &http.Server{Addr: ":" + port, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	slog.Info("global API started", "port", port)
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-result; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
