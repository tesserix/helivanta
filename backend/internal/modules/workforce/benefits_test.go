package workforce_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type benefitFixture struct {
	t                     *testing.T
	r                     *gin.Engine
	db                    *tenantdb.DB
	hospital, base, other string
	spouse, child         string
}

func newBenefitFixture(t *testing.T) *benefitFixture {
	t.Helper()
	return newBenefitFixtureWith(t, nil)
}

func newBenefitFixtureWith(t *testing.T, files *attachments.GCS) *benefitFixture {
	t.Helper()
	benefits := []authz.Permission{workforce.PermBenefitsView, workforce.PermBenefitsManage, workforce.PermBenefitsApprove}
	admin := append([]authz.Permission{workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermMasterManage}, benefits...)
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"hr": testutil.TenantA, "approver": testutil.TenantA, "manage-only": testutil.TenantA, "reader": testutil.TenantA, "staff-only": testutil.TenantA,
			"exporter": testutil.TenantA, "export-ungranted": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{"hr": admin, "approver": benefits, "manage-only": {workforce.PermBenefitsView, workforce.PermBenefitsManage}, "reader": benefits,
			"staff-only": {workforce.PermEnrollmentManage, workforce.PermDocumentManage, workforce.PermPayrollView, workforce.PermCredentialReview},
			"exporter":   {workforce.PermBenefitsExport}, "export-ungranted": {workforce.PermBenefitsExport}, "foreign": append(admin, workforce.PermBenefitsExport)},
		Modules: []platform.Module{workforce.New()}, Documents: files,
	})
	f := &benefitFixture{t: t, r: r, db: db, hospital: uuid.NewString()}
	f.base = "/v1/workforce/hospitals/" + f.hospital
	f.other = "/v1/workforce/hospitals/" + uuid.NewString()
	for _, base := range []string{f.base, f.other} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base, "hr", `{"name":"Synthetic Benefits Hospital"}`).Code)
	}
	for user, level := range map[string]string{"hr": "manager", "approver": "manager", "manage-only": "manager", "staff-only": "manager", "reader": "reader", "exporter": "reader"} {
		require.Equal(t, 200, testutil.Do(r, "PUT", f.base+"/access/user-"+user, "hr", `{"level":"`+level+`"}`).Code)
	}
	f.spouse = f.master("SPOUSE", "Synthetic spouse")
	f.child = f.master("CHILD", "Synthetic child")
	return f
}

func (f *benefitFixture) call(method, path, user string, body any) (int, map[string]any) {
	f.t.Helper()
	data := ""
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(f.t, err)
		data = string(raw)
	}
	w := testutil.Do(f.r, method, path, user, data)
	if w.Code != 403 && w.Code != 404 {
		require.Equal(f.t, "no-store", w.Header().Get("Cache-Control"), path)
	}
	var out map[string]any
	require.NoError(f.t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return w.Code, out
}

func (f *benefitFixture) ok(method, path, user string, body any) map[string]any {
	f.t.Helper()
	code, out := f.call(method, path, user, body)
	require.Equal(f.t, 200, code, out)
	return out
}

func (f *benefitFixture) conflict(reason, method, path, user string, body any) {
	f.t.Helper()
	code, out := f.call(method, path, user, body)
	require.Equal(f.t, 409, code, out)
	require.Equal(f.t, reason, out["reason"], out)
}

// master creates an active relationship master and returns its id.
func (f *benefitFixture) master(code, name string) string {
	f.t.Helper()
	id := uuid.NewString()
	f.ok("PUT", f.base+"/hr/masters/"+id, "hr", map[string]any{"expected_version": 0, "kind": "relationship", "code": code, "name": name, "description": "", "active": true})
	return id
}

func (f *benefitFixture) masterRevision(id string) string {
	f.t.Helper()
	out := f.ok("GET", f.base+"/hr/masters/"+id+"/history?limit=1", "hr", nil)
	return out["data"].([]any)[0].(map[string]any)["id"].(string)
}

func (f *benefitFixture) staff(code string) string {
	f.t.Helper()
	id := uuid.NewString()
	f.ok("PUT", f.base+"/staff/"+id+"/enrollment", "hr", enrollmentBody(0, code))
	return id
}

