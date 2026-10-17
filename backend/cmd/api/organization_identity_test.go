package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tesserix/helivanta/internal/config"
)

func TestOrganisationIdentityConfigurationDoesNotReuseTheLoginCredential(t *testing.T) {
	client, err := newOrganizationIdentity(config.Config{ZitadelLoginClientToken: "PRIVATE-login-credential"})
	if err != nil || client != nil {
		t.Fatal("unconfigured organisation identity must remain unavailable without borrowing the login credential")
	}
	_, err = newOrganizationIdentity(config.Config{OrganizationIdentityURL: "https://onboard.tesserix.app"})
	if err == nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("partial organisation identity configuration did not fail safely")
	}
}

func TestOrganisationIdentityLoadsOnlyTheDedicatedBoundedCredential(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{"type": "serviceaccount", "keyId": "TEST-key", "userId": "TEST-product-machine", "key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "machine-key.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{OrganizationIdentityURL: "https://onboard.tesserix.app", OrganizationIdentityProductKey: "TEST-product", OrganizationIdentityTokenProjectID: "TEST-onboarding-project", OrganizationIdentityKeyFile: file, ZitadelIssuerURL: "https://auth.tesserix.app"}
	client, err := newOrganizationIdentity(cfg)
	if err != nil || client == nil {
		t.Fatal("complete dedicated configuration did not construct the identity client")
	}
	for _, contents := range []string{"PRIVATE-invalid-machine-key", strings.Repeat("PRIVATE", 3000)} {
		if err := os.WriteFile(file, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		client, err := newOrganizationIdentity(cfg)
		if err == nil || client != nil || strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), file) {
			t.Fatal("unsafe credential file was accepted or included in startup diagnostics")
		}
	}
	cfg.OrganizationIdentityKeyFile = filepath.Join(t.TempDir(), "PRIVATE-missing-file")
	client, err = newOrganizationIdentity(cfg)
	if err == nil || client != nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("missing credential file did not fail safely")
	}
}
