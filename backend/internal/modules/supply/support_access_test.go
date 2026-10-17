package supply_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own module's public test interface
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func supportHarness(t *testing.T) (*gin.Engine, *tenantdb.DB) {
	return supportHarnessWithRoles(t, accessMembership{})
}

func supportHarnessWithRoles(t *testing.T, roles platform.RoleLister) (*gin.Engine, *tenantdb.DB) {
	t.Helper()
	permissions := []authz.Permission{"supply.party.read", "supply.party.write", "supply.directory.read", "supply.party.access.manage", "supply.organization.manage", "supply.premise.read", "supply.premise.write", "supply.bin.read", "supply.bin.write", "supply.support.manage", "supply.support.read", "supply.support.revoke"}
	var beneficiaryPermissions []authz.Permission
	for _, permission := range permissions {
		if permission != "supply.party.access.manage" {
			beneficiaryPermissions = append(beneficiaryPermissions, permission)
		}
	}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA, "manager": testutil.TenantA, "auditor": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"owner": permissions, "manager": permissions, "auditor": beneficiaryPermissions, "foreign": permissions}, Roles: roles, Modules: []platform.Module{supply.New()}})
	return r, db
}
func supportFixture(t *testing.T, r *gin.Engine) (string, string) {
	t.Helper()
	root := ownedAccessParty(t, r)
	premise := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access/user-manager/administrator", "owner", "").Code)
	return root, premise
}
func supportBody(premise string, from, until time.Time) string {
	data, _ := json.Marshal(map[string]any{"expected_version": 0, "beneficiary_subject": "user-auditor", "premise_id": premise, "ticket_reference": "TEST-SUPPORT-001", "reason": "TEST investigate operational status only", "valid_from": from.UTC().Format(time.RFC3339), "expires_at": until.UTC().Format(time.RFC3339)})
	return string(data)
}
func decisionBody(version int, action string) string {
	data, _ := json.Marshal(map[string]any{"expected_version": version, "action": action, "reason": "TEST independently reviewed operational support"})
	return string(data)
}
func TestSupportRequiresIndependentApprovalAndAuditsNarrowDiagnostics(t *testing.T) {
	r, db := supportHarness(t)
	root, premise := supportFixture(t, r)
	path := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	from := time.Now().Add(-time.Minute)
	body := supportBody(premise, from, from.Add(time.Hour))
	w := testutil.Do(r, "PUT", path, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"pending"`)
	require.Contains(t, w.Body.String(), `"requester_subject":"user-owner"`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", body).Body.String())
	require.Equal(t, 403, testutil.Do(r, "POST", path+"/decision", "owner", decisionBody(1, "approve")).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/diagnostics", "auditor", "").Code)
	w = testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(1, "approve"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"approved"`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(1, "approve")).Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/diagnostics", "owner", "").Code)
	for range 2 {
		w = testutil.Do(r, "GET", path+"/diagnostics", "auditor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"premise_status":"active"`)
		for _, private := range []string{"TEST", "contact", "address", "reason", "ticket", "registration", "credit", "license", "patient"} {
			require.NotContains(t, w.Body.String(), private)
		}
	}
	require.Equal(t, 404, testutil.Do(r, "GET", root, "auditor", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", root+"/premises/"+premise, "auditor", "").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		if e := tx.Table("audit_events").Where("action='support_read'").Count(&n).Error; e != nil {
			return e
		}
		require.EqualValues(t, 2, n)
		return nil
	}))
	w = testutil.Do(r, "POST", path+"/decision", "owner", decisionBody(2, "revoke"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/diagnostics", "auditor", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "foreign", "").Code)
	history := testutil.Do(r, "GET", path+"/revisions?limit=1", "owner", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	require.Contains(t, history.Body.String(), `"version":3`)
}

func TestSupportApprovalStopsWhenApproverAuthorityIsRevoked(t *testing.T) {
	r, _ := supportHarness(t)
	root, premise := supportFixture(t, r)
	path := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	from := time.Now().Add(-time.Minute)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", supportBody(premise, from, from.Add(time.Hour))).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(1, "approve")).Code)
	list := testutil.Do(r, "GET", "/v1/supply/parties", "auditor", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), `"legal_name":"TEST Explicit Access"`)
	require.Equal(t, 204, testutil.Do(r, "DELETE", root+"/access/user-manager/administrator", "owner", "").Code)
	w := testutil.Do(r, "GET", path, "auditor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"effective_status":"approval_withdrawn"`)
	require.NotContains(t, w.Body.String(), `read_diagnostics`)
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/diagnostics", "auditor", "").Code)
	list = testutil.Do(r, "GET", "/v1/supply/parties", "auditor", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), `"data":[]`)
	require.Equal(t, 404, testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(1, "approve")).Code)
}
func TestSupportBeneficiaryCannotApproveEvenWithAdministratorAuthority(t *testing.T) {
	r, _ := supportHarness(t)
	root, premise := supportFixture(t, r)
	path := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	from := time.Now().Add(-time.Minute)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access/user-auditor/administrator", "owner", "").Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", supportBody(premise, from, from.Add(time.Hour))).Code)
	require.Equal(t, 403, testutil.Do(r, "POST", path+"/decision", "auditor", decisionBody(1, "approve")).Code)
	require.Equal(t, 400, testutil.Do(r, "POST", path+"/revoke", "auditor", decisionBody(1, "approve")).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(1, "reject")).Code)
	require.Equal(t, 409, testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(2, "approve")).Code)
}
func TestSupportAuditFailureDoesNotReturnDiagnostics(t *testing.T) {
	r, db := supportHarness(t)
	root, premise := supportFixture(t, r)
	path := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	from := time.Now().Add(-time.Minute)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", supportBody(premise, from, from.Add(time.Hour))).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(1, "approve")).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_support_audit_failure", SQL: `ALTER TABLE audit_events ADD CONSTRAINT test_support_audit_failure CHECK(action<>'support_read') NOT VALID`}}))
	w := testutil.Do(r, "GET", path+"/diagnostics", "auditor", "")
	require.Equal(t, 500, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "bin_count")
	require.NotContains(t, w.Body.String(), "test_support_audit_failure")
}
func TestSupportOutboxFailureRollsBackIndependentApproval(t *testing.T) {
	r, db := supportHarness(t)
	root, premise := supportFixture(t, r)
	path := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	from := time.Now().Add(-time.Minute)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", supportBody(premise, from, from.Add(time.Hour))).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_support_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_support_outbox_failure CHECK(subject<>'helivanta.in.supply.support_access_changed.v1') NOT VALID`}}))
	w := testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(1, "approve"))
	require.Equal(t, 500, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "test_support_outbox_failure")
	w = testutil.Do(r, "GET", path, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"pending"`)
	require.Contains(t, w.Body.String(), `"version":1`)
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/diagnostics", "auditor", "").Code)
}

func TestSupportConcurrentRetriesCommitOneRequestAndDecision(t *testing.T) {
	r, db := supportHarness(t)
	root, premise := supportFixture(t, r)
	path := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	from := time.Now().Add(-time.Minute)
	body := supportBody(premise, from, from.Add(time.Hour))
	for _, request := range []struct{ method, url, token, body string }{{"PUT", path, "owner", body}, {"POST", path + "/decision", "manager", decisionBody(1, "approve")}} {
		var group sync.WaitGroup
		codes := make(chan int, 2)
		for range 2 {
			group.Go(func() { codes <- testutil.Do(r, request.method, request.url, request.token, request.body).Code })
		}
		group.Wait()
		close(codes)
		for code := range codes {
			require.Equal(t, 200, code)
		}
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-owner',true)`, strings.TrimPrefix(root, "/v1/supply/parties/")).Error; e != nil {
			return e
		}
		var n int64
		if e := tx.Table("supply_support_access_revisions").Count(&n).Error; e != nil {
			return e
		}
		require.EqualValues(t, 2, n)
		if e := tx.Table("outbox_events").Where("subject='helivanta.in.supply.support_access_changed.v1'").Count(&n).Error; e != nil {
			return e
		}
		require.EqualValues(t, 2, n)
		return nil
	}))
}
func TestSupportFiniteApproverCannotApproveBeyondOwnAuthority(t *testing.T) {
	r, _ := supportHarness(t)
	root, premise := supportFixture(t, r)
	require.Equal(t, 204, testutil.Do(r, "DELETE", root+"/access/user-manager/administrator", "owner", "").Code)
	from := time.Now().Add(-time.Minute)
	w := testutil.Do(r, "PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", accessBody(0, "user-manager", "administrator", nil, from, from.Add(30*time.Minute), "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	path := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", supportBody(premise, from, from.Add(time.Hour))).Code)
	w = testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(1, "approve"))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/diagnostics", "auditor", "").Code)
}
func TestSupportScopeExpiryAndDatabaseIsolation(t *testing.T) {
	r, db := supportHarness(t)
	root, premise := supportFixture(t, r)
	id := uuid.Must(uuid.NewV7()).String()
	path := root + "/support/" + id
	from := time.Now().Add(time.Hour)
	w := testutil.Do(r, "PUT", path, "owner", supportBody(premise, from, from.Add(time.Hour)))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 200, testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(1, "approve")).Code)
	w = testutil.Do(r, "GET", path, "auditor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"effective_status":"scheduled"`)
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/diagnostics", "auditor", "").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-auditor',true),set_config('app.supply_support_id',?,true)`, strings.TrimPrefix(root, "/v1/supply/parties/"), id).Error; e != nil {
			return e
		}
		var n int64
		if e := tx.Table("supply_premises").Where("id=?", premise).Count(&n).Error; e != nil {
			return e
		}
		require.Zero(t, n, "scheduled approval does not expose premise through RLS")
		return nil
	}))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-auditor',true),set_config('app.supply_support_id',?,true)`, strings.TrimPrefix(root, "/v1/supply/parties/"), id).Error; e != nil {
			return e
		}
		for _, table := range []string{"supply_support_access", "supply_support_access_revisions", "supply_premises", "supply_bins"} {
			var n int64
			if e := tx.Table(table).Count(&n).Error; e != nil {
				return e
			}
			require.Zero(t, n)
		}
		return nil
	}))
}