func planBody(version int, code, from string, until any, relationships ...string) map[string]any {
	if relationships == nil {
		relationships = []string{}
	}
	body := map[string]any{"expected_version": version, "code": code, "name": "Synthetic " + code, "kind": "health", "description": "Synthetic group cover",
		"covers_dependents": len(relationships) > 0, "covered_relationships": relationships, "effective_from": from, "status": "active"}
	if until != nil {
		body["effective_until"] = until
	}
	return body
}

func (f *benefitFixture) plan(code string, relationships ...string) (string, map[string]any) {
	f.t.Helper()
	id := uuid.NewString()
	return id, f.ok("PUT", f.base+"/hr/benefits/plans/"+id, "hr", planBody(0, code, "2020-01-01", nil, relationships...))
}

func dependentBody(version int, name, relationshipRevision, status string) map[string]any {
	return map[string]any{"expected_version": version, "full_name": name, "relationship_revision_id": relationshipRevision, "birth_date": "2015-06-01", "phone": "", "status": status, "reason": "Synthetic declaration form"}
}

func (f *benefitFixture) dependentPath(staff, dependent string) string {
	return f.base + "/staff/" + staff + "/dependents/" + dependent
}

func (f *benefitFixture) proposalPath(staff, enrollment, proposal, suffix string) string {
	return f.base + "/staff/" + staff + "/benefit-enrollments/" + enrollment + "/proposals/" + proposal + suffix
}

func proposalBody(version int, plan string, dependent any, from string, until any) map[string]any {
	body := map[string]any{"expected_version": version, "plan_id": plan, "coverage_from": from, "eligibility_basis": "Synthetic eligibility checked"}
	if dependent != nil {
		body["dependent_id"] = dependent
	}
	if until != nil {
		body["coverage_until"] = until
	}
	return body
}

// cover proposes coverage as hr and approves it as approver, returning the enrollment id.
func (f *benefitFixture) cover(staff, plan string, dependent any, from string, until any) string {
	f.t.Helper()
	enrollment, proposal := uuid.NewString(), uuid.NewString()
	f.ok("PUT", f.proposalPath(staff, enrollment, proposal, ""), "hr", proposalBody(0, plan, dependent, from, until))
	f.ok("POST", f.proposalPath(staff, enrollment, proposal, "/decision"), "approver", map[string]any{"expected_version": 0, "decision": "approve", "note": ""})
	return enrollment
}

func (f *benefitFixture) payloads(subject string) []map[string]any {
	f.t.Helper()
	var raw []string
	require.NoError(f.t, f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=?", subject).Order("created_at").Pluck("payload", &raw).Error
	}))
	result := make([]map[string]any, len(raw))
	for i, p := range raw {
		var event map[string]any
		require.NoError(f.t, json.Unmarshal([]byte(p), &event))
		data := event["data"]
		if s, ok := data.(string); ok {
			require.NoError(f.t, json.Unmarshal([]byte(s), &data))
		}
		result[i] = data.(map[string]any)
	}
	return result
}

func (f *benefitFixture) audits(action string) int {
	f.t.Helper()
	var n int
	require.NoError(f.t, f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-hr',true),set_config('app.workforce_hospital',?,true)`, f.hospital).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT count(*) FROM workforce_audit WHERE action=?`, action).Scan(&n).Error
	}))
	return n
}

// auditChanges returns the change sets recorded under one action, oldest first.
func (f *benefitFixture) auditChanges(action string) []map[string]any {
	f.t.Helper()
	var raw []string
	require.NoError(f.t, f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-hr',true),set_config('app.workforce_hospital',?,true)`, f.hospital).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT changes::text FROM workforce_audit WHERE action=? ORDER BY created_at,id`, action).Scan(&raw).Error
	}))
	result := make([]map[string]any, len(raw))
	for i, changes := range raw {
		require.NoError(f.t, json.Unmarshal([]byte(changes), &result[i]))
	}
	return result
}

