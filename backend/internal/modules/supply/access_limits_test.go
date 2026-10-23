package supply_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func accessLimitsBody(version, grantHours, supportHours int) string {
	return fmt.Sprintf(`{"expected_version":%d,"max_grant_hours":%d,"max_support_hours":%d,"reason":"TEST independently bounded operational access"}`, version, grantHours, supportHours)
}

func TestOperationalAccessLimitsAreVersionedAndReplaySafe(t *testing.T) {
	r, _ := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	path := root + "/access-limits"
	w := testutil.Do(r, "GET", path, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":0`)
	require.Contains(t, w.Body.String(), `"max_grant_hours":8784`)
	require.Contains(t, w.Body.String(), `"max_support_hours":24`)
	w = testutil.Do(r, "PUT", path, "owner", accessLimitsBody(0, 48, 2))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":1`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", accessLimitsBody(0, 48, 2)).Body.String())
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "GET", path, "owner", "").Body.String())
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "owner", accessLimitsBody(0, 24, 1)).Code)
	w = testutil.Do(r, "PUT", path, "owner", accessLimitsBody(1, 24, 1))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", path, "owner", accessLimitsBody(1, 24, 1)).Body.String())
	w = testutil.Do(r, "GET", path+"/revisions?limit=2", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":1`)
	require.Contains(t, w.Body.String(), `"version":2`)
}

func TestOperationalAccessLimitsPreventPermanentCompatibilityBypass(t *testing.T) {
	r, _ := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	existing := root + "/access/user-auditor/auditor"
	w := testutil.Do(r, "PUT", existing, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access-limits", "owner", accessLimitsBody(0, 48, 2)).Code)
	require.JSONEq(t, w.Body.String(), testutil.Do(r, "PUT", existing, "owner", "").Body.String())
	w = testutil.Do(r, "PUT", root+"/access/user-manager/purchasing", "owner", "")
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "timed access grant")
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access/user-manager/administrator", "owner", "").Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", existing, "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", root, "auditor", "").Code)
}

func TestOperationalAccessLimitsAreExactOwnedPartyAndAdministratorOnly(t *testing.T) {
	r, db := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	path := root + "/access-limits"
	for _, token := range []string{"auditor", "manager", "foreign"} {
		for _, endpoint := range []string{path, path + "/revisions"} {
			require.Equal(t, 404, testutil.Do(r, "GET", endpoint, token, "").Code)
		}
		require.Equal(t, 404, testutil.Do(r, "PUT", path, token, accessLimitsBody(0, 48, 2)).Code)
	}
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access/user-manager/administrator", "owner", "").Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "manager", accessLimitsBody(0, 48, 2)).Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", root+"/access/user-manager/administrator", "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "manager", accessLimitsBody(0, 48, 2)).Code)
	other := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := strings.ReplaceAll(strings.ReplaceAll(partyBody(0, "TEST Counterparty Limits"), "TEST-DISTRIBUTOR", "TEST-COUNTERPARTY-LIMIT"), "TEST-REG-001", "TEST-LIMIT-COUNTERPARTY")
	require.Equal(t, 200, testutil.Do(r, "PUT", other, "owner", body).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", other+"/access-limits", "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", other+"/access-limits", "owner", accessLimitsBody(0, 48, 2)).Code)
	id := strings.TrimPrefix(root, "/v1/supply/parties/")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		var count int64
		if e := tx.Table("supply_operational_access_limits").Where("party_id=?", id).Count(&count).Error; e != nil {
			return e
		}
		require.Zero(t, count)
		return nil
	}))
}

