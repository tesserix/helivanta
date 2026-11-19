package organizationidentity_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/helivanta/pkg/organizationidentity"
	"go.yaml.in/yaml/v3"
)

type fixtureTokenTransport func(*http.Request) (*http.Response, error)

func (f fixtureTokenTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNativeFixtureIssuerLifetimesAllowBoundedProductCredentials(t *testing.T) {
	raw, err := os.ReadFile("../../../docker-compose.dev.yml")
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatal(err)
	}
	for _, setting := range []string{
		"ZITADEL_OIDC_DEFAULTACCESSTOKENLIFETIME",
		"ZITADEL_DEFAULTINSTANCE_OIDCSETTINGS_ACCESSTOKENLIFETIME",
	} {
		t.Run(setting, func(t *testing.T) {
			// Zitadel v4.19.5 cmd/defaults.yaml supplies 12h at both configuration layers.
			lifetime := 12 * time.Hour
			if configured := compose.Services["zitadel"].Environment[setting]; configured != "" {
				lifetime, err = time.ParseDuration(configured)
				if err != nil {
					t.Fatal(err)
				}
			}
			client := &http.Client{Transport: fixtureTokenTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPost || r.URL.String() != "https://fixture.example/oauth/v2/token" {
					t.Fatal("credential exchange escaped the fixture issuer")
				}
				body := fmt.Sprintf(`{"access_token":"TEST-fixture-access","token_type":"Bearer","expires_in":%d}`, int64(lifetime/time.Second))
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			tokens, err := organizationidentity.NewClientTokens("TEST-machine", []byte("TEST-secret"), "https://fixture.example", "TEST-project", client)
			if err != nil {
				t.Fatal(err)
			}
			token, err := tokens.Token(t.Context())
			if err != nil || token != "TEST-fixture-access" {
				t.Fatalf("native fixture %s=%s cannot satisfy bounded product credentials: %v", setting, lifetime, err)
			}
		})
	}
}