func TestBenefitPlansAreVersionedAndRetireOnce(t *testing.T) {
	f := newBenefitFixture(t)
	plan := uuid.NewString()
	path := f.base + "/hr/benefits/plans/" + plan
	body := planBody(0, " syn-health ", "2020-01-01", nil, f.child, f.spouse)
	saved := f.ok("PUT", path, "hr", body)
	require.Equal(t, "SYN-HEALTH", saved["code"], "code is trimmed and upper-cased")
	require.EqualValues(t, 1, saved["version"])
	require.Len(t, saved["covered_relationships"], 2)
	require.Equal(t, saved["revision_id"], f.ok("PUT", path, "hr", body)["revision_id"], "an unchanged retry returns the current plan")

	f.conflict("version_conflict", "PUT", path, "hr", planBody(0, "SYN-HEALTH", "2021-01-01", nil, f.child))
	f.conflict("duplicate_plan", "PUT", f.base+"/hr/benefits/plans/"+uuid.NewString(), "hr", planBody(0, "SYN-HEALTH", "2020-01-01", nil))
	for name, invalid := range map[string]map[string]any{
		"dependents without relationships": func() map[string]any {
			b := planBody(0, "SYN-X", "2020-01-01", nil)
			b["covers_dependents"] = true
			return b
		}(),
		"relationships without dependents": func() map[string]any {
			b := planBody(0, "SYN-X", "2020-01-01", nil, f.child)
			b["covers_dependents"] = false
			return b
		}(),
		"reversed period":           planBody(0, "SYN-X", "2020-01-01", "2019-12-31"),
		"retired without end date":  func() map[string]any { b := planBody(0, "SYN-X", "2020-01-01", nil); b["status"] = "retired"; return b }(),
		"not a relationship master": planBody(0, "SYN-X", "2020-01-01", nil, uuid.NewString()),
		"duplicate relationship":    planBody(0, "SYN-X", "2020-01-01", nil, f.child, f.child),
	} {
		code, out := f.call("PUT", f.base+"/hr/benefits/plans/"+uuid.NewString(), "hr", invalid)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}

	changed := f.ok("PUT", path, "hr", planBody(1, "SYN-HEALTH", "2020-01-01", "2030-12-31", f.child))
	require.EqualValues(t, 2, changed["version"])
	f.conflict("version_conflict", "PUT", path, "hr", planBody(1, "SYN-HEALTH", "2020-01-01", "2029-12-31", f.child))
	retire := planBody(2, "SYN-HEALTH", "2020-01-01", "2026-12-31", f.child)
	retire["status"] = "retired"
	require.Equal(t, "retired", f.ok("PUT", path, "hr", retire)["status"])
	f.conflict("plan_retired", "PUT", path, "hr", planBody(3, "SYN-HEALTH", "2020-01-01", "2027-12-31", f.child))

	history := f.ok("GET", path+"/history?limit=2", "reader", nil)
	require.Len(t, history["data"], 2)
	require.Equal(t, true, history["page"].(map[string]any)["has_more"])
	require.Equal(t, "retired", history["data"].([]any)[0].(map[string]any)["status"], "newest revision first")
	listed := f.ok("GET", f.base+"/hr/benefits/plans", "reader", nil)
	require.Len(t, listed["data"], 1)

	events := f.payloads(workforce.BenefitPlanChanged)
	require.Len(t, events, 3)
	require.Equal(t, map[string]any{"hospital_id": f.hospital, "plan_id": plan, "revision_id": events[2]["revision_id"], "version": float64(3), "status": "retired"}, events[2])
	require.Equal(t, 3, f.audits("workforce.benefits.plan.changed"))
	require.Equal(t, 1, f.audits("workforce.benefits.plans.read"))
	require.Equal(t, 1, f.audits("workforce.benefits.plan_history.read"))
}

