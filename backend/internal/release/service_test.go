package release

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type sdkTransport func(*http.Request) (*http.Response, error)

func (f sdkTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testService(t *testing.T, payload *string, now *time.Time) *Service {
	t.Helper()
	service, err := New(Config{
		URL: "https://growthbook.test", ClientKey: "sdk-test",
		Now: func() time.Time { return *now },
		Client: &http.Client{Transport: sdkTransport(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, "/api/features/sdk-test", r.URL.Path)
			require.Empty(t, r.URL.RawQuery)
			require.Empty(t, r.Header.Get("Authorization"))
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(*payload)), Header: http.Header{}}, nil
		})},
	})
	require.NoError(t, err)
	return service
}

func available(t *testing.T, snapshot ReleaseSnapshot, key string) bool {
	t.Helper()
	for _, capability := range snapshot.Capabilities {
		if capability.Key == key {
			return capability.Available
		}
	}
	t.Fatalf("capability missing: %s", key)
	return false
}

func TestTenantTargetingAndDependencyClosureUseTheActualGrowthBookEvaluator(t *testing.T) {
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	tenant := uuid.MustParse("018f8f5b-1111-7111-8111-111111111111")
	payload := `{"features":{
		"helivanta.release.team_accounts":{"defaultValue":false,"rules":[{"condition":{"tenant_id":"` + tenant.String() + `"},"force":true}]},
		"helivanta.release.staff_core":{"defaultValue":true},
		"helivanta.release.department_teams":{"defaultValue":true},
		"helivanta.release.pharmacy_counter":{"defaultValue":true}
	}}`
	service := testService(t, &payload, &now)
	require.NoError(t, service.Refresh(t.Context()))
	first, err := service.Snapshot(t.Context(), tenant, "owner")
	require.NoError(t, err)
	require.Equal(t, tenant, first.TenantID)
	require.Equal(t, "owner", first.Subject)
	require.True(t, available(t, first, "team_accounts"))
	require.True(t, available(t, first, "staff_core"))
	require.True(t, available(t, first, "department_teams"))
	require.False(t, available(t, first, "pharmacy_counter"), "remote configuration cannot release an ineligible group")
	other, err := service.Snapshot(t.Context(), uuid.MustParse("018f8f5b-2222-7222-8222-222222222222"), "another-owner")
	require.NoError(t, err)
	require.False(t, available(t, other, "team_accounts"))
	require.False(t, available(t, other, "staff_core"), "a forced child cannot bypass its disabled dependency")
	require.False(t, available(t, other, "department_teams"))
}

func TestUnloadedInvalidAndExpiredSnapshotsCannotEnableNewWork(t *testing.T) {
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	payload := `{"features":{"helivanta.release.team_accounts":{"defaultValue":true}}}`
	service := testService(t, &payload, &now)
	_, err := service.Snapshot(t.Context(), uuid.MustParse("018f8f5b-2222-7222-8222-222222222222"), "owner")
	require.ErrorIs(t, err, ErrUnavailable)
	require.NoError(t, service.Refresh(t.Context()))
	now = now.Add(59 * time.Second)
	snapshot, err := service.Snapshot(t.Context(), uuid.MustParse("018f8f5b-2222-7222-8222-222222222222"), "owner")
	require.NoError(t, err)
	require.True(t, available(t, snapshot, "team_accounts"))
	payload = `{"features":{"helivanta.release.team_accounts":{"defaultValue":"true"}}}`
	require.ErrorIs(t, service.Refresh(t.Context()), ErrUnavailable)
	now = now.Add(time.Second)
	_, err = service.Snapshot(t.Context(), uuid.MustParse("018f8f5b-2222-7222-8222-222222222222"), "owner")
	require.ErrorIs(t, err, ErrUnavailable, "failed refresh cannot extend an enabled snapshot")
}

