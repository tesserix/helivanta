package workforce_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// IAM is the external authority boundary here; its real projection/epoch behavior
// is exercised by TestIdentityTemplateScope in the IAM integration suite.
type identityScopeAuthority struct {
	db         *tenantdb.DB
	hospital   string
	permission authz.Permission
	after      error
	failure    error
}

func (a *identityScopeAuthority) AuthorizeScope(_ context.Context, r authz.ScopeRequest) (authz.ScopeGrant, error) {
	if a.failure != nil {
		return authz.ScopeGrant{}, a.failure
	}
	permission := a.permission
	if permission == "" {
		permission = workforce.PermIdentityChecksManage
	}
	if r.TenantID != testutil.TenantA || r.HospitalID != a.hospital || r.Subject != "user-hr-manager" || r.Permission != permission {
		return authz.ScopeGrant{}, nil
	}
	return authz.ScopeGrant{AssignmentID: "019a0000-0000-7000-8000-000000000001", Version: 4, ScopeEpoch: 1, MembershipEpoch: 1}, nil
}

func (a *identityScopeAuthority) WithScopeGrant(ctx context.Context, r authz.ScopeRequest, _ authz.ScopeGrant, _ time.Time, action func(context.Context) error) error {
	return a.db.WithGuardedTenantCommand(ctx, r.TenantID, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		if err := action(commandCtx); err != nil {
			return err
		}
		return a.after
	})
}

func newIdentitySourceHarness(t *testing.T) (*requestHarness, *identityScopeAuthority, string) {
	t.Helper()
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	require.NoError(t, fga.GrantTenantRole(t.Context(), testutil.TenantA, authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(t.Context(), testutil.TenantA, "user-hr-manager", authz.RoleStaffMember))
	authority := &identityScopeAuthority{}
	h := newRequestHarness(t, func(opts *testutil.HarnessOptions) {
		opts.Writer, opts.Roles = fga, fga
		opts.Tokens["hr-manager"] = testutil.TenantA
		opts.Perms["admin"] = append(opts.Perms["admin"], workforce.PermIdentityChecksManage)
		opts.Perms["admin"] = append(opts.Perms["admin"], workforce.PermLoginLinkManage)
		opts.Perms["profiler"] = append(opts.Perms["profiler"], workforce.PermLoginLinkReview)
		opts.ScopedAuthorizer = func(db *tenantdb.DB) authz.ScopedAuthorizer { authority.db = db; return authority }
	})
	authority.hospital = h.hospital
	code, out := h.call("PUT", "/access/user-profiler", "admin", map[string]any{"level": "manager"})
	require.Equal(t, 200, code, out)
	actor := h.staff("SYN-HR-MANAGER")
	link := "/staff/" + actor + "/login-links/" + uuid.Must(uuid.NewV7()).String()
	code, out = h.call("PUT", link, "admin", map[string]any{"subject": "user-hr-manager", "reason": "Synthetic active HR binding"})
	require.Equal(t, 200, code, out)
	code, out = h.call("POST", link+"/decision", "profiler", map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic binding review"})
	require.Equal(t, 200, code, out)
	return h, authority, actor
}

func TestIdentitySourceGeneration(t *testing.T) {
	h, _, _ := newIdentitySourceHarness(t)
	target := h.staff("SYN-HR-TARGET")
	code, raw := h.do("GET", h.base+"/identity-staff/"+target, "hr-manager", "")
	require.Equal(t, 200, code, raw)
	var original map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &original))
	require.Equal(t, target, original["staff_id"])
	require.Equal(t, float64(1), original["employment_epoch"])
	require.Len(t, original["identity_hash"].(string), 64)
	code, basic := h.call("GET", "/staff/"+target, "admin", nil)
	require.Equal(t, 200, code, basic)
	basic = map[string]any{"employee_code": basic["employee_code"], "display_name": basic["display_name"], "job_category": basic["job_category"], "employment_status": basic["employment_status"]}
	profile := map[string]any{"work_phone": "+12025550100"}
	code, saved := h.call("PUT", "/staff/"+target+"/enrollment", "admin", map[string]any{
		"expected_version": original["staff_version"], "staff": basic, "profile": profile,
	})
	require.Equal(t, 200, code, saved)
	code, contactEdit := h.call("GET", "/identity-staff/"+target, "hr-manager", nil)
	require.Equal(t, 200, code, contactEdit)
	require.Equal(t, original["identity_hash"], contactEdit["identity_hash"], "contact edits do not change identity evidence")
	profile["date_of_birth"] = "1990-01-02"
	code, saved = h.call("PUT", "/staff/"+target+"/enrollment", "admin", map[string]any{
		"expected_version": contactEdit["staff_version"], "staff": basic, "profile": profile,
	})
	require.Equal(t, 200, code, saved)
	code, identityEdit := h.call("GET", "/identity-staff/"+target, "hr-manager", nil)
	require.Equal(t, 200, code, identityEdit)
	require.NotEqual(t, contactEdit["identity_hash"], identityEdit["identity_hash"], "DOB amendments need a new identity review")
	for _, transition := range []struct {
		status string
		epoch  float64
	}{{"inactive", 2}, {"active", 3}} {
		basic["employment_status"] = transition.status
		code, out := h.call("PUT", "/staff/"+target, "admin", basic)
		require.Equal(t, 200, code, out)
		code, source := h.call("GET", "/identity-staff/"+target, "hr-manager", nil)
		require.Equal(t, 200, code, source)
		require.Equal(t, transition.epoch, source["employment_epoch"], "rehire must never reuse an earlier employment generation")
	}
}

