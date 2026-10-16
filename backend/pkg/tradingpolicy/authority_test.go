package tradingpolicy_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workspace"
	workspacecontract "github.com/tesserix/helivanta/internal/modules/workspace/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/tradingpolicy"
	"gorm.io/gorm"
)

func TestAuthorityKeepsReviewedVersionsHistoryAndRevocationAtomic(t *testing.T) {
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{workspace.New()}})
	p := approvedPolicy("PK")
	scopeID := uuid.Must(uuid.NewV7()).String()
	in := tradingpolicy.Submission{ID: p.ID, ScopeID: scopeID, Country: p.Scope.Country, Jurisdiction: p.Scope.Jurisdiction, Category: p.Scope.Category, Operation: p.Scope.Operation, Currency: p.Scope.Currency, EffectiveFrom: p.EffectiveFrom, EffectiveUntil: p.EffectiveUntil, Sources: p.Sources, Requirements: p.Requirements}
	apply := func(actor string, fn func(*gorm.DB) error) error {
		return db.WithTenant(tenantdb.WithActor(ctx, actor), p.Scope.Tenant, fn)
	}
	submit := func(input tradingpolicy.Submission) (tradingpolicy.Receipt, error) {
		var receipt tradingpolicy.Receipt
		err := apply(p.Author, func(tx *gorm.DB) error {
			var err error
			receipt, err = tradingpolicy.Submit(tx, p.Scope.Tenant, p.Author, input, testNow)
			return err
		})
		return receipt, err
	}
	first, err := submit(in)
	require.NoError(t, err)
	require.Equal(t, int64(1), first.ResultRevision)
	replayed, err := submit(in)
	require.NoError(t, err)
	require.Equal(t, first, replayed)
	changed := in
	changed.Jurisdiction = "OTHER-TEST-JURISDICTION"
	_, err = submit(changed)
	require.ErrorIs(t, err, tradingpolicy.ErrConflict)
	changed = in
	changed.ID = uuid.Must(uuid.NewV7()).String()
	_, err = submit(changed)
	require.ErrorIs(t, err, tradingpolicy.ErrConflict, "a new request must use the current revision")
	require.ErrorIs(t, apply("TEST-ungranted", func(tx *gorm.DB) error {
		_, err := tradingpolicy.ReadScope(tx, p.Scope.Tenant, "TEST-ungranted", scopeID)
		return err
	}), gorm.ErrRecordNotFound)
	require.NoError(t, apply(p.Author, func(tx *gorm.DB) error {
		rows, err := tradingpolicy.ListScopes(tx, p.Scope.Tenant, p.Author, pagination.Params{Limit: 1})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		encoded, err := json.Marshal(rows)
		require.NoError(t, err)
		require.Contains(t, string(encoded), `"access_role":"owner"`)
		at, id := rows[0].PageKey()
		require.False(t, at.IsZero())
		require.Equal(t, scopeID, id.String())
		next, err := tradingpolicy.ListScopes(tx, p.Scope.Tenant, p.Author, pagination.Params{Limit: 1, After: &pagination.Cursor{CreatedAt: at, ID: id}})
		require.NoError(t, err)
		require.Empty(t, next)
		version, err := tradingpolicy.ReadVersion(tx, p.Scope.Tenant, p.Author, scopeID, in.ID)
		require.NoError(t, err)
		require.Equal(t, "submitted", version.State)
		require.Len(t, version.Requirements, 11)
		return nil
	}))
	grant := tradingpolicy.AccessChange{ID: uuid.Must(uuid.NewV7()).String(), ExpectedRevision: 1, Subject: p.ApprovedBy, Role: "reviewer"}
	require.ErrorIs(t, apply(p.ApprovedBy, func(tx *gorm.DB) error {
		_, err := tradingpolicy.ChangeAccess(tx, p.Scope.Tenant, p.ApprovedBy, scopeID, grant, testNow)
		return err
	}), gorm.ErrRecordNotFound)
	require.NoError(t, apply(p.Author, func(tx *gorm.DB) error {
		first, err := tradingpolicy.ChangeAccess(tx, p.Scope.Tenant, p.Author, scopeID, grant, testNow)
		require.NoError(t, err)
		replay, err := tradingpolicy.ChangeAccess(tx, p.Scope.Tenant, p.Author, scopeID, grant, testNow)
		require.NoError(t, err)
		require.Equal(t, first, replay)
		return nil
	}))
	review := tradingpolicy.Review{ID: uuid.Must(uuid.NewV7()).String(), VersionID: in.ID, ExpectedRevision: 2, Action: "approved", Reason: "TEST ONLY independent review"}
	require.ErrorIs(t, apply(p.Author, func(tx *gorm.DB) error {
		_, err := tradingpolicy.Decide(tx, p.Scope.Tenant, p.Author, scopeID, review, testNow)
		return err
	}), tradingpolicy.ErrIndependentReview)
	require.NoError(t, apply(p.ApprovedBy, func(tx *gorm.DB) error {
		first, err := tradingpolicy.Decide(tx, p.Scope.Tenant, p.ApprovedBy, scopeID, review, testNow)
		require.NoError(t, err)
		replay, err := tradingpolicy.Decide(tx, p.Scope.Tenant, p.ApprovedBy, scopeID, review, testNow)
		require.NoError(t, err)
		require.Equal(t, first, replay)
		return nil
	}))
	require.NoError(t, apply(p.Author, func(tx *gorm.DB) error {
		decision, err := tradingpolicy.Check(tx, p.Scope, testNow)
		require.NoError(t, err)
		require.True(t, decision.Allowed, decision.Reason)
		return nil
	}))
	in.ID = uuid.Must(uuid.NewV7()).String()
	in.ExpectedRevision = 3
	_, err = submit(in)
	require.NoError(t, err)
	review.ID = uuid.Must(uuid.NewV7()).String()
	review.VersionID = in.ID
	review.ExpectedRevision = 4
	require.NoError(t, apply(p.ApprovedBy, func(tx *gorm.DB) error {
		_, err := tradingpolicy.Decide(tx, p.Scope.Tenant, p.ApprovedBy, scopeID, review, testNow)
		return err
	}))
	suspend := tradingpolicy.Review{ID: uuid.Must(uuid.NewV7()).String(), ExpectedRevision: 5, Action: "suspended", Reason: "TEST ONLY safety hold"}
	require.NoError(t, apply(p.ApprovedBy, func(tx *gorm.DB) error {
		_, err := tradingpolicy.Decide(tx, p.Scope.Tenant, p.ApprovedBy, scopeID, suspend, testNow)
		return err
	}))
	suspend.ID = uuid.Must(uuid.NewV7()).String()
	suspend.ExpectedRevision = 6
	require.ErrorIs(t, apply(p.ApprovedBy, func(tx *gorm.DB) error {
		_, err := tradingpolicy.Decide(tx, p.Scope.Tenant, p.ApprovedBy, scopeID, suspend, testNow)
		return err
	}), tradingpolicy.ErrConflict)
	in.ID = uuid.Must(uuid.NewV7()).String()
	in.ExpectedRevision = 6
	_, err = submit(in)
	require.NoError(t, err)
	review.ID = uuid.Must(uuid.NewV7()).String()
	review.VersionID = in.ID
	review.ExpectedRevision = 7
	review.Action = "rejected"
	require.NoError(t, apply(p.ApprovedBy, func(tx *gorm.DB) error {
		_, err := tradingpolicy.Decide(tx, p.Scope.Tenant, p.ApprovedBy, scopeID, review, testNow)
		return err
	}))
	require.NoError(t, apply(p.Author, func(tx *gorm.DB) error {
		decision, err := tradingpolicy.Check(tx, p.Scope, testNow)
		require.NoError(t, err)
		require.False(t, decision.Allowed, "rejecting another version must not reactivate suspended authority")
		versions, err := tradingpolicy.ListVersions(tx, p.Scope.Tenant, p.Author, scopeID, pagination.Params{Limit: 10})
		require.NoError(t, err)
		require.Len(t, versions, 3)
		actions, err := tradingpolicy.ListActions(tx, p.Scope.Tenant, p.Author, scopeID, pagination.Params{Limit: 10})
		require.NoError(t, err)
		require.Len(t, actions, 8)
		at, id := actions[len(actions)-1].PageKey()
		after, err := tradingpolicy.ListActions(tx, p.Scope.Tenant, p.Author, scopeID, pagination.Params{Limit: 10, After: &pagination.Cursor{CreatedAt: at, ID: id}})
		require.NoError(t, err)
		require.Empty(t, after)
		at, id = versions[len(versions)-1].PageKey()
		afterVersions, err := tradingpolicy.ListVersions(tx, p.Scope.Tenant, p.Author, scopeID, pagination.Params{Limit: 10, After: &pagination.Cursor{CreatedAt: at, ID: id}})
		require.NoError(t, err)
		require.Empty(t, afterVersions)
		var audits, outbox int64
		require.NoError(t, tx.Table("audit_events").Where("resource_type=?", "workspace_country_policy_actions").Count(&audits).Error)
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workspacecontract.SubjectCountryPolicyChanged).Count(&outbox).Error)
		require.Equal(t, int64(8), audits)
		require.Equal(t, int64(8), outbox)
		return nil
	}))
	grant.ID = uuid.Must(uuid.NewV7()).String()
	grant.ExpectedRevision = 8
	grant.Revoke = true
	grant.Role = ""
	require.NoError(t, apply(p.Author, func(tx *gorm.DB) error {
		_, err := tradingpolicy.ChangeAccess(tx, p.Scope.Tenant, p.Author, scopeID, grant, testNow.Add(time.Minute))
		return err
	}))
	require.ErrorIs(t, apply(p.ApprovedBy, func(tx *gorm.DB) error {
		_, err := tradingpolicy.ReadScope(tx, p.Scope.Tenant, p.ApprovedBy, scopeID)
		return err
	}), gorm.ErrRecordNotFound)
}
