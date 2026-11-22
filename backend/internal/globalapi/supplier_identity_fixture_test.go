package globalapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/organizationidentity"
)

type supplierIdentityToken struct{}

func (supplierIdentityToken) Token(context.Context) (string, error) {
	return "TEST-PRODUCT-IDENTITY", nil
}

type supplierIdentityProtocol struct{}

func (supplierIdentityProtocol) RoundTrip(request *http.Request) (*http.Response, error) {
	response := httptest.NewRecorder()
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if request.Method != http.MethodGet || len(parts) != 4 || parts[0] != "v1" || parts[1] != "orgs" || parts[3] != "identity" || request.Header.Get("Authorization") != "Bearer TEST-PRODUCT-IDENTITY" || request.Header.Get("Cache-Control") != "no-cache" || request.URL.RawQuery != "" {
		http.Error(response, "unexpected synthetic identity request", http.StatusBadRequest)
		return response.Result(), nil
	}
	tenant, err := uuid.Parse(parts[2])
	if err != nil {
		http.Error(response, "invalid tenant", http.StatusBadRequest)
		return response.Result(), nil
	}
	proof := organizationidentity.Proof{SchemaVersion: 1, TenantID: tenant, ProductKey: "helivanta", OrganizationID: "TEST-org-" + tenant.String(), ProjectID: "TEST-project", GrantID: "TEST-grant-" + tenant.String(), State: "active", VerifiedAt: time.Now().UTC(), SourceRevisions: organizationidentity.Revisions{Organization: "1", Metadata: "2", Project: "3", Grant: "4"}}
	response.Header().Set("Content-Type", "application/json")
	if err = json.NewEncoder(response).Encode(proof); err != nil {
		return nil, err
	}
	return response.Result(), nil
}

func supplierIdentityForTest(t *testing.T) *organizationidentity.Client {
	t.Helper()
	identity, err := organizationidentity.New("https://identity.example.invalid", "helivanta", &http.Client{Transport: supplierIdentityProtocol{}}, supplierIdentityToken{})
	require.NoError(t, err)
	return identity
}