func TestIdentitySourceAuthority(t *testing.T) {
	h, authority, actor := newIdentitySourceHarness(t)
	target := h.staff("SYN-ID-SCOPE")
	path := h.base + "/identity-staff/" + target
	code, body := h.do("GET", path, "admin", "")
	require.Equal(t, 403, code, body)
	require.Contains(t, body, "Staff identity action not permitted")
	code, body = h.do("GET", path, "hr-manager", "")
	require.Equal(t, 200, code, body)
	authority.failure = errors.New("private dependency failure")
	code, body = h.do("GET", path, "hr-manager", "")
	require.Equal(t, 503, code, body)
	require.NotContains(t, body, "private dependency")
	require.NotContains(t, body, "SYN-ID-SCOPE")
	authority.failure = nil
	before := identityReadAudits(t, h, target)
	require.Equal(t, int64(1), before, "successful sensitive read must be audited")
	var audit string
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT changes::text FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce.identity_source.read' ORDER BY created_at DESC,id DESC LIMIT 1`, testutil.TenantA, h.hospital, target).Scan(&audit).Error
	}))
	var changes map[string]any
	require.NoError(t, json.Unmarshal([]byte(audit), &changes))
	require.Len(t, changes, 6, "read audit contains authority metadata only")
	require.Equal(t, float64(4), changes["assignment_version"])
	require.NotContains(t, audit, "SYN-ID-SCOPE")
	require.Equal(t, "no-store", h.header("/identity-staff/"+target, "hr-manager", "Cache-Control"))
	before = identityReadAudits(t, h, target)
	authority.after = authz.ErrScopedCommandDenied
	code, body = h.do("GET", path, "hr-manager", "")
	require.Equal(t, 403, code, body)
	require.NotContains(t, body, "identity_hash")
	require.Equal(t, before, identityReadAudits(t, h, target), "denied commit must roll back sensitive read audit")
	authority.after = nil
	code, body = h.do("GET", "/v1/workforce/hospitals/"+uuid.NewString()+"/identity-staff/"+target, "hr-manager", "")
	require.Contains(t, []int{403, 404}, code, body)
	code, body = h.do("GET", h.base+"/identity-staff/"+uuid.NewString(), "hr-manager", "")
	require.Equal(t, 404, code, body)
	code, basic := h.call("GET", "/staff/"+actor, "admin", nil)
	require.Equal(t, 200, code, basic)
	code, out := h.call("PUT", "/staff/"+actor, "admin", map[string]any{"employee_code": basic["employee_code"], "display_name": basic["display_name"], "job_category": basic["job_category"], "employment_status": "inactive"})
	require.Equal(t, 200, code, out)
	code, body = h.do("GET", path, "hr-manager", "")
	require.Equal(t, 404, code, body)
	require.NotContains(t, body, "identity_hash")
}

func identityReadAudits(t *testing.T, h *requestHarness, staff string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce.identity_source.read'", testutil.TenantA, h.hospital, staff).Count(&count).Error
	}))
	return count
}

func TestIdentitySourceEpochCannotBeForged(t *testing.T) {
	h, _, _ := newIdentitySourceHarness(t)
	target := h.staff("SYN-ID-EPOCH")
	for _, epoch := range []int64{0, 2, 9007199254740991} {
		err := h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, h.hospital).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE workforce_staff SET employment_epoch=? WHERE tenant_id=? AND hospital_id=? AND id=?`, epoch, testutil.TenantA, h.hospital, target).Error
		})
		require.ErrorContains(t, err, "employment epoch is maintained by owner")
	}
	code, out := h.call("GET", "/identity-staff/"+target, "hr-manager", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, float64(1), out["employment_epoch"])
}

func TestIdentitySourceReviewerReadsSameProtectedSource(t *testing.T) {
	h, authority, _ := newIdentitySourceHarness(t)
	authority.permission = workforce.PermIdentityChecksReview
	target := h.staff("SYN-REVIEW-SOURCE")
	code, body := h.do("GET", h.base+"/identity-staff/"+target, "hr-manager", "")
	require.Equal(t, 200, code, body)
	var source map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &source))
	require.Equal(t, target, source["staff_id"])
	require.Equal(t, int64(1), identityReadAudits(t, h, target))
}