func TestSupportBeneficiaryCanOnlyReduceOwnAccess(t *testing.T) {
	r, _ := supportHarness(t)
	root, premise := supportFixture(t, r)
	path := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	from := time.Now().Add(-time.Minute)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", supportBody(premise, from, from.Add(time.Hour))).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(1, "approve")).Code)
	body := `{"expected_version":2,"reason":"TEST end my approved support session"}`
	w := testutil.Do(r, "POST", path+"/revoke", "auditor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"revoked"`)
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/diagnostics", "auditor", "").Code)
}

func TestSupportRejectsSpoofedScopeAndUnavailableBeneficiary(t *testing.T) {
	r, _ := supportHarness(t)
	root, premise := supportFixture(t, r)
	from := time.Now().Add(-time.Minute)
	valid := supportBody(premise, from, from.Add(time.Hour))
	for _, entry := range []struct {
		body   string
		status int
	}{{supportBody(premise, from, from.Add(25*time.Hour)), 400}, {strings.Replace(valid, "user-auditor", "unknown-user", 1), 404}, {supportBody(uuid.Must(uuid.NewV7()).String(), from, from.Add(time.Hour)), 404}, {strings.TrimSuffix(valid, "}") + `,"requester_subject":"user-manager"}`, 400}} {
		w := testutil.Do(r, "PUT", root+"/support/"+uuid.Must(uuid.NewV7()).String(), "owner", entry.body)
		require.Equal(t, entry.status, w.Code, w.Body.String())
	}
	require.Equal(t, 404, testutil.Do(r, "PUT", root+"/support/"+uuid.Must(uuid.NewV7()).String(), "auditor", valid).Code)
}
func TestExpiredApprovedSupportNeverAuthorizesAReadWithoutTimer(t *testing.T) {
	r, db := supportHarness(t)
	root, premise := supportFixture(t, r)
	id := uuid.Must(uuid.NewV7()).String()
	partyID := strings.TrimPrefix(root, "/v1/supply/parties/")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-owner',true),set_config('app.actor_subject','user-owner',true),set_config('app.supply_access_admin','on',true)`, partyID).Error; e != nil {
			return e
		}
		if e := tx.Exec(`INSERT INTO supply_support_access(tenant_id,party_id,id,premise_id,requester_subject,beneficiary_subject,ticket_reference,reason,valid_from,expires_at,status,version) VALUES(?,?,?,?,'user-owner','user-auditor','TEST-EXPIRED','TEST historical approved support',clock_timestamp()-interval '2 hours',clock_timestamp()-interval '1 hour','pending',1)`, testutil.TenantA, partyID, id, premise).Error; e != nil {
			return e
		}
		if e := tx.Exec(`INSERT INTO supply_support_access_revisions(tenant_id,party_id,support_id,id,version,snapshot,actor_subject) SELECT tenant_id,party_id,id,?,version,to_jsonb(supply_support_access),'user-owner' FROM supply_support_access WHERE tenant_id=? AND party_id=? AND id=?`, uuid.Must(uuid.NewV7()), testutil.TenantA, partyID, id).Error; e != nil {
			return e
		}
		if e := tx.Exec(`SELECT set_config('app.supply_subject','user-manager',true),set_config('app.actor_subject','user-manager',true)`).Error; e != nil {
			return e
		}
		if e := tx.Exec(`UPDATE supply_support_access SET status='approved',approver_subject='user-manager',decision_actor='user-manager',decision_reason='TEST historical independent approval',version=2 WHERE tenant_id=? AND party_id=? AND id=?`, testutil.TenantA, partyID, id).Error; e != nil {
			return e
		}
		return tx.Exec(`INSERT INTO supply_support_access_revisions(tenant_id,party_id,support_id,id,version,snapshot,actor_subject) SELECT tenant_id,party_id,id,?,version,to_jsonb(supply_support_access),'user-manager' FROM supply_support_access WHERE tenant_id=? AND party_id=? AND id=?`, uuid.Must(uuid.NewV7()), testutil.TenantA, partyID, id).Error
	}))
	path := root + "/support/" + id
	w := testutil.Do(r, "GET", path, "auditor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"effective_status":"expired"`)
	require.NotContains(t, w.Body.String(), "read_diagnostics")
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/diagnostics", "auditor", "").Code)
	require.Contains(t, testutil.Do(r, "GET", "/v1/supply/parties", "auditor", "").Body.String(), `"data":[]`)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_subject','user-auditor',true),set_config('app.supply_support_id',?,true)`, partyID, id).Error; e != nil {
			return e
		}
		var n int64
		if e := tx.Table("supply_premises").Where("id=?", premise).Count(&n).Error; e != nil {
			return e
		}
		require.Zero(t, n)
		return nil
	}))
}
