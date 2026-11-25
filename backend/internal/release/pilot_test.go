package release

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/pkg/authn"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPilotCannotReleaseOtherTenantsOrOtherUnreleasedCapabilities(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	tenant := uuid.New()
	entries := make([]string, 0, len(definitions))
	for _, d := range definitions {
		entries = append(entries, fmt.Sprintf(`"helivanta.release.%s":{"defaultValue":true}`, d.Key))
	}
	payload := `{"features":{` + strings.Join(entries, ",") + `}}`
	policy := &PilotConfig{TenantIDs: []uuid.UUID{tenant}, ExpiresAt: now.Add(30 * time.Second)}
	service, err := New(Config{URL: "https://growthbook.test", ClientKey: "sdk-test", Now: func() time.Time { return now }, Pilot: policy,
		Client: &http.Client{Transport: sdkTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload)), Header: http.Header{}}, nil
		})}})
	require.NoError(t, err)
	require.NoError(t, service.Refresh(t.Context()))
	snapshot, err := service.Snapshot(t.Context(), tenant, "owner")
	require.NoError(t, err)
	require.True(t, available(t, snapshot, "pharmacy_counter"))
	require.True(t, available(t, snapshot, "pharmacy_reports"))
	require.False(t, available(t, snapshot, "workforce_payroll"))
	require.Equal(t, policy.ExpiresAt, snapshot.ExpiresAt)
	other, err := service.Snapshot(t.Context(), uuid.New(), "other-owner")
	require.NoError(t, err)
	require.False(t, available(t, other, "pharmacy_counter"))
	require.False(t, available(t, other, "pharmacy_stock"))
	now = policy.ExpiresAt
	expired, err := service.Snapshot(t.Context(), tenant, "owner")
	require.NoError(t, err)
	require.False(t, available(t, expired, "pharmacy_counter"))
	require.True(t, available(t, expired, "pharmacy_setup"))
	now = policy.ExpiresAt.Add(-time.Second)
	service.now = func() time.Time { at := now; now = now.Add(2 * time.Second); return at }
	boundary, err := service.Snapshot(t.Context(), tenant, "owner")
	require.NoError(t, err)
	require.True(t, available(t, boundary, "pharmacy_counter"), "one evaluation uses one eligibility instant")
	require.Equal(t, policy.ExpiresAt, boundary.ExpiresAt, "crossing expiry cannot extend the snapshot")
	afterBoundary, err := service.Snapshot(t.Context(), tenant, "owner")
	require.NoError(t, err)
	require.False(t, available(t, afterBoundary, "pharmacy_counter"))
}

func TestPilotConfigurationIsBoundedCopiedAndDoesNotReplaceProviderApproval(t *testing.T) {
	now := time.Now().UTC()
	tenant := uuid.New()
	for _, policy := range []*PilotConfig{
		{ExpiresAt: now.Add(time.Hour)},
		{TenantIDs: []uuid.UUID{uuid.Nil}, ExpiresAt: now.Add(time.Hour)},
		{TenantIDs: []uuid.UUID{tenant, tenant}, ExpiresAt: now.Add(time.Hour)},
		{TenantIDs: []uuid.UUID{tenant}},
		{TenantIDs: []uuid.UUID{tenant}, ExpiresAt: now.Add(8 * 24 * time.Hour)},
	} {
		_, err := copyPilot(policy, now)
		require.Error(t, err)
	}
	original := &PilotConfig{TenantIDs: []uuid.UUID{tenant}, ExpiresAt: now.Add(time.Hour)}
	copied, err := copyPilot(original, now)
	require.NoError(t, err)
	original.TenantIDs[0] = uuid.New()
	require.Equal(t, tenant, copied.TenantIDs[0])
	payload := `{"features":{"helivanta.release.pharmacy_counter":{"defaultValue":true}}}`
	service := testService(t, &payload, &now)
	service.pilot = copied
	require.NoError(t, service.Refresh(t.Context()))
	snapshot, err := service.Snapshot(t.Context(), tenant, "owner")
	require.NoError(t, err)
	require.False(t, available(t, snapshot, "pharmacy_counter"), "pilot cannot bypass missing provider prerequisites")
	now = now.Add(SnapshotLifetime)
	_, err = service.Snapshot(t.Context(), tenant, "owner")
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestPilotPolicyFileRefusesAmbiguousOrMalformedConfiguration(t *testing.T) {
	for _, value := range []string{
		`null`,
		`{"tenant_ids":[],"tenant_ids":[]}`,
		`{"unexpected":true}`,
		`{"tenant_ids":["not-a-tenant"]}`,
		`{}` + strings.Repeat(" ", 8193),
		`{} {}`,
	} {
		path := t.TempDir() + "/pilot.json"
		require.NoError(t, os.WriteFile(path, []byte(value), 0600))
		policy, err := readPilotFile(path)
		if err == nil {
			_, err = copyPilot(policy, time.Now())
		}
		require.Error(t, err, value)
	}
	policy, err := readPilotFile("")
	require.NoError(t, err)
	require.Nil(t, policy)
	_, err = readPilotFile(t.TempDir() + "/missing")
	require.Error(t, err)
}

func TestPilotAdmissionUsesAuthenticatedTenantNotCallerHeaders(t *testing.T) {
	now := time.Now().UTC()
	pilot := uuid.New()
	other := uuid.New()
	payload := `{"features":{"helivanta.release.team_accounts":{"defaultValue":true},"helivanta.release.staff_core":{"defaultValue":true},"helivanta.release.department_teams":{"defaultValue":true},"helivanta.release.pharmacy_setup":{"defaultValue":true},"helivanta.release.pharmacy_catalogue":{"defaultValue":true}}}`
	service := testService(t, &payload, &now)
	service.pilot = &PilotConfig{TenantIDs: []uuid.UUID{pilot}, ExpiresAt: now.Add(time.Hour)}
	require.NoError(t, service.Refresh(t.Context()))
	const pattern = "GET /v1/drugmaster/search"
	parts := strings.SplitN(pattern, " ", 2)
	capability, ok := RouteCapability(parts[0], parts[1])
	require.True(t, ok)
	require.Equal(t, "pharmacy_catalogue", capability)
	for _, tenant := range []uuid.UUID{pilot, other} {
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		engine.Use(func(c *gin.Context) {
			c.Set("authn.principal", authn.Principal{TenantID: tenant.String(), Subject: "owner"})
			c.Next()
		})
		engine.GET(parts[1], service.Admission(parts[0], parts[1]), func(c *gin.Context) { c.Status(204) })
		req := httptest.NewRequest("GET", "/v1/drugmaster/search", nil)
		req.Header.Set("X-Tenant-ID", pilot.String())
		req.Header.Set("X-Helivanta-Expected-Tenant", pilot.String())
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		if tenant == pilot {
			require.Equal(t, 204, response.Code)
		} else {
			require.Equal(t, 404, response.Code)
		}
	}
}