func TestForeignProjectRulesAndExperimentPayloadsAreRefused(t *testing.T) {
	for _, payload := range []string{
		`{"features":{"another-product.flag":{"defaultValue":true}}}`,
		`{"features":{"helivanta.release.team_accounts":{"defaultValue":true,"rules":[{"variations":[true,false]}]}}}`,
		`{"features":{"helivanta.release.team_accounts":{"defaultValue":true,"rules":[{"force":"true"}]}}}`,
		`{"features":null}`,
		`{"features":{}} trailing`,
		`{"features":{},"encryptedFeatures":"unreadable"}`,
		`{"features":{},"features":{"helivanta.release.team_accounts":{"defaultValue":true}}}`,
		`{"features":{"helivanta.release.team_accounts":{"defaultValue":false,"defaultValue":true}}}`,
	} {
		t.Run(payload, func(t *testing.T) {
			now := time.Now().UTC()
			service := testService(t, &payload, &now)
			require.ErrorIs(t, service.Refresh(t.Context()), ErrUnavailable)
			_, err := service.Snapshot(t.Context(), uuid.MustParse("018f8f5b-2222-7222-8222-222222222222"), "owner")
			require.ErrorIs(t, err, ErrUnavailable)
		})
	}
}

func TestMissingFlagsAreOffAndUnknownRoutesNeverInheritBaseline(t *testing.T) {
	now := time.Now().UTC()
	payload := `{"features":{}}`
	service := testService(t, &payload, &now)
	require.NoError(t, service.Refresh(t.Context()))
	snapshot, err := service.Snapshot(t.Context(), uuid.MustParse("018f8f5b-2222-7222-8222-222222222222"), "owner")
	require.NoError(t, err)
	for _, capability := range snapshot.Capabilities {
		require.False(t, capability.Available)
	}
	_, ok := RouteCapability("POST", "/v1/new-untested-command")
	require.False(t, ok)
	key, ok := RouteCapability("POST", "/v1/auth/login")
	require.True(t, ok)
	require.Equal(t, "baseline", key)
}

func TestPublicAvailabilityDoesNotExposeTenantRulesOrAccountDetails(t *testing.T) {
	now := time.Now().UTC()
	payload := `{"features":{"helivanta.release.onboarding":{"defaultValue":true}}}`
	service := testService(t, &payload, &now)
	require.NoError(t, service.Refresh(t.Context()))
	public, err := service.PublicSnapshot(t.Context())
	require.NoError(t, err)
	require.True(t, public.Onboarding)
	require.Equal(t, now, public.IssuedAt)
	require.Equal(t, now.Add(time.Minute), public.ExpiresAt)
}

func TestCancelledRefreshCannotPublishAnEnabledSnapshot(t *testing.T) {
	now := time.Now().UTC()
	payload := `{"features":{"helivanta.release.team_accounts":{"defaultValue":true}}}`
	service := testService(t, &payload, &now)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, service.Refresh(ctx), ErrUnavailable)
	_, err := service.Snapshot(t.Context(), uuid.MustParse("018f8f5b-2222-7222-8222-222222222222"), "owner")
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestDedicatedHelivantaProviderUsesOnlyItsDeclaredInternalOrigin(t *testing.T) {
	_, err := New(Config{URL: "http://helivanta-growthbook.helivanta-growthbook.svc.cluster.local:3100", ClientKey: "sdk-synthetic"})
	require.NoError(t, err)
	for _, origin := range []string{"http://127.0.0.1:3100", "http://helivanta-growthbook.evil.svc.cluster.local:3100", "http://helivanta-growthbook.helivanta-growthbook.svc.cluster.local:3000"} {
		_, err = New(Config{URL: origin, ClientKey: "sdk-synthetic"})
		require.Error(t, err)
	}
}

func TestAssignmentFamiliesRequireTheirReleasedDependencies(t *testing.T) {
	tenant := uuid.MustParse("018f8f5b-1111-7111-8111-111111111111")
	for _, test := range []struct {
		name, family, payload string
		want                  bool
		unavailable           bool
	}{
		{name: "core identity", family: "identity", payload: `{"features":{"helivanta.release.team_accounts":{"defaultValue":true},"helivanta.release.staff_core":{"defaultValue":true}}}`, want: true},
		{name: "missing dependency", family: "identity", payload: `{"features":{"helivanta.release.staff_core":{"defaultValue":true}}}`},
		{name: "off", family: "posting", payload: `{"features":{}}`},
		{name: "unreleased supplier", family: "supplier_access", payload: `{"features":{"helivanta.release.supplier_collaboration":{"defaultValue":true}}}`},
		{name: "unknown family", family: "invented", payload: `{"features":{}}`, unavailable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			payload := test.payload
			service := testService(t, &payload, &now)
			require.NoError(t, service.Refresh(t.Context()))
			enabled, err := service.AssignmentEnabled(t.Context(), tenant, "owner", test.family)
			if test.unavailable {
				require.ErrorIs(t, err, ErrUnavailable)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.want, enabled)
		})
	}
}
