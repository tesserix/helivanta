package main

import (
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/pkg/organizationidentity"
)

func newOrganizationIdentity(cfg config.Config) (*organizationidentity.Client, error) {
	values := []string{cfg.OrganizationIdentityURL, cfg.OrganizationIdentityProductKey, cfg.OrganizationIdentityTokenProjectID, cfg.OrganizationIdentityKeyFile}
	configured := 0
	for _, value := range values {
		if value != "" {
			configured++
		}
	}
	if configured == 0 {
		return nil, nil
	}
	if configured != len(values) {
		return nil, errors.New("organisation identity configuration incomplete")
	}
	file, err := os.Open(cfg.OrganizationIdentityKeyFile)
	if err != nil {
		return nil, errors.New("organisation identity credential unavailable")
	}
	const limit = 16 << 10
	raw, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(raw) > limit {
		return nil, errors.New("organisation identity credential unavailable")
	}
	tokens, err := organizationidentity.NewKeyTokens(raw, cfg.ZitadelIssuerURL, cfg.OrganizationIdentityTokenProjectID, http.DefaultClient)
	if err != nil {
		return nil, errors.New("organisation identity credential configuration invalid")
	}
	return organizationidentity.New(cfg.OrganizationIdentityURL, cfg.OrganizationIdentityProductKey, http.DefaultClient, tokens)
}
