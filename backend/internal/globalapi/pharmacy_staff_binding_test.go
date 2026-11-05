package globalapi

import (
	"context"
	"maps"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type pharmacyBindingPreparation struct {
	base, path, preparer string
	pending              map[string]any
}

func assertPharmacyDepartmentBindingPreparation(t *testing.T, db *tenantdb.DB, call func(*testing.T, string, string, string, any, int) map[string]any, administrator, preparer, hospital, staff, department, revision string) *pharmacyBindingPreparation {
	t.Helper()
	pharmacy, outlet, binding := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	base := "/v1/pharmacy/pharmacies/" + pharmacy
	call(t, "PUT", base, administrator, map[string]any{"code": "SYN-BIND-PHARM", "name": "Synthetic hospital pharmacy", "ownership_type": "hospital_owned"}, 200)
	base += "/outlets/" + outlet
	call(t, "PUT", base, administrator, map[string]any{"code": "SYN-BIND-OUTLET", "name": "Synthetic binding outlet", "country_code": "IN", "timezone": "Asia/Kolkata"}, 200)
	call(t, "PUT", base+"/access/"+preparer, administrator, map[string]any{"level": "editor"}, 200)
	path := base + "/hospitals/" + hospital + "/staff-department-bindings/" + binding
	in := map[string]any{"department_id": department, "department_revision_id": revision, "replaces_binding_id": nil, "replaces_binding_version": nil, "reason": "Synthetic reviewed relationship preparation"}
	prepared := call(t, "PUT", path, preparer, in, 200)
	require.Contains(t, prepared, "binding", "proposal and decision responses must use the canonical retained receipt, not an unverified row")
	require.Len(t, prepared["authorizations"], 2)
	saved := prepared["binding"].(map[string]any)
	require.Equal(t, binding, saved["id"])
	require.Equal(t, pharmacy, saved["pharmacy_id"])
	require.Equal(t, outlet, saved["outlet_id"])
	require.Equal(t, department, saved["department_id"])
	require.Equal(t, revision, saved["department_revision_id"])
	require.Equal(t, staff, saved["proposer_staff_id"])
	require.Equal(t, preparer, saved["proposed_by_subject"])
	require.Equal(t, "pending", saved["status"])
	require.Equal(t, float64(1), saved["version"])
	require.Nil(t, saved["decided_by_subject"])
	require.NotContains(t, saved, "pharmacist_name")
	require.Equal(t, prepared, call(t, "PUT", path, preparer, in, 200), "retry returns the original proposal receipt, not another publication")
	require.Equal(t, prepared, call(t, "GET", path+"/commands/"+binding, preparer, nil, 200), "the initial response is the exact recoverable receipt")
	changed := maps.Clone(in)
	changed["reason"] = "Synthetic changed-content replay"
	call(t, "PUT", path, preparer, changed, 409)
	var publications int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("tenant_id=? AND subject=? AND payload->'data'->>'binding_id'=?", testutil.TenantA, pharmacycontract.SubjectStaffDepartmentBindingChanged, binding).Count(&publications).Error
	}))
	require.EqualValues(t, 1, publications)
	return &pharmacyBindingPreparation{base: base, path: path, preparer: preparer, pending: saved}
}

