package workspace_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workspace" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/tradingpolicy"
	"gorm.io/gorm"
)

func TestCountryPolicySubmissionIsTenantScopedAndReplaySafe(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"owner": testutil.TenantA, "reader": testutil.TenantA, "other": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"owner": {"workspace.country_policy.manage", "workspace.country_policy.read"},
			"other": {"workspace.country_policy.manage", "workspace.country_policy.read"},
		},
		Modules: []platform.Module{workspace.New()},
	})
	id := uuid.Must(uuid.NewV7()).String()
	scopeID := uuid.Must(uuid.NewV7()).String()
	body, err := json.Marshal(map[string]any{
		"id": id, "scope_id": scopeID, "expected_revision": 0,
		"country": "IN", "jurisdiction": "TEST-PROVINCE", "category": "otc", "operation": "order", "currency": "INR",
		"effective_from": time.Now().UTC().Add(-time.Hour), "effective_until": time.Now().UTC().Add(24 * time.Hour),
		"sources":      []map[string]any{{"key": "TEST-source", "title": "TEST ONLY fictional source", "url": "https://regulator.example.invalid/test", "reviewed_at": time.Now().UTC().Add(-2 * time.Hour)}},
		"requirements": syntheticCountryRequirements(),
	})
	require.NoError(t, err)
	path := "/v1/workspace/country-policies"
	require.Equal(t, 403, testutil.Do(r, "POST", path, "reader", string(body)).Code)
	first := testutil.Do(r, "POST", path, "owner", string(body))
	require.Equal(t, 202, first.Code, first.Body.String())
	replay := testutil.Do(r, "POST", path, "owner", string(body))
	require.Equal(t, 202, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	get := testutil.Do(r, "GET", path+"/"+scopeID, "owner", "")
	require.Equal(t, 200, get.Code, get.Body.String())
	require.Contains(t, get.Body.String(), "TEST-PROVINCE")
	require.Contains(t, get.Body.String(), "unapproved")
	require.Contains(t, get.Body.String(), `"policy_decision":{"allowed":false,"reason":"country_policy_not_approved"`)
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/"+scopeID, "other", "").Code)
	page := testutil.Do(r, "GET", path+"?limit=1", "owner", "")
	require.Equal(t, 200, page.Code, page.Body.String())
	require.Contains(t, page.Body.String(), scopeID)
	require.Contains(t, page.Body.String(), `"has_more":false`)
	otherPage := testutil.Do(r, "GET", path, "other", "")
	require.Equal(t, 200, otherPage.Code, otherPage.Body.String())
	require.Contains(t, otherPage.Body.String(), `"data":[]`)
	for _, suffix := range []string{"/versions", "/actions", "/versions/" + id} {
		denied := testutil.Do(r, "GET", path+"/"+scopeID+suffix, "other", "")
		require.Equal(t, 404, denied.Code, denied.Body.String())
	}
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/not-an-id/versions", "owner", "").Code)
	require.Equal(t, 400, testutil.Do(r, "POST", path, "owner", `{"unrecognised":true}`).Code)
	var submitted tradingpolicy.Submission
	require.NoError(t, json.Unmarshal(body, &submitted))
	var original tradingpolicy.Receipt
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &original))
	require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "user-owner"), testutil.TenantA, func(tx *gorm.DB) error {
		replayed, err := tradingpolicy.Submit(tx, testutil.TenantA, "user-owner", submitted, time.Now().Add(48*time.Hour))
		require.NoError(t, err, "an unchanged retry must retain its original receipt even after eligibility expires")
		require.Equal(t, original.ID, replayed.ID)
		return err
	}))
	err = db.WithTenant(tenantdb.WithActor(ctx, "user-owner"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workspace_country_policy_actions(tenant_id,id,scope_id,version_id,actor_subject,action,reason,expected_revision,result_revision,request_hash) VALUES(?,?,?,?,?,'approved','TEST self approval must fail',1,2,?)`, testutil.TenantA, uuid.Must(uuid.NewV7()).String(), scopeID, id, "user-owner", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef").Error
	})
	require.Error(t, err, "the database must reject self approval even if a future handler misses its check")
	err = db.WithTenant(tenantdb.WithActor(ctx, "user-owner"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workspace_country_policy_sources(tenant_id,version_id,key,title,url,reviewed_at) VALUES(?,?,'TEST-late','TEST ONLY unreviewed addition','https://regulator.example.invalid/late',now())`, testutil.TenantA, id).Error
	})
	require.Error(t, err, "submitted source snapshots must reject additions as well as edits")
}

