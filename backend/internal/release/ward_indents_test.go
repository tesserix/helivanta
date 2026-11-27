package release

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWardIndentRoutesHaveTheirOwnReleaseBoundary(t *testing.T) {
	for _, tc := range []struct{ method, path, capability string }{
		{"GET", "/v1/iam/me/hospital-ward-indent-scopes", "pharmacy_ward_indents"},
		{"GET", "/v1/iam/pharmacy-ward-indent-assignment-proposals", "pharmacy_ward_indents"},
		{"GET", "/v1/iam/me/hospital-pharmacy-stocktake-scopes", "baseline"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			got, known := RouteCapability(tc.method, tc.path)
			require.True(t, known, "the admission classifier must recognize the route")
			require.Equal(t, tc.capability, got)
		})
	}
}

func TestWardIndentsStayLockedWhileCompiledIneligible(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			now := time.Now().UTC()
			payload := fmt.Sprintf(`{"features":{
				"helivanta.release.team_accounts":{"defaultValue":true},
				"helivanta.release.staff_core":{"defaultValue":true},
				"helivanta.release.pharmacy_setup":{"defaultValue":true},
				"helivanta.release.pharmacy_staff":{"defaultValue":true},
				"helivanta.release.pharmacy_catalogue":{"defaultValue":true},
				"helivanta.release.pharmacy_low_stock":{"defaultValue":true},
				"helivanta.release.pharmacy_stock":{"defaultValue":true},
				"helivanta.release.pharmacy_prescriptions":{"defaultValue":true},
				"helivanta.release.pharmacy_returns":{"defaultValue":true},
				"helivanta.release.pharmacy_reports":{"defaultValue":true},
				"helivanta.release.pharmacy_counter":{"defaultValue":true},
				"helivanta.release.pharmacy_ward_indents":{"defaultValue":%t}
			}}`, enabled)
			service := testService(t, &payload, &now)
			require.NoError(t, service.Refresh(t.Context()))
			tenant := uuid.MustParse("018f8f5b-1111-7111-8111-111111111111")
			snapshot, err := service.Snapshot(t.Context(), tenant, "TEST-owner")
			require.NoError(t, err)
			require.False(t, available(t, snapshot, "pharmacy_ward_indents"), "a provider value cannot release an unreviewed capability")
			assigned, err := service.AssignmentEnabled(t.Context(), tenant, "TEST-owner", "pharmacy_ward_indent")
			require.NoError(t, err)
			require.False(t, assigned, "ward indent assignments stay locked with the capability")
		})
	}
}