func TestDependentsUseCurrentRelationshipMasters(t *testing.T) {
	f := newBenefitFixture(t)
	staff := f.staff("DEP-01")
	child := f.masterRevision(f.child)
	dep := uuid.NewString()
	path := f.dependentPath(staff, dep)
	body := dependentBody(0, " Synthetic Child ", child, "active")
	saved := f.ok("PUT", path, "hr", body)
	require.Equal(t, "Synthetic Child", saved["full_name"])
	require.Equal(t, f.child, saved["relationship_master_id"])
	require.Equal(t, "Synthetic child", saved["relationship_name"])
	require.Equal(t, saved["revision_id"], f.ok("PUT", path, "hr", body)["revision_id"], "an unchanged retry returns the current dependent")

	f.conflict("duplicate_dependent", "PUT", f.dependentPath(staff, uuid.NewString()), "hr", dependentBody(0, "synthetic child", child, "active"))
	f.conflict("version_conflict", "PUT", path, "hr", dependentBody(0, "Synthetic Child Renamed", child, "active"))
	for name, invalid := range map[string]map[string]any{
		"future birth": func() map[string]any {
			b := dependentBody(0, "Synthetic Future", child, "active")
			b["birth_date"] = "2199-01-01"
			return b
		}(),
		"bad phone": func() map[string]any {
			b := dependentBody(0, "Synthetic Phone", child, "active")
			b["phone"] = "12345"
			return b
		}(),
		"unknown master":   dependentBody(0, "Synthetic Unknown", uuid.NewString(), "active"),
		"created removed":  dependentBody(0, "Synthetic Removed", child, "removed"),
		"not relationship": dependentBody(0, "Synthetic Grade", f.masterRevision(f.masterOfKind("grade")), "active"),
		"missing reason": func() map[string]any {
			b := dependentBody(0, "Synthetic Reason", child, "active")
			b["reason"] = " "
			return b
		}(),
		"government number": func() map[string]any {
			b := dependentBody(0, "Synthetic Aadhaar", child, "active")
			b["aadhaar"] = "1234"
			return b
		}(),
	} {
		code, out := f.call("PUT", f.dependentPath(staff, uuid.NewString()), "hr", invalid)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}

	f.ok("PUT", f.base+"/hr/masters/"+f.spouse, "hr", map[string]any{"expected_version": 1, "kind": "relationship", "code": "SPOUSE", "name": "Synthetic spouse", "description": "", "active": false})
	code, out := f.call("PUT", f.dependentPath(staff, uuid.NewString()), "hr", dependentBody(0, "Synthetic Spouse", f.masterRevision(f.spouse), "active"))
	require.Equal(t, 400, code, "a retired relationship cannot be cited: %v", out)

	removed := f.ok("PUT", path, "hr", dependentBody(1, "Synthetic Child", child, "removed"))
	require.Equal(t, "removed", removed["status"])
	history := f.ok("GET", path+"/history", "reader", nil)
	require.Len(t, history["data"], 2)
	require.Equal(t, "Synthetic declaration form", history["data"].([]any)[0].(map[string]any)["reason"])

	f.ok("PUT", f.base+"/staff/"+staff+"/enrollment", "hr", func() map[string]any {
		b := enrollmentBody(1, "DEP-01")
		b["staff"].(map[string]any)["employment_status"] = "inactive"
		return b
	}())
	f.conflict("staff_inactive", "PUT", f.dependentPath(staff, uuid.NewString()), "hr", dependentBody(0, "Synthetic Late", child, "active"))

	events := f.payloads(workforce.DependentChanged)
	require.Len(t, events, 2)
	for _, event := range events {
		raw, err := json.Marshal(event)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "Synthetic", "names never leave the API")
		require.NotContains(t, string(raw), "2015-06-01", "birth dates never leave the API")
	}
}

func (f *benefitFixture) masterOfKind(kind string) string {
	f.t.Helper()
	id := uuid.NewString()
	f.ok("PUT", f.base+"/hr/masters/"+id, "hr", map[string]any{"expected_version": 0, "kind": kind, "code": "SYN-" + strings.ToUpper(kind), "name": "Synthetic " + kind, "description": "", "active": true})
	return id
}

