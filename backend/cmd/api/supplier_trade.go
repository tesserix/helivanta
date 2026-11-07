package main

import (
	"context"
	"errors"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/workloadidentity"
	"k8s.io/client-go/rest"
	"os"
	"path/filepath"
	"strings"
)

func supplierTradeSourceIdentity() (*suppliertrade.CurrentSourceClient, *workloadidentity.Verifier, error) {
	endpoint := os.Getenv("SUPPLIER_TRADE_CURRENT_URL")
	path := os.Getenv("SUPPLIER_TRADE_TOKEN_FILE")
	account := os.Getenv("SUPPLIER_TRADE_CALLER_ACCOUNT")
	if endpoint == "" && path == "" && account == "" {
		return nil, nil, nil
	}
	if endpoint == "" || path == "" || account == "" || !filepath.IsAbs(path) || !strings.HasPrefix(filepath.Clean(path), "/var/run/secrets/") {
		return nil, nil, errors.New("supplier source checks require a configured peer, projected token and exact caller account")
	}
	configuration, err := rest.InClusterConfig()
	if err != nil {
		return nil, nil, errors.New("supplier source checks require Kubernetes workload identity")
	}
	verifier, err := workloadidentity.New(configuration, suppliertrade.SourceCheckAudience, account)
	if err != nil {
		return nil, nil, err
	}
	client, err := suppliertrade.NewCurrentSourceClient(endpoint, nil, func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", suppliertrade.ErrUnavailable
		}
		if len(raw) > 8192 {
			return "", suppliertrade.ErrUnavailable
		}
		return strings.TrimSpace(string(raw)), nil
	})
	if err != nil {
		return nil, nil, err
	}
	return client, verifier, nil
}