func TestCountryPolicyRequiresExplicitIndependentReviewer(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"owner": testutil.TenantA, "reviewer": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"owner":    {"workspace.country_policy.manage", "workspace.country_policy.read", "workspace.country_policy.review"},
			"reviewer": {"workspace.country_policy.read", "workspace.country_policy.review"},
		}, Modules: []platform.Module{workspace.New()},
	})
	now := time.Now().UTC()
	scope := uuid.Must(uuid.NewV7()).String()
	version := uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"id": version, "scope_id": scope, "expected_revision": 0, "country": "IN", "jurisdiction": "TEST-STATE", "category": "otc", "operation": "order", "currency": "INR", "effective_from": now.Add(-time.Hour), "effective_until": now.Add(time.Hour), "sources": []map[string]any{{"key": "TEST-source", "title": "TEST ONLY not legal approval", "url": "https://regulator.example.invalid/test", "reviewed_at": now.Add(-time.Hour)}}, "requirements": syntheticCountryRequirements()}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	path := "/v1/workspace/country-policies"
	w := testutil.Do(r, "POST", path, "owner", string(body))
	require.Equal(t, 202, w.Code, w.Body.String())
	decision := map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "version_id": version, "expected_revision": 1, "action": "approved", "reason": "TEST ONLY independent synthetic review"}
	review, err := json.Marshal(decision)
	require.NoError(t, err)
	w = testutil.Do(r, "POST", path+"/"+scope+"/review", "reviewer", string(review))
	require.Equal(t, 404, w.Code, "global review permission must not reveal an ungranted scope")
	w = testutil.Do(r, "POST", path+"/"+scope+"/review", "owner", string(review))
	require.Equal(t, 403, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "independent_review_required")
	grant := map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "expected_revision": 1, "subject": "user-reviewer", "role": "reviewer"}
	access, err := json.Marshal(grant)
	require.NoError(t, err)
	w = testutil.Do(r, "POST", path+"/"+scope+"/access", "owner", string(access))
	require.Equal(t, 202, w.Code, w.Body.String())
	decision["expected_revision"] = 2
	review, err = json.Marshal(decision)
	require.NoError(t, err)
	w = testutil.Do(r, "POST", path+"/"+scope+"/review", "reviewer", string(review))
	require.Equal(t, 202, w.Code, w.Body.String())
	var receipt tradingpolicy.Receipt
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &receipt))
	require.Equal(t, "approved", receipt.Action)
	require.Equal(t, int64(3), receipt.ResultRevision)
	require.Equal(t, "user-reviewer", receipt.ActorSubject)
	replay := testutil.Do(r, "POST", path+"/"+scope+"/review", "reviewer", string(review))
	require.Equal(t, 202, replay.Code, replay.Body.String())
	require.JSONEq(t, w.Body.String(), replay.Body.String())
	get := testutil.Do(r, "GET", path+"/"+scope, "reviewer", "")
	require.Equal(t, 200, get.Code, get.Body.String())
	require.Contains(t, get.Body.String(), "active")
	now = time.Now().UTC()
	request := tradingpolicy.Scope{Tenant: testutil.TenantA, Country: "IN", Jurisdiction: "TEST-STATE", Category: "otc", Operation: "order", Currency: "INR"}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		allowed, err := tradingpolicy.Check(tx, request, now)
		require.NoError(t, err)
		require.True(t, allowed.Allowed, allowed.Reason)
		require.Equal(t, version, allowed.PolicyVersion)
		otherState := request
		otherState.Jurisdiction = "TEST-OTHER-STATE"
		denied, err := tradingpolicy.Check(tx, otherState, now)
		require.NoError(t, err)
		require.False(t, denied.Allowed, "one state's approval must never activate another")
		expired, err := tradingpolicy.Check(tx, request, now.Add(2*time.Hour))
		require.NoError(t, err)
		require.False(t, expired.Allowed)
		return nil
	}))
	versionRead := testutil.Do(r, "GET", path+"/"+scope+"/versions/"+version, "reviewer", "")
	require.Equal(t, 200, versionRead.Code, versionRead.Body.String())
	var snapshot tradingpolicy.Version
	require.NoError(t, json.Unmarshal(versionRead.Body.Bytes(), &snapshot))
	require.Len(t, snapshot.Requirements, 11)
	require.Len(t, snapshot.Sources, 1)
	require.Equal(t, "TEST-source", snapshot.Requirements[0].SourceKeys[0])
	require.Equal(t, "user-reviewer", snapshot.ApprovedBy)
	for _, suffix := range []string{"/versions", "/actions"} {
		history := testutil.Do(r, "GET", path+"/"+scope+suffix+"?limit=1", "reviewer", "")
		require.Equal(t, 200, history.Code, history.Body.String())
		require.Contains(t, history.Body.String(), "approved")
	}

	checked := make(chan error, 1)
	finished := make(chan error, 1)
	release := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	defer unlock()
	go func() {
		finished <- db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			decision, err := tradingpolicy.Check(tx, request, time.Now())
			if err == nil && !decision.Allowed {
				err = tradingpolicy.ErrInvalid
			}
			checked <- err
			if err != nil {
				return err
			}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	require.NoError(t, <-checked)
	suspension := tradingpolicy.Review{ID: uuid.Must(uuid.NewV7()).String(), ExpectedRevision: 3, Action: "suspended", Reason: "TEST ONLY safety suspension"}
	err = db.WithTenant(tenantdb.WithActor(ctx, "user-reviewer"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout='100ms'").Error; err != nil {
			return err
		}
		_, err := tradingpolicy.Decide(tx, testutil.TenantA, "user-reviewer", scope, suspension, time.Now())
		return err
	})
	require.ErrorContains(t, err, "lock timeout", "suspension must serialize with the business commitment, not revoke between its check and commit")
	unlock()
	require.NoError(t, <-finished)
	require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "user-reviewer"), testutil.TenantA, func(tx *gorm.DB) error {
		_, err := tradingpolicy.Decide(tx, testutil.TenantA, "user-reviewer", scope, suspension, time.Now())
		return err
	}))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		decision, err := tradingpolicy.Check(tx, request, time.Now())
		require.NoError(t, err)
		require.False(t, decision.Allowed, "the next commitment must observe suspension")
		var count int64
		if err := tx.Table("workspace_country_policy_actions").Count(&count).Error; err != nil {
			return err
		}
		require.Equal(t, int64(4), count, "submission, delegation, review and suspension; replay and failed lock attempt add no actions")
		return nil
	}))
}

func syntheticCountryRequirements() []map[string]any {
	out := make([]map[string]any, 0, 11)
	for _, domain := range []string{"licensing", "product_registration", "prescription", "controlled", "cold_chain", "tax_invoice", "credit_note", "retention", "privacy", "residency", "reporting"} {
		out = append(out, map[string]any{"domain": domain, "applicability": "required", "reason": "TEST ONLY fictional requirement", "source_keys": []string{"TEST-source"}})
	}
	return out
}