func TestOperationalAccessLimitsAreSeparateForEachOrganisation(t *testing.T) {
	r, _ := scopedAccessHarness(t)
	india := ownedAccessParty(t, r)
	other := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(partyBody(0, "TEST Second Distributor"), "counterparty", "owned_organisation"), "TEST-REG-001", "TEST-SECOND-LIMIT-001"), "TEST-DISTRIBUTOR", "TEST-SECOND-DISTRIBUTOR")
	w := testutil.Do(r, "PUT", other, "manager", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 200, testutil.Do(r, "PUT", india+"/access-limits", "owner", accessLimitsBody(0, 48, 2)).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", other+"/access-limits", "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", other+"/access-limits", "owner", accessLimitsBody(0, 48, 2)).Code)
	w = testutil.Do(r, "GET", other+"/access-limits", "manager", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"max_grant_hours":8784`)
	require.Equal(t, 200, testutil.Do(r, "PUT", other+"/access-limits", "manager", accessLimitsBody(0, 72, 3)).Code)
	require.Contains(t, testutil.Do(r, "GET", india+"/access-limits", "owner", "").Body.String(), `"max_grant_hours":48`)
}

func TestOperationalAccessLimitsValidateCeilingsAndDoNotLoosenIndependentApproval(t *testing.T) {
	r, _ := supportHarness(t)
	root, premise := supportFixture(t, r)
	path := root + "/access-limits"
	for _, body := range []string{accessLimitsBody(0, 0, 1), accessLimitsBody(0, 8785, 1), accessLimitsBody(0, 1, 0), accessLimitsBody(0, 1, 25), strings.Replace(accessLimitsBody(0, 1, 1), "\"max_grant_hours\":1", "\"max_grant_hours\":1.5", 1), strings.Replace(accessLimitsBody(0, 1, 1), "TEST independently bounded operational access", "bad", 1), strings.Replace(accessLimitsBody(0, 1, 1), `"expected_version":0`, `"expected_version":null`, 1)} {
		w := testutil.Do(r, "PUT", path, "owner", body)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", accessLimitsBody(0, 1, 1)).Code)
	from := time.Now().Add(-time.Minute)
	support := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", support, "owner", supportBody(premise, from, from.Add(time.Hour))).Code)
	require.Equal(t, 403, testutil.Do(r, "POST", support+"/decision", "owner", decisionBody(1, "approve")).Code)
}

func TestOperationalAccessLimitsGovernNewAndExtendedTimedAccessWithoutRewritingHistory(t *testing.T) {
	r, _ := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	grant := root + "/grants/" + uuid.Must(uuid.NewV7()).String()
	from := time.Now().Add(-time.Minute)
	body := accessBody(0, "user-auditor", "auditor", nil, from, from.Add(24*time.Hour), "active")
	w := testutil.Do(r, "PUT", grant, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	original := w.Body.String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access-limits", "owner", accessLimitsBody(0, 2, 1)).Code)
	require.JSONEq(t, original, testutil.Do(r, "GET", grant, "owner", "").Body.String())
	require.JSONEq(t, original, testutil.Do(r, "PUT", grant, "owner", body).Body.String())
	w = testutil.Do(r, "PUT", grant, "owner", accessBody(1, "user-auditor", "auditor", nil, from, from.Add(25*time.Hour), "active"))
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "operational access limits")
	newGrant := root + "/grants/" + uuid.Must(uuid.NewV7()).String()
	w = testutil.Do(r, "PUT", newGrant, "owner", accessBody(0, "user-manager", "finance", nil, from, from.Add(3*time.Hour), "active"))
	require.Equal(t, 400, w.Code, w.Body.String())
	w = testutil.Do(r, "PUT", newGrant, "owner", accessBody(0, "user-manager", "finance", nil, from, from.Add(2*time.Hour), "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	corrected := strings.Replace(accessBody(1, "user-auditor", "auditor", nil, from, from.Add(24*time.Hour), "active"), "TEST approved operational access", "TEST clarified operational access", 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", grant, "owner", corrected).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", grant, "owner", accessBody(2, "user-auditor", "auditor", nil, from, from.Add(24*time.Hour), "revoked")).Code)
}

func TestOperationalAccessLimitsRecheckPendingSupportAtApprovalAndAllowRevocation(t *testing.T) {
	r, _ := supportHarness(t)
	root, premise := supportFixture(t, r)
	from := time.Now().Add(-time.Minute)
	approved := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	pending := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	for _, path := range []string{approved, pending} {
		require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", supportBody(premise, from, from.Add(2*time.Hour))).Code)
	}
	require.Equal(t, 200, testutil.Do(r, "POST", approved+"/decision", "manager", decisionBody(1, "approve")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access-limits", "owner", accessLimitsBody(0, 2, 1)).Code)
	require.Equal(t, 200, testutil.Do(r, "GET", approved+"/diagnostics", "auditor", "").Code)
	w := testutil.Do(r, "POST", pending+"/decision", "manager", decisionBody(1, "approve"))
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "operational access limits")
	require.Contains(t, testutil.Do(r, "GET", pending, "owner", "").Body.String(), `"status":"pending"`)
	newRequest := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 400, testutil.Do(r, "PUT", newRequest, "owner", supportBody(premise, from, from.Add(2*time.Hour))).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", approved+"/decision", "owner", decisionBody(2, "revoke")).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", approved+"/diagnostics", "auditor", "").Code)
	require.Equal(t, 200, testutil.Do(r, "POST", pending+"/decision", "manager", decisionBody(1, "reject")).Code)
}

func TestOperationalAccessLimitsConcurrentReplayAuditAndOutboxAreAtomic(t *testing.T) {
	r, db := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	path := root + "/access-limits"
	var start sync.WaitGroup
	start.Add(1)
	results := make(chan int, 4)
	for range 4 {
		go func() { start.Wait(); results <- testutil.Do(r, "PUT", path, "owner", accessLimitsBody(0, 48, 2)).Code }()
	}
	start.Done()
	for range 4 {
		require.Equal(t, 200, <-results)
	}
	id := strings.TrimPrefix(root, "/v1/supply/parties/")
	var counts struct{ Revisions, Audits, Events int }
	require.NoError(t, partyTransaction(t, db, id, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (SELECT count(*) FROM supply_operational_access_limit_revisions WHERE party_id=?) revisions,(SELECT count(*) FROM audit_events WHERE resource_type='supply_operational_access_limits' AND resource_id=?) audits,(SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.supply.operational_access_limits_updated.v1' AND payload->'data'->>'party_id'=?) events`, id, id, id).Scan(&counts).Error
	}))
	require.Equal(t, 1, counts.Revisions)
	require.Equal(t, 1, counts.Audits)
	require.Equal(t, 1, counts.Events)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_access_limit_outbox_failure", SQL: `ALTER TABLE outbox_events ADD CONSTRAINT test_access_limit_outbox_failure CHECK(subject <> 'helivanta.in.supply.operational_access_limits_updated.v1') NOT VALID`}}))
	require.Equal(t, 500, testutil.Do(r, "PUT", path, "owner", accessLimitsBody(1, 24, 1)).Code)
	w := testutil.Do(r, "GET", path, "owner", "")
	require.Contains(t, w.Body.String(), `"version":1`)
	require.Contains(t, w.Body.String(), `"max_grant_hours":48`)
	for _, query := range []string{`DELETE FROM supply_operational_access_limits`, `UPDATE supply_operational_access_limit_revisions SET version=99`, `DELETE FROM supply_operational_access_limit_revisions`} {
		require.Error(t, partyTransaction(t, db, id, func(tx *gorm.DB) error { return tx.Exec(query).Error }))
	}
}