func assertPharmacyDepartmentBindingReview(t *testing.T, db *tenantdb.DB, call func(*testing.T, string, string, string, any, int) map[string]any, prepared *pharmacyBindingPreparation, administrator, reviewer, reviewerStaff, department, revision string, allowProposerReview func()) {
	t.Helper()
	require.NotNil(t, prepared, "include the preceding normal preparer scenario")
	call(t, "PUT", prepared.base+"/access/"+reviewer, administrator, map[string]any{"level": "editor"}, 200)
	command := func(operation string, version int) map[string]any {
		return map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "operation": operation, "expected_version": version, "reason": "Synthetic independent relationship decision", "attest_department_responsibility": operation == "approve"}
	}
	call(t, "POST", prepared.path+"/commands", reviewer, command("approve", 1), 409)
	rejected := call(t, "POST", prepared.path+"/commands", reviewer, command("reject", 1), 200)["binding"].(map[string]any)
	require.Equal(t, "rejected", rejected["status"], "stale structure prevents approval but does not trap a pending proposal")
	path := prepared.base + "/hospitals/" + prepared.pending["hospital_id"].(string) + "/staff-department-bindings/" + uuid.Must(uuid.NewV7()).String()
	in := map[string]any{"department_id": department, "department_revision_id": revision, "replaces_binding_id": nil, "replaces_binding_version": nil, "reason": "Synthetic current independently reviewed relationship"}
	call(t, "PUT", path, reviewer, in, 403)
	pending := call(t, "PUT", path, prepared.preparer, in, 200)["binding"].(map[string]any)
	decision := command("approve", 1)
	call(t, "POST", path+"/commands", prepared.preparer, decision, 403)
	allowProposerReview()
	call(t, "POST", path+"/commands", prepared.preparer, decision, 409)
	approved := call(t, "POST", path+"/commands", reviewer, decision, 200)
	require.Len(t, approved["authorizations"], 2, "the receipt retains the independently approved action and Staff-source assignments used at commit")
	observedPermissions := []string{}
	for _, authorization := range approved["authorizations"].([]any) {
		entry := authorization.(map[string]any)
		require.NotEmpty(t, entry["assignment_id"])
		require.Greater(t, entry["assignment_version"].(float64), float64(0))
		require.Greater(t, entry["scope_epoch"].(float64), float64(0))
		require.Greater(t, entry["membership_epoch"].(float64), float64(0))
		observedPermissions = append(observedPermissions, entry["permission"].(string))
	}
	require.ElementsMatch(t, []string{"pharmacy.staff_binding.review", "workforce.staff_actor.use"}, observedPermissions)
	row := approved["binding"].(map[string]any)
	require.Equal(t, pending["id"], row["id"])
	require.Equal(t, "approved", row["status"])
	require.Equal(t, float64(2), row["version"])
	require.Equal(t, reviewer, row["decided_by_subject"])
	require.Equal(t, reviewerStaff, row["reviewer_staff_id"])
	require.Equal(t, approved, call(t, "POST", path+"/commands", reviewer, decision, 200))
	require.Equal(t, approved, call(t, "GET", path+"/commands/"+decision["id"].(string), reviewer, nil, 200))
	changed := maps.Clone(decision)
	changed["reason"] = "Synthetic conflicting review retry"
	call(t, "POST", path+"/commands", reviewer, changed, 409)
	current := call(t, "GET", path, prepared.preparer, nil, 200)
	require.Equal(t, "current", current["source_state"])
	require.Equal(t, row, current["binding"])
	collection := path[:strings.LastIndex(path, "/")]
	page := call(t, "GET", collection+"?limit=1", reviewer, nil, 200)
	require.Len(t, page["data"], 1)
	require.Equal(t, row["id"], page["data"].([]any)[0].(map[string]any)["id"])
	cursor := page["page"].(map[string]any)["next_cursor"].(string)
	require.NotEmpty(t, cursor)
	older := call(t, "GET", collection+"?limit=1&cursor="+url.QueryEscape(cursor), reviewer, nil, 200)
	require.Len(t, older["data"], 1)
	require.Equal(t, prepared.pending["id"], older["data"].([]any)[0].(map[string]any)["id"])
	call(t, "GET", collection+"?limit=51", reviewer, nil, 400)
	call(t, "GET", collection+"?purpose=operating", reviewer, nil, 400)
	history := call(t, "GET", path+"/history?limit=50", reviewer, nil, 200)["data"].([]any)
	actions := []string{}
	for _, entry := range history {
		audit := entry.(map[string]any)
		require.Equal(t, row["id"], audit["binding_id"])
		require.Greater(t, audit["recorded_version"].(float64), float64(0))
		require.Len(t, audit["authorizations"], 2)
		actions = append(actions, audit["action"].(string))
	}
	require.Subset(t, actions, []string{"propose", "approve"})
	ended := call(t, "POST", path+"/commands", reviewer, command("end", 2), 200)["binding"].(map[string]any)
	require.Equal(t, "ended", ended["status"])
	require.Equal(t, float64(3), ended["version"])
	require.Equal(t, row["decided_by_subject"], ended["decided_by_subject"])
	require.Equal(t, row["decided_at"], ended["decided_at"])
	require.NotNil(t, ended["ended_at"])
	require.Equal(t, "not_applicable", call(t, "GET", path, reviewer, nil, 200)["source_state"])
	require.Equal(t, approved, call(t, "GET", path+"/commands/"+decision["id"].(string), reviewer, nil, 200), "ending preserves original approval evidence")
	t.Run("sibling outlet and organization paths cannot disclose a relationship", func(t *testing.T) {
		for _, hidden := range []string{
			strings.Replace(path, prepared.pending["outlet_id"].(string), uuid.Must(uuid.NewV7()).String(), 1),
			strings.Replace(path, prepared.pending["pharmacy_id"].(string), uuid.Must(uuid.NewV7()).String(), 1),
		} {
			before := postingSourceUseEffects(t, db, testutil.TenantA, reviewer)
			for _, endpoint := range []string{hidden, hidden + "/history?limit=50", hidden + "/commands/" + decision["id"].(string)} {
				call(t, "GET", endpoint, reviewer, nil, 404)
			}
			require.Equal(t, before, postingSourceUseEffects(t, db, testutil.TenantA, reviewer))
		}
	})

	t.Run("replacement retains both relationships and one correlated command", func(t *testing.T) {
		firstPath := path[:strings.LastIndex(path, "/")+1] + uuid.Must(uuid.NewV7()).String()
		first := call(t, "PUT", firstPath, prepared.preparer, in, 200)["binding"].(map[string]any)
		firstDecision := command("approve", 1)
		firstApproved := call(t, "POST", firstPath+"/commands", reviewer, firstDecision, 200)
		overlapPath := path[:strings.LastIndex(path, "/")+1] + uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", overlapPath, prepared.preparer, in, 200)
		call(t, "POST", overlapPath+"/commands", reviewer, command("approve", 1), 409)
		replacementPath := path[:strings.LastIndex(path, "/")+1] + uuid.Must(uuid.NewV7()).String()
		replacement := maps.Clone(in)
		replacement["replaces_binding_id"], replacement["replaces_binding_version"] = first["id"], 2
		call(t, "PUT", replacementPath, prepared.preparer, replacement, 200)
		replaceDecision := command("approve", 1)
		replaced := call(t, "POST", replacementPath+"/commands", reviewer, replaceDecision, 200)
		require.Equal(t, first["id"], replaced["replaced_binding_id"])
		require.Equal(t, float64(3), replaced["replaced_binding_version"])
		require.Equal(t, "approved", replaced["binding"].(map[string]any)["status"])
		old := call(t, "GET", firstPath, reviewer, nil, 200)
		require.Equal(t, "not_applicable", old["source_state"])
		require.Equal(t, "ended", old["binding"].(map[string]any)["status"])
		require.Equal(t, float64(3), old["binding"].(map[string]any)["version"])
		require.Equal(t, firstApproved, call(t, "GET", firstPath+"/commands/"+firstDecision["id"].(string), reviewer, nil, 200))
		require.Equal(t, replaced, call(t, "POST", replacementPath+"/commands", reviewer, replaceDecision, 200))
		var events int64
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Table("outbox_events").Where("subject=? AND payload->'data'->>'command_id'=? AND payload->'data'->>'replaced_binding_id'=? AND payload->'data'->>'replaced_binding_version'='3'", pharmacycontract.SubjectStaffDepartmentBindingChanged, replaceDecision["id"], first["id"]).Count(&events).Error
		}))
		require.EqualValues(t, 1, events)
	})
	t.Run("failed disclosure evidence withholds the record and actor audit", func(t *testing.T) {
		apply := func(sql string) {
			t.Helper()
			require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "synthetic_binding_read_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
		}
		clear := func() {
			apply(`ALTER TABLE pharmacy_staff_binding_audit DROP CONSTRAINT IF EXISTS synthetic_binding_read_audit`)
		}
		t.Cleanup(clear)
		before := postingSourceUseEffects(t, db, testutil.TenantA, reviewer)
		apply(`ALTER TABLE pharmacy_staff_binding_audit ADD CONSTRAINT synthetic_binding_read_audit CHECK(action<>'read') NOT VALID`)
		denied := call(t, "GET", path, reviewer, nil, 503)
		require.NotContains(t, denied, "binding")
		require.Equal(t, before, postingSourceUseEffects(t, db, testutil.TenantA, reviewer))
		for _, replay := range []struct {
			name, method, path, subject string
			body                        any
		}{
			{"receipt read", "GET", path + "/commands/" + decision["id"].(string), reviewer, nil},
			{"approval replay", "POST", path + "/commands", reviewer, decision},
			{"proposal replay", "PUT", path, prepared.preparer, in},
			{"relationship list", "GET", collection + "?limit=50", reviewer, nil},
			{"relationship history", "GET", path + "/history?limit=50", reviewer, nil},
		} {
			t.Run(replay.name, func(t *testing.T) {
				before := postingSourceUseEffects(t, db, testutil.TenantA, replay.subject)
				denied := call(t, replay.method, replay.path, replay.subject, replay.body, 503)
				require.NotContains(t, denied, "binding")
				require.Equal(t, before, postingSourceUseEffects(t, db, testutil.TenantA, replay.subject))
			})
		}
		clear()
		require.Equal(t, "ended", call(t, "GET", path, reviewer, nil, 200)["binding"].(map[string]any)["status"])
	})
	t.Run("outbox failure rolls back a proposal and its Staff source evidence", func(t *testing.T) {
		apply := func(sql string) {
			t.Helper()
			require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "synthetic_binding_outbox_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
		}
		clear := func() { apply(`ALTER TABLE outbox_events DROP CONSTRAINT IF EXISTS synthetic_binding_outbox`) }
		t.Cleanup(clear)
		fresh := path[:strings.LastIndex(path, "/")+1] + uuid.Must(uuid.NewV7()).String()
		before := postingSourceUseEffects(t, db, testutil.TenantA, prepared.preparer)
		apply(`ALTER TABLE outbox_events ADD CONSTRAINT synthetic_binding_outbox CHECK(subject<>'` + pharmacycontract.SubjectStaffDepartmentBindingChanged + `') NOT VALID`)
		call(t, "PUT", fresh, prepared.preparer, in, 503)
		call(t, "GET", fresh, prepared.preparer, nil, 404)
		require.Equal(t, before, postingSourceUseEffects(t, db, testutil.TenantA, prepared.preparer))
		clear()
		require.Equal(t, "pending", call(t, "PUT", fresh, prepared.preparer, in, 200)["binding"].(map[string]any)["status"])
	})
	t.Run("revoking the outlet grant withholds retained relationship receipts", func(t *testing.T) {
		call(t, "DELETE", prepared.base+"/access/"+reviewer, administrator, nil, 200)
		before := postingSourceUseEffects(t, db, testutil.TenantA, reviewer)
		call(t, "GET", path, reviewer, nil, 404)
		call(t, "GET", path+"/commands/"+decision["id"].(string), reviewer, nil, 404)
		call(t, "GET", collection+"?limit=50", reviewer, nil, 404)
		call(t, "GET", path+"/history?limit=50", reviewer, nil, 404)
		call(t, "POST", path+"/commands", reviewer, decision, 404)
		require.Equal(t, before, postingSourceUseEffects(t, db, testutil.TenantA, reviewer))
		call(t, "PUT", prepared.base+"/access/"+reviewer, administrator, map[string]any{"level": "editor"}, 200)
		require.Equal(t, approved, call(t, "GET", path+"/commands/"+decision["id"].(string), reviewer, nil, 200))
	})
}