func TestBenefitCoverageNeedsASecondPersonAndNeverOverlaps(t *testing.T) {
	f := newBenefitFixture(t)
	staff := f.staff("COV-01")
	plan, _ := f.plan("SYN-COVER", f.child)
	enrollment, proposal := uuid.NewString(), uuid.NewString()
	path := f.proposalPath(staff, enrollment, proposal, "")
	body := proposalBody(0, plan, nil, "2024-01-01", nil)
	saved := f.ok("PUT", path, "hr", body)
	require.Equal(t, "pending", saved["status"])
	require.Equal(t, saved["created_at"], f.ok("PUT", path, "hr", body)["created_at"], "an identical replay returns the saved proposal")
	f.conflict("proposal_conflict", "PUT", path, "hr", proposalBody(0, plan, nil, "2024-02-01", nil))
	f.conflict("proposal_pending", "PUT", f.proposalPath(staff, enrollment, uuid.NewString(), ""), "approver", proposalBody(0, plan, nil, "2024-02-01", nil))
	f.conflict("coverage_overlap", "PUT", f.proposalPath(staff, uuid.NewString(), uuid.NewString(), ""), "approver", proposalBody(0, plan, nil, "2025-01-01", "2025-12-31"))

	decide := func(user string, version int, decision, note string) (int, map[string]any) {
		return f.call("POST", f.proposalPath(staff, enrollment, proposal, "/decision"), user, map[string]any{"expected_version": version, "decision": decision, "note": note})
	}
	code, out := decide("hr", 0, "approve", "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "self_review", out["reason"])
	code, out = decide("manage-only", 0, "approve", "")
	require.Equal(t, 403, code, out)
	code, out = decide("approver", 0, "reject", " ")
	require.Equal(t, 400, code, "a rejection needs a note: %v", out)
	code, out = decide("approver", 1, "approve", "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "version_conflict", out["reason"])
	f.conflict("not_proposer", "POST", f.proposalPath(staff, enrollment, proposal, "/withdraw"), "approver", map[string]any{"note": ""})

	code, out = decide("approver", 0, "approve", "Synthetic payslip checked")
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["approved_version"])
	code, again := decide("approver", 0, "approve", "Synthetic payslip checked")
	require.Equal(t, 200, code, again)
	require.Equal(t, out["decided_at"], again["decided_at"], "an identical decision replay changes nothing")

	listed := f.ok("GET", f.base+"/staff/"+staff+"/benefit-enrollments", "reader", nil)
	require.Len(t, listed["data"], 1)
	coverage := listed["data"].([]any)[0].(map[string]any)["coverage"].(map[string]any)
	require.Equal(t, "2024-01-01", coverage["coverage_from"])
	require.Equal(t, proposal, coverage["proposal_id"])
	original := coverage["original_plan_revision_id"]

	f.conflict("nothing_to_change", "PUT", f.proposalPath(staff, enrollment, uuid.NewString(), ""), "hr", proposalBody(1, plan, nil, "2024-01-01", nil))
	f.conflict("coverage_overlap", "PUT", f.proposalPath(staff, uuid.NewString(), uuid.NewString(), ""), "hr", proposalBody(0, plan, nil, "2030-01-01", nil))
	code, out = f.call("PUT", f.proposalPath(staff, enrollment, uuid.NewString(), ""), "hr", proposalBody(1, uuid.NewString(), nil, "2024-01-01", "2024-12-31"))
	require.Equal(t, 400, code, "an enrollment keeps its plan: %v", out)
	f.conflict("coverage_outside_plan", "PUT", f.proposalPath(staff, uuid.NewString(), uuid.NewString(), ""), "hr", func() map[string]any {
		other, _ := f.plan("SYN-LATE")
		return proposalBody(0, other, nil, "2019-12-31", nil)
	}())

	// A plan change between proposal and approval forces a fresh proposal.
	amend := uuid.NewString()
	f.ok("PUT", f.proposalPath(staff, enrollment, amend, ""), "hr", proposalBody(1, plan, nil, "2024-01-01", "2026-06-30"))
	f.ok("PUT", f.base+"/hr/benefits/plans/"+plan, "hr", func() map[string]any {
		b := planBody(1, "SYN-COVER", "2020-01-01", nil, f.child)
		b["description"] = "Synthetic revised"
		return b
	}())
	f.conflict("plan_changed", "POST", f.proposalPath(staff, enrollment, amend, "/decision"), "approver", map[string]any{"expected_version": 1, "decision": "approve", "note": ""})
	f.ok("POST", f.proposalPath(staff, enrollment, amend, "/decision"), "approver", map[string]any{"expected_version": 1, "decision": "reject", "note": "Synthetic stale proposal"})

	f.conflict("coverage_outside_plan", "PUT", f.base+"/hr/benefits/plans/"+plan, "hr", func() map[string]any {
		b := planBody(2, "SYN-COVER", "2020-01-01", "2026-12-31", f.child)
		b["status"] = "retired"
		return b
	}())
	end := uuid.NewString()
	f.ok("PUT", f.proposalPath(staff, enrollment, end, ""), "hr", proposalBody(1, plan, nil, "2024-01-01", "2026-06-30"))
	ended := f.ok("POST", f.proposalPath(staff, enrollment, end, "/decision"), "approver", map[string]any{"expected_version": 1, "decision": "approve", "note": ""})
	require.EqualValues(t, 2, ended["approved_version"])
	retire := planBody(2, "SYN-COVER", "2020-01-01", "2026-12-31", f.child)
	retire["status"] = "retired"
	f.ok("PUT", f.base+"/hr/benefits/plans/"+plan, "hr", retire)
	f.conflict("plan_retired", "PUT", f.proposalPath(staff, uuid.NewString(), uuid.NewString(), ""), "hr", proposalBody(0, plan, nil, "2026-07-01", "2026-12-31"))
	shorter := uuid.NewString()
	f.ok("PUT", f.proposalPath(staff, enrollment, shorter, ""), "hr", proposalBody(2, plan, nil, "2024-01-01", "2025-12-31"))
	require.EqualValues(t, 3, f.ok("POST", f.proposalPath(staff, enrollment, shorter, "/decision"), "approver", map[string]any{"expected_version": 2, "decision": "approve", "note": ""})["approved_version"], "a retired plan may still shorten coverage")

	listed = f.ok("GET", f.base+"/staff/"+staff+"/benefit-enrollments", "hr", nil)
	coverage = listed["data"].([]any)[0].(map[string]any)["coverage"].(map[string]any)
	require.Equal(t, original, coverage["original_plan_revision_id"], "the original plan revision stays pinned")
	require.Equal(t, "2025-12-31", coverage["coverage_until"])
	history := f.ok("GET", f.base+"/staff/"+staff+"/benefit-enrollments/"+enrollment+"/history?limit=10", "reader", nil)
	require.Len(t, history["data"], 4)

	events := f.payloads(workforce.BenefitEnrollmentChanged)
	require.Equal(t, map[string]any{"hospital_id": f.hospital, "staff_id": staff, "enrollment_id": enrollment, "proposal_id": shorter, "status": "approved", "version": float64(3)}, events[len(events)-1])
	require.Equal(t, 4, f.audits("workforce.benefits.enrollment.pending"))
	require.Equal(t, 3, f.audits("workforce.benefits.enrollment.approved"))
}

func TestDependentCoverageFollowsTheirRelationship(t *testing.T) {
	f := newBenefitFixture(t)
	staff := f.staff("FAM-01")
	plan, _ := f.plan("SYN-FAMILY", f.child)
	child, spouse := f.masterRevision(f.child), f.masterRevision(f.spouse)
	kid, partner := uuid.NewString(), uuid.NewString()
	f.ok("PUT", f.dependentPath(staff, kid), "hr", dependentBody(0, "Synthetic Kid", child, "active"))
	f.ok("PUT", f.dependentPath(staff, partner), "hr", dependentBody(0, "Synthetic Partner", spouse, "active"))

	f.conflict("relationship_not_covered", "PUT", f.proposalPath(staff, uuid.NewString(), uuid.NewString(), ""), "hr", proposalBody(0, plan, partner, "2024-01-01", nil))
	other := f.staff("FAM-02")
	code, out := f.call("PUT", f.proposalPath(other, uuid.NewString(), uuid.NewString(), ""), "hr", proposalBody(0, plan, kid, "2024-01-01", nil))
	require.Equal(t, 404, code, "another staff member's dependent is unknown: %v", out)

	open := f.cover(staff, plan, kid, "2024-01-01", nil)
	f.cover(staff, plan, nil, "2024-01-01", nil)
	f.conflict("dependent_covered", "PUT", f.dependentPath(staff, kid), "hr", dependentBody(1, "Synthetic Kid", child, "removed"))
	f.conflict("dependent_covered", "PUT", f.dependentPath(staff, kid), "hr", dependentBody(1, "Synthetic Kid", spouse, "active"))

	end := uuid.NewString()
	f.ok("PUT", f.proposalPath(staff, open, end, ""), "hr", proposalBody(1, plan, kid, "2024-01-01", "2024-12-31"))
	f.ok("PUT", f.dependentPath(staff, kid), "hr", func() map[string]any {
		b := dependentBody(1, "Synthetic Kid", child, "active")
		b["phone"] = "+919800000001"
		return b
	}())
	f.conflict("dependent_changed", "POST", f.proposalPath(staff, open, end, "/decision"), "approver", map[string]any{"expected_version": 1, "decision": "approve", "note": ""})
	f.ok("POST", f.proposalPath(staff, open, end, "/withdraw"), "hr", map[string]any{"note": "Synthetic stale"})
	again := uuid.NewString()
	f.ok("PUT", f.proposalPath(staff, open, again, ""), "hr", proposalBody(1, plan, kid, "2024-01-01", "2024-12-31"))
	f.ok("POST", f.proposalPath(staff, open, again, "/decision"), "approver", map[string]any{"expected_version": 1, "decision": "approve", "note": ""})
	require.Equal(t, "removed", f.ok("PUT", f.dependentPath(staff, kid), "hr", dependentBody(2, "Synthetic Kid", child, "removed"))["status"], "ended coverage releases the dependent")
	f.conflict("dependent_removed", "PUT", f.proposalPath(staff, uuid.NewString(), uuid.NewString(), ""), "hr", proposalBody(0, plan, kid, "2025-01-01", nil))
}

func TestBenefitsStayInsideTheirOwnScope(t *testing.T) {
	f := newBenefitFixture(t)
	staff := f.staff("ISO-01")
	plan, _ := f.plan("SYN-ISO")
	dep := uuid.NewString()
	f.ok("PUT", f.dependentPath(staff, dep), "hr", dependentBody(0, "Synthetic Iso", f.masterRevision(f.child), "active"))
	enrollment := f.cover(staff, plan, nil, "2024-01-01", nil)

	reads := []string{f.base + "/hr/benefits/plans", f.base + "/hr/benefits/plans/" + plan + "/history", f.dependentPath(staff, "") + "?", f.dependentPath(staff, dep) + "/history",
		f.base + "/staff/" + staff + "/benefit-enrollments", f.base + "/staff/" + staff + "/benefit-enrollments/" + enrollment + "/history"}
	for _, path := range reads {
		path = strings.TrimSuffix(strings.Replace(path, "/?", "", 1), "?")
		code, out := f.call("GET", path, "staff-only", nil)
		require.Equal(t, 403, code, "enrollment and payroll permissions grant no benefits access: %s %v", path, out)
		code, out = f.call("GET", path, "foreign", nil)
		require.Equal(t, 404, code, "%s %v", path, out)
		require.Equal(t, 200, func() int { c, _ := f.call("GET", path, "reader", nil); return c }(), path)
		code, out = f.call("GET", strings.Replace(path, f.base, f.other, 1), "hr", nil)
		require.Equal(t, 404, code, "another hospital: %s %v", path, out)
	}
	code, out := f.call("PUT", f.dependentPath(staff, uuid.NewString()), "reader", dependentBody(0, "Synthetic Reader", f.masterRevision(f.child), "active"))
	require.Equal(t, 404, code, "a reader grant cannot write: %v", out)
	code, out = f.call("GET", f.base+"/staff/"+uuid.NewString()+"/dependents", "hr", nil)
	require.Equal(t, 404, code, out)
	code, out = f.call("GET", f.base+"/hr/benefits/plans?limit=101", "hr", nil)
	require.Equal(t, 400, code, out)

	count := func(flag string) (n int) {
		require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-hr',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true),set_config('app.workforce_benefits',?,true)`, f.hospital, flag).Error; err != nil {
				return err
			}
			return tx.Raw(`SELECT (SELECT count(*) FROM workforce_benefit_plans)+(SELECT count(*) FROM workforce_staff_dependents)+(SELECT count(*) FROM workforce_benefit_enrollments)`).Scan(&n).Error
		}))
		return n
	}
	require.Equal(t, 0, count(""), "the enrollment scope alone sees no benefits data")
	require.Equal(t, 3, count("read"), "the benefits scope sees the same rows")

	require.Error(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-hr',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_benefits','write',true)`, f.hospital).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE workforce_benefit_enrollments SET coverage_until='2024-06-30'`).Error
	}), "coverage changes only through an approved proposal")
	require.Error(t, f.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Exec(`DELETE FROM workforce_staff_dependents`).Error }))
}