func TestOperationalAccessLimitsRejectExpiredAdministratorAndDatabaseBypasses(t *testing.T) {
	r, db := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	from := time.Now().Add(-time.Hour)
	grantID := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/grants/"+grantID, "owner", accessBody(0, "user-manager", "administrator", nil, from, from.Add(24*time.Hour), "active")).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access-limits", "manager", accessLimitsBody(0, 2, 1)).Code)
	id := strings.TrimPrefix(root, "/v1/supply/parties/")
	require.NoError(t, partyTransaction(t, db, id, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_access_admin','on',true),set_config('app.actor_subject','user-owner',true)`).Error; e != nil {
			return e
		}
		if e := tx.Exec(`UPDATE supply_access_grants SET expires_at=clock_timestamp()-interval '1 second',version=version+1 WHERE tenant_id=? AND party_id=? AND id=?`, testutil.TenantA, id, grantID).Error; e != nil {
			return e
		}
		return tx.Exec(`INSERT INTO supply_access_grant_revisions(tenant_id,party_id,grant_id,id,version,snapshot,actor_subject) SELECT tenant_id,party_id,id,?,version,to_jsonb(supply_access_grants),'user-owner' FROM supply_access_grants WHERE tenant_id=? AND party_id=? AND id=?`, uuid.Must(uuid.NewV7()), testutil.TenantA, id, grantID).Error
	}))
	require.Equal(t, 404, testutil.Do(r, "GET", root+"/access-limits", "manager", "").Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", root+"/access-limits", "manager", accessLimitsBody(0, 2, 1)).Code)
	err := partyTransaction(t, db, id, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_access_admin','on',true)`).Error; e != nil {
			return e
		}
		return tx.Exec(`INSERT INTO supply_access_grants(tenant_id,party_id,id,subject,role,valid_from,expires_at,reason,status,version) VALUES(?,?,?,'user-auditor','finance',?,?,'TEST direct bounded grant','active',1)`, testutil.TenantA, id, uuid.Must(uuid.NewV7()), from, from.Add(3*time.Hour)).Error
	})
	var pg *pgconn.PgError
	require.ErrorAs(t, err, &pg)
	require.Equal(t, "supply_operational_access_duration", pg.ConstraintName)
	err = partyTransaction(t, db, id, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE supply_operational_access_limits SET max_grant_hours=8785,version=version+1 WHERE party_id=?`, id).Error
	})
	require.Error(t, err)
	err = partyTransaction(t, db, id, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.supply_subject','user-auditor',true),set_config('app.supply_access_admin','on',true)`).Error; e != nil {
			return e
		}
		return tx.Exec(`SELECT supply_operational_access_limit(?,?,'grant')`, testutil.TenantA, id).Error
	})
	require.ErrorAs(t, err, &pg)
	require.Equal(t, "supply_operational_access_limits_authority", pg.ConstraintName)
}

func TestOperationalAccessLimitsPolicyFailureDoesNotUseDefaultOrWriteGrant(t *testing.T) {
	r, db := scopedAccessHarness(t)
	root := ownedAccessParty(t, r)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_access_limit_policy_unavailable", SQL: `ALTER TABLE supply_operational_access_limits RENAME TO test_unavailable_operational_access_limits`}}))
	w := testutil.Do(r, "GET", root+"/access-limits", "owner", "")
	require.Equal(t, 500, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "max_grant_hours")
	from := time.Now().Add(-time.Minute)
	path := root + "/grants/" + uuid.Must(uuid.NewV7()).String()
	w = testutil.Do(r, "PUT", path, "owner", accessBody(0, "user-auditor", "auditor", nil, from, from.Add(time.Hour), "active"))
	require.Equal(t, 500, w.Code, w.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", path, "owner", "").Code)
}
