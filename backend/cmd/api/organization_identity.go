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
	values := []string{cfg.OrganizationIdentityURL, cfg.OrganizationIdentityProductKey, cfg.OrganizationIdentityTokenProjectID}
	configured := 0
	for _, value := range values {
		if value != "" {
			configured++
		}
	}
	keyConfigured := cfg.OrganizationIdentityKeyFile != ""
	secretConfigured := cfg.OrganizationIdentityClientID != "" && cfg.OrganizationIdentityClientSecretFile != ""
	secretPartial := cfg.OrganizationIdentityClientID != "" || cfg.OrganizationIdentityClientSecretFile != ""
	if configured == 0 && !keyConfigured && !secretPartial {
		return nil, nil
	}
	if configured != len(values) || keyConfigured && secretPartial || !keyConfigured && !secretConfigured {
		return nil, errors.New("organisation identity configuration incomplete or ambiguous")
	}
	credentialFile := cfg.OrganizationIdentityKeyFile
	if secretConfigured {
		credentialFile = cfg.OrganizationIdentityClientSecretFile
	}
	file, err := os.Open(credentialFile) // #nosec G304 -- Startup-only operator configuration, never a request-supplied path.
	if err != nil {
		return nil, errors.New("organisation identity credential unavailable")
	}
	const limit = 16 << 10
	raw, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(raw) > limit {
		return nil, errors.New("organisation identity credential unavailable")
	}
	var tokens organizationidentity.TokenSource
	if secretConfigured {
		tokens, err = organizationidentity.NewClientTokens(cfg.OrganizationIdentityClientID, raw, cfg.ZitadelIssuerURL, cfg.OrganizationIdentityTokenProjectID, http.DefaultClient)
	} else {
		tokens, err = organizationidentity.NewKeyTokens(raw, cfg.ZitadelIssuerURL, cfg.OrganizationIdentityTokenProjectID, http.DefaultClient)
	}
	if err != nil {
		return nil, errors.New("organisation identity credential configuration invalid")
	}
	return organizationidentity.New(cfg.OrganizationIdentityURL, cfg.OrganizationIdentityProductKey, http.DefaultClient, tokens)
}
