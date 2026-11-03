package globalapi

import (
	"context"
	"gorm.io/gorm"
	"maps"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func assertPostingSourceEvidenceIsCommandBound(t *testing.T, guard authz.CurrentStaffActorGuard, scope authz.StaffActorScope, use func(func(context.Context, authz.StaffActorReference) error) error) {
	t.Helper()
	appDSN, adminDSN, _ := testinfra.StartPostgres(t)
	foreignDB, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, foreignDB.Close()) })
	foreignGuard := workforce.New().StaffActors(foreignDB)
	var escaped context.Context
	require.NoError(t, use(func(commandCtx context.Context, reference authz.StaffActorReference) error {
		escaped = commandCtx
		require.NotEqual(t, uuid.Nil, reference.GenerationID)
		for _, change := range []struct {
			name  string
			scope authz.StaffActorScope
		}{
			{"foreign tenant", authz.StaffActorScope{TenantID: testutil.TenantB, Subject: scope.Subject, HospitalID: scope.HospitalID}},
			{"different subject", authz.StaffActorScope{TenantID: scope.TenantID, Subject: "synthetic-other-source-actor", HospitalID: scope.HospitalID}},
			{"different hospital", authz.StaffActorScope{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: uuid.New()}},
		} {
			t.Run(change.name, func(t *testing.T) {
				called := false
				err := guard.WithCurrentStaffActor(commandCtx, change.scope, func(context.Context, authz.StaffActorReference) error { called = true; return nil })
				require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
				require.False(t, called, "one checked scope is not authority for another")
			})
		}
		called := false
		require.ErrorIs(t, foreignGuard.WithCurrentStaffActor(commandCtx, scope, func(context.Context, authz.StaffActorReference) error { called = true; return nil }), authz.ErrScopedAuthorizationUnavailable)
		require.False(t, called, "a command on one owner database cannot authorize another database")
		return nil
	}))
	require.NotNil(t, escaped)
	called := false
	require.ErrorIs(t, guard.WithCurrentStaffActor(escaped, scope, func(context.Context, authz.StaffActorReference) error { called = true; return nil }), authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called, "capturing a completed command context must not retain authorization")
}

func assertPostingConcurrentCommandsRetainOneRevision(t *testing.T, db *tenantdb.DB, hospital, staffBase, subject string, request func(string, string, string, []byte, string) *httptest.ResponseRecorder, input map[string]any) {
	t.Helper()
	for _, conflicting := range []bool{false, true} {
		name := "exact concurrent retry"
		if conflicting {
			name = "conflicting concurrent retry"
		}
		t.Run(name, func(t *testing.T) {
			mutationAudits := func() int64 {
				t.Helper()
				var audits int64
				require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
					return tx.Table("workforce_audit").Where("action='workforce.posting_draft.saved'").Count(&audits).Error
				}))
				return audits
			}
			before := mutationAudits()
			postingID, commandID := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
			path := staffBase + "/posting-drafts/" + postingID + "/commands/" + commandID
			secondInput := maps.Clone(input)
			if conflicting {
				secondInput["reason"] = "Synthetic conflicting command intent"
			}
			first, second := startSerializedPostingRequests(t, db, hospital, request,
				postingRaceRequest{"PUT", path, subject, input},
				postingRaceRequest{"PUT", path, subject, secondInput})
			receipt := finishActivationRequest(t, first, 200)
			require.EqualValues(t, 1, receipt["version"])
			require.Equal(t, false, receipt["grants_access"])
			if conflicting {
				denied := finishActivationRequest(t, second, 409)
				require.NotContains(t, denied, "id")
			} else {
				require.Equal(t, receipt, finishActivationRequest(t, second, 200))
			}
			require.Equal(t, receipt, finishActivationRequest(t, startActivationRequest(t, request, "GET", path, subject, nil), 200))
			history := finishActivationRequest(t, startActivationRequest(t, request, "GET", staffBase+"/posting-drafts/"+postingID+"/history", subject, nil), 200)
			require.Len(t, history["data"], 1, "concurrent commands retain one immutable revision")
			require.EqualValues(t, before+1, mutationAudits(), "a command retry cannot create a second business mutation audit")
		})
	}
}

type postingRaceRequest struct {
	method, path, subject string
	input                 any
}

func startSerializedPostingRequests(t *testing.T, db *tenantdb.DB, hospital string, request func(string, string, string, []byte, string) *httptest.ResponseRecorder, firstInput, secondInput postingRaceRequest) (<-chan *httptest.ResponseRecorder, <-chan *httptest.ResponseRecorder) {
	t.Helper()
	blocker, unlock := holdActivationSupervision(t, db, hospital)
	first := startActivationRequest(t, request, firstInput.method, firstInput.path, firstInput.subject, firstInput.input)
	firstPID := awaitActivationBlock(t, db, blocker, "workforce_lock_supervision")
	second := startActivationRequest(t, request, secondInput.method, secondInput.path, secondInput.subject, secondInput.input)
	require.Eventually(t, func() bool {
		var peer int
		require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error {
			return tx.Raw(`SELECT pid FROM pg_stat_activity WHERE pid<>? AND ?=ANY(pg_blocking_pids(pid)) AND query LIKE '%workforce_lock_supervision%' LIMIT 1`, firstPID, blocker).Scan(&peer).Error
		}))
		return peer != 0
	}, 2*time.Second, 10*time.Millisecond, "both commands must reach the real supervision lock before it is released")
	unlock()
	return first, second
}
