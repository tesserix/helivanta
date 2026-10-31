package globalapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestHRIntakeDelegationPreparesInactiveStaffWithoutManagerGrant(t *testing.T) {
	ctx := t.Context()
	cloud := testinfra.NewGCSProtocol(t)
	documents, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	modules := []platform.Module{&scopedLeaveIAMModule{iam.New(nil)}, workforce.New()}
	registry := platform.NewRegistry()
	for _, module := range modules {
		require.NoError(t, registry.Register(module))
	}
	require.NoError(t, platform.ReconcileTenant(ctx, registry, graph, testutil.TenantA))
	for _, role := range []authz.Role{authz.RoleTenantAdmin, authz.RoleStaffMember} {
		require.NoError(t, graph.GrantTenantRole(ctx, testutil.TenantA, role))
	}
	const maker, checker, intake = "synthetic-intake-maker", "synthetic-intake-checker", "synthetic-intake-operator"
	for _, subject := range []string{maker, checker} {
		require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, subject, authz.RoleTenantAdmin))
	}
	signer, verifier := testutil.NewSessionSignerForTest(t)
	owner, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Writer: graph, Roles: graph, Membership: graph, Resolver: graph,
		Verifier: authn.NewSessionVerifier(verifier), Modules: modules, Documents: documents,
		ScopedAuthorizer: func(db *tenantdb.DB) authz.ScopedAuthorizer { return iam.NewScopedAuthorizer(db, graph) },
	})
	upstream := httptest.NewServer(owner)
	t.Cleanup(upstream.Close)
	gateway, err := New(Config{Development: true, Timeout: 10 * time.Second, Upstreams: map[string]string{"platform": upstream.URL, "workforce": upstream.URL}})
	require.NoError(t, err)
	tokens := map[string]string{}
	for _, subject := range []string{maker, checker, intake} {
		tokens[subject], err = signer.Mint(subject, testutil.TenantA, time.Now(), time.Now().Add(time.Hour))
		require.NoError(t, err)
	}
	request := func(method, path, subject string, body []byte, contentType string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+tokens[subject])
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		r.Header.Set("X-Helivanta-Expected-Subject", subject)
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, r)
		return w
	}
	call := func(method, path, subject string, input any, status int) map[string]any {
		t.Helper()
		var body []byte
		if input != nil {
			body, err = json.Marshal(input)
			require.NoError(t, err)
		}
		w := request(method, path, subject, body, "application/json")
		require.Equal(t, status, w.Code, "%s %s: %s", method, path, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	for _, member := range []struct{ subject, role string }{{maker, "tenant_admin"}, {checker, "tenant_admin"}, {intake, "staff_member"}} {
		call("POST", "/v1/iam/members", maker, map[string]any{"subject": member.subject, "role_key": member.role}, 202)
	}
	require.Eventually(t, func() bool {
		member, e := graph.IsMember(ctx, intake, testutil.TenantA)
		return e == nil && member
	}, 10*time.Second, 20*time.Millisecond)
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	call("PUT", base, maker, map[string]any{"name": "Synthetic Scoped HR Intake Hospital"}, 200)
	for _, subject := range []string{maker, checker} {
		call("PUT", base+"/access/"+subject, maker, map[string]any{"level": "manager"}, 200)
	}
	actor := uuid.NewString()
	call("PUT", base+"/staff/"+actor, maker, map[string]any{"employee_code": "SYN-INTAKE-ACTOR", "display_name": "Synthetic Intake Operator", "job_category": "other", "employment_status": "active"}, 200)
	link := base + "/staff/" + actor + "/login-links/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", link, maker, map[string]any{"subject": intake, "reason": "Synthetic current HR actor binding"}, 200)
	call("POST", link+"/decision", checker, map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic actor identity review"}, 200)
	proposal := "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", proposal, maker, map[string]any{
		"hospital_id": hospital, "subject": intake, "template_key": "workforce.hr_intake.v1",
		"reason": "Synthetic hospital-local HR intake", "valid_from": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}, 200)
	require.Eventually(t, func() bool { return call("GET", proposal, maker, nil, 200)["status"] == "pending_review" }, 10*time.Second, 20*time.Millisecond)
	call("POST", proposal+"/decision", checker, map[string]any{"expected_version": 2, "decision": "approve", "reason": "Independent synthetic HR access approval"}, 200)
	authority := iam.NewScopedAuthorizer(db, graph)
	require.Eventually(t, func() bool {
		for _, permission := range []authz.Permission{workforce.PermEnrollmentManage, workforce.PermDocumentManage} {
			grant, e := authority.AuthorizeScope(ctx, authz.ScopeRequest{TenantID: testutil.TenantA, Subject: intake, HospitalID: hospital, Permission: permission})
			if e != nil || grant.AssignmentID == "" {
				return false
			}
		}
		return true
	}, 10*time.Second, 20*time.Millisecond)
	permissions, err := graph.Resolve(ctx, intake, testutil.TenantA)
	require.NoError(t, err)
	require.Empty(t, permissions, "a scoped HR assignment grants no tenant-wide business permission")
	intakeAssignments := "/v1/iam/intake-assignment-proposals"
	for _, administrator := range []string{maker, checker} {
		assignments := call("GET", intakeAssignments, administrator, nil, 200)["data"].([]any)
		require.Len(t, assignments, 1)
		require.Equal(t, strings.TrimPrefix(proposal, "/v1/iam/assignment-proposals/"), assignments[0].(map[string]any)["id"])
		require.Empty(t, call("GET", "/v1/iam/assignment-proposals", administrator, nil, 200)["data"], "legacy leave list stays closed")
		require.Empty(t, call("GET", "/v1/iam/identity-assignment-proposals", administrator, nil, 200)["data"], "identity list stays closed")
	}
	call("GET", intakeAssignments, intake, nil, 403)
	blockedProposal := "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	blockedBody, err := json.Marshal(map[string]any{
		"hospital_id": hospital, "subject": intake, "template_key": "workforce.hr_intake.v1",
		"reason": "Synthetic retained form from the original account", "valid_from": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)
	changedCommand := httptest.NewRequest("PUT", blockedProposal, bytes.NewReader(blockedBody))
	changedCommand.Header.Set("Authorization", "Bearer "+tokens[checker])
	changedCommand.Header.Set("Content-Type", "application/json")
	changedCommand.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	changedCommand.Header.Set("X-Helivanta-Expected-Subject", maker)
	blockedResponse := httptest.NewRecorder()
	gateway.ServeHTTP(blockedResponse, changedCommand)
	require.Equal(t, 409, blockedResponse.Code, "a retained form cannot create a proposal under a replacement account: %s", blockedResponse.Body.String())
	call("GET", blockedProposal, maker, nil, 404)
	for _, expectedAccounts := range [][]string{{maker}, {""}, {checker, checker}} {
		changed := httptest.NewRequest("GET", intakeAssignments, nil)
		changed.Header.Set("Authorization", "Bearer "+tokens[checker])
		changed.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		for _, account := range expectedAccounts {
			changed.Header.Add("X-Helivanta-Expected-Subject", account)
		}
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, changed)
		require.Equal(t, 409, w.Code, "assignment reads must bind the original account: %s", w.Body.String())
		require.NotContains(t, w.Body.String(), "Synthetic hospital-local HR intake")
	}
	scopesPath := "/v1/workforce/hr-intake-scopes"
	scopes := call("GET", scopesPath, intake, nil, 200)["data"].([]any)
	require.Len(t, scopes, 1)
	require.Equal(t, map[string]any{"hospital_id": hospital, "name": "Synthetic Scoped HR Intake Hospital", "can_prepare_staff": true, "can_manage_staff_documents": true}, scopes[0])
	require.Empty(t, call("GET", scopesPath, checker, nil, 200)["data"], "legacy managers without reviewed HR assignment/link are not scoped HR")
	call("GET", scopesPath+"?limit=21", intake, nil, 400)
	masterPath := base + "/hr/masters/" + uuid.Must(uuid.NewV7()).String()
	masterInput := map[string]any{"expected_version": 0, "kind": "department", "code": "SYN_INTAKE_HR", "name": "Synthetic HR Department", "active": true}
	master := call("PUT", masterPath, maker, masterInput, 200)
	masterChoices := base + "/hr/masters?kind=department&hr_intake=v1"
	choices := call("GET", masterChoices, intake, nil, 200)["data"].([]any)
	require.Len(t, choices, 1)
	require.Equal(t, master["revision_id"], choices[0].(map[string]any)["revision_id"])
	require.Len(t, call("GET", masterPath+"/history?hr_intake=v1", intake, nil, 200)["data"], 1)
	call("PUT", masterPath+"?hr_intake=v1", intake, masterInput, 403)
	call("GET", base+"/hr/masters?kind=department", intake, nil, 404)
	writeErr := db.WithTenant(tenantdb.WithActor(ctx, intake), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_hr_intake','authorized',true),set_config('app.workforce_hr_intake_actor',?,true),set_config('app.workforce_hr_intake_link',?,true),set_config('app.workforce_master','authorized',true)`, intake, hospital, actor, link[strings.LastIndex(link, "/")+1:]).Error; err != nil {
			return err
		}
		return tx.Table("workforce_hr_masters").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, hospital, master["id"]).Updates(map[string]any{"name": "Synthetic forbidden HR setup change", "version": 2}).Error
	})
	var deniedWrite *pgconn.PgError
	require.ErrorAs(t, writeErr, &deniedWrite)
	require.Equal(t, "42501", deniedWrite.Code, "read-only intake context cannot satisfy HR master write policy")
	require.Equal(t, "Synthetic HR Department", call("GET", masterChoices, intake, nil, 200)["data"].([]any)[0].(map[string]any)["name"])
	staff := uuid.NewString()
	legacyPath := base + "/staff/" + staff + "/enrollment"
	path := legacyPath + "?hr_intake=v1"
	input := map[string]any{
		"expected_version": 0,
		"staff":            map[string]any{"employee_code": "SYN-INTAKE-NEW", "display_name": "Synthetic Inactive New Hire", "job_category": "other", "employment_status": "inactive"},
		"profile":          map[string]any{"engagement_type": "contractor", "personal_email": "synthetic-new-hire@example.invalid", "department_revision_id": master["revision_id"]},
		"credentials":      []any{}, "qualifications": []any{}, "experiences": []any{},
	}
	saved := call("PUT", path, intake, input, 200)
	require.Equal(t, "inactive", saved["staff"].(map[string]any)["employment_status"])
	require.Equal(t, "Synthetic HR Department", saved["profile"].(map[string]any)["department"])
	reloaded := call("GET", path, intake, nil, 200)
	require.Equal(t, saved, reloaded)
	for _, privatePath := range []string{scopesPath, base + "/staff?hr_intake=v1", base + "/staff/" + staff + "?hr_intake=v1"} {
		unbound := httptest.NewRequest("GET", privatePath, nil)
		unbound.Header.Set("Authorization", "Bearer "+tokens[intake])
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, unbound)
		require.Equal(t, 409, w.Code, "missing original workspace must deny %s: %s", privatePath, w.Body.String())
		require.NotContains(t, w.Body.String(), "Synthetic Inactive New Hire")
		require.NotContains(t, w.Body.String(), "Synthetic Scoped HR Intake Hospital")
	}
	directory := base + "/staff?hr_intake=v1"
	rows := call("GET", directory, intake, nil, 200)["data"].([]any)
	require.Len(t, rows, 2, "scoped HR must discover Staff without a legacy hospital-reader grant")
	readStaff := call("GET", base+"/staff/"+staff+"?hr_intake=v1", intake, nil, 200)
	require.Equal(t, staff, readStaff["id"])
	require.Equal(t, "Synthetic Inactive New Hire", readStaff["display_name"])
	call("GET", base+"/staff/"+uuid.NewString()+"?hr_intake=v1", intake, nil, 404)
	call("GET", base+"/staff?hr_intake=unknown", intake, nil, 400)
	call("GET", base+"/staff?hr_intake=v1&hr_intake=v1", intake, nil, 400)
	require.Empty(t, call("GET", base+"/staff", intake, nil, 200)["data"], "legacy discovery must remain unchanged")
	unbound := httptest.NewRequest("GET", path, nil)
	unbound.Header.Set("Authorization", "Bearer "+tokens[intake])
	unboundResponse := httptest.NewRecorder()
	gateway.ServeHTTP(unboundResponse, unbound)
	require.Equal(t, 409, unboundResponse.Code, "new intake consumers must bind their original tenant and account")
	require.NotContains(t, unboundResponse.Body.String(), "synthetic-new-hire@example.invalid")
	call("GET", legacyPath, intake, nil, 403)
	call("GET", legacyPath+"?hr_intake=unknown", intake, nil, 400)
	call("GET", legacyPath+"?hr_intake=v1&hr_intake=v1", intake, nil, 400)
	active := map[string]any{
		"expected_version": reloaded["version"],
		"staff":            map[string]any{"employee_code": "SYN-INTAKE-NEW", "display_name": "Synthetic Inactive New Hire", "job_category": "other", "employment_status": "active"},
		"profile":          input["profile"], "credentials": []any{}, "qualifications": []any{}, "experiences": []any{},
	}
	denied := call("PUT", path, intake, active, 409)
	require.Equal(t, "employment_review_required", denied["error"])
	require.Equal(t, saved, call("GET", path, intake, nil, 200), "denied activation retains the original private profile")
	active["expected_version"] = 0
	denied = call("PUT", base+"/staff/"+uuid.NewString()+"/enrollment?hr_intake=v1", intake, active, 409)
	require.Equal(t, "employment_review_required", denied["error"], "new Staff cannot enter active employment through intake")
	staffBase := base + "/staff/" + staff
	source := call("GET", staffBase+"/employment-source?hr_intake=v1", intake, nil, 200)
	period := staffBase + "/employment-periods/" + uuid.Must(uuid.NewV7()).String()
	terms := map[string]any{
		"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0,
		"staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"],
		"engagement_kind": "contractor", "duration": "fixed", "starts_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		"ends_at":           time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339),
		"contracting_party": "Synthetic Contracting Organisation", "sponsor_id": actor,
		"reason": "Synthetic contractor intake", "evidence_reference": "SYN-INTAKE-EVIDENCE",
	}
	draft := call("PUT", period+"?hr_intake=v1", intake, terms, 200)
	require.Equal(t, draft, call("GET", period+"?hr_intake=v1", intake, nil, 200))
	require.Equal(t, draft, call("PUT", period+"?hr_intake=v1", intake, terms, 200), "exact retry retains the prepared draft")
	require.Len(t, call("GET", period+"/history?hr_intake=v1", intake, nil, 200)["data"], 1)
	require.Equal(t, "inactive", call("GET", path, intake, nil, 200)["staff"].(map[string]any)["employment_status"], "prepared contractor terms confer no operational access")
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	file := uuid.Must(uuid.NewV7()).String()
	filePath := staffBase + "/documents/" + file
	call("PUT", filePath+"?hr_intake=v1", intake, map[string]any{
		"record_kind": "enrollment", "record_id": staff, "category": "employment_evidence",
		"filename": "synthetic-intake-contract.png", "content_type": "image/png", "size": imageBytes.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(imageBytes.Bytes())),
	}, 201)
	withContributorContext := func(tenantID, hospitalID string, action func(*gorm.DB) error) error {
		return db.WithTenantCommand(tenantdb.WithActor(ctx, maker), tenantID, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, maker, hospitalID).Error; err != nil {
				return err
			}
			return action(tx)
		})
	}
	readEmploymentContributors := func() []string {
		t.Helper()
		var subjects []string
		require.NoError(t, withContributorContext(testutil.TenantA, hospital, func(tx *gorm.DB) error {
			return tx.Table("workforce_document_contributors").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND document_id=?", testutil.TenantA, hospital, staff, file).Order("subject").Pluck("subject", &subjects).Error
		}))
		return subjects
	}
	require.Equal(t, []string{intake}, readEmploymentContributors(), "employment evidence preparation must retain its attributable contributor for independent employment review")
	require.NoError(t, withContributorContext(testutil.TenantA, hospital, func(tx *gorm.DB) error {
		var captured int64
		if err := tx.Table("workforce_document_contributors").Where("document_id=? AND creation_captured", file).Count(&captured).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, captured, "only attributable document creation establishes complete provenance")
		return nil
	}))
	uploaded := request("PUT", filePath+"/content?hr_intake=v1", intake, imageBytes.Bytes(), "image/png")
	require.Equal(t, 202, uploaded.Code, uploaded.Body.String())
	var database string
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(ctx, testinfra.DocumentWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(ctx, worker, documents, scanner)
	require.NoError(t, err)
	scanState, err := processor.Process(ctx, workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: hospital, StaffID: staff, FileID: file, ScanAttempt: 1})
	require.NoError(t, err)
	require.Equal(t, "available", scanState)
	require.Equal(t, []string{intake}, readEmploymentContributors(), "upload and scanner release must preserve the actual preparer without attributing the scan worker as a human contributor")
	download := request("GET", filePath+"/content?hr_intake=v1", intake, nil, "")
	require.Equal(t, 200, download.Code, download.Body.String())
	require.Equal(t, imageBytes.Bytes(), download.Body.Bytes())
	require.Contains(t, download.Header().Get("Cache-Control"), "no-store")
	evidence := period + "/revisions/1/evidence/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", evidence+"?hr_intake=v1", intake, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "document_id": file, "reason": "Synthetic protected contractor evidence"}, 200)
	boundDownload := request("GET", evidence+"/content?hr_intake=v1", intake, nil, "")
	require.Equal(t, 200, boundDownload.Code, boundDownload.Body.String())
	require.Equal(t, imageBytes.Bytes(), boundDownload.Body.Bytes())
	replacement := "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
	cloud.AfterReleasedRead(func() {
		current := call("GET", proposal, maker, nil, 200)
		call("POST", proposal+"/decision", maker, map[string]any{"expected_version": current["version"], "decision": "withdraw", "reason": "Synthetic in-flight intake authority withdrawal"}, 200)
		call("PUT", replacement, maker, map[string]any{
			"hospital_id": hospital, "subject": intake, "template_key": "workforce.hr_intake.v1",
			"reason": "Synthetic independently reviewed replacement", "valid_from": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		}, 200)
		require.Eventually(t, func() bool { return call("GET", replacement, maker, nil, 200)["status"] == "pending_review" }, 10*time.Second, 20*time.Millisecond)
		call("POST", replacement+"/decision", checker, map[string]any{"expected_version": 2, "decision": "approve", "reason": "Independent synthetic replacement approval"}, 200)
		require.Eventually(t, func() bool {
			grant, err := authority.AuthorizeScope(ctx, authz.ScopeRequest{TenantID: testutil.TenantA, Subject: intake, HospitalID: hospital, Permission: workforce.PermDocumentManage})
			return err == nil && strings.HasSuffix(replacement, grant.AssignmentID) && grant.AssignmentID != ""
		}, 10*time.Second, 20*time.Millisecond)
	})
	inFlight := request("GET", filePath+"/content?hr_intake=v1", intake, nil, "")
	cloud.AfterReleasedRead(nil)
	call("GET", path, intake, nil, 200)
	require.Equal(t, 403, inFlight.Code, "an original request cannot borrow a separately approved replacement assignment: %s", inFlight.Body.String())
	require.Empty(t, inFlight.Header().Get("Content-Disposition"))
	require.NotContains(t, inFlight.Body.String(), "synthetic-intake-contract.png")
	freshDownload := request("GET", filePath+"/content?hr_intake=v1", intake, nil, "")
	require.Equal(t, 200, freshDownload.Code, freshDownload.Body.String())
	require.Equal(t, imageBytes.Bytes(), freshDownload.Body.Bytes())
	replacementLink := base + "/staff/" + actor + "/login-links/" + uuid.Must(uuid.NewV7()).String()
	cloud.AfterReleasedRead(func() {
		call("POST", link+"/decision", maker, map[string]any{"expected_version": 2, "decision": "revoke", "reason": "Synthetic in-flight actor binding cutoff"}, 200)
		call("PUT", replacementLink, maker, map[string]any{"subject": intake, "reason": "Synthetic independently reviewed actor recovery"}, 200)
		call("POST", replacementLink+"/decision", checker, map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic replacement identity review"}, 200)
	})
	oldActor := request("GET", evidence+"/content?hr_intake=v1", intake, nil, "")
	cloud.AfterReleasedRead(nil)
	link = replacementLink
	call("GET", path, intake, nil, 200)
	require.Equal(t, 403, oldActor.Code, "an original request cannot borrow a reviewed replacement actor link: %s", oldActor.Body.String())
	require.Empty(t, oldActor.Header().Get("Content-Disposition"))
	require.NotContains(t, oldActor.Body.String(), "synthetic-intake-contract.png")
	freshActor := request("GET", evidence+"/content?hr_intake=v1", intake, nil, "")
	require.Equal(t, 200, freshActor.Code, freshActor.Body.String())
	require.Equal(t, imageBytes.Bytes(), freshActor.Body.Bytes())
	require.Len(t, call("GET", staffBase+"/documents?hr_intake=v1", intake, nil, 200)["data"], 1)
	call("GET", base+"/hr/payroll/salary-heads", intake, nil, 403)
	call("PUT", strings.Replace(path, hospital, uuid.NewString(), 1), intake, input, 403)
	require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, maker), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("workforce_hospital_access").Where("subject=?", intake).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count, "the delegated workflow must not manufacture hospital-manager grants")
		return nil
	}))
	call("POST", link+"/decision", maker, map[string]any{"expected_version": 2, "decision": "revoke", "reason": "Synthetic HR actor link cutoff"}, 200)
	denied = call("GET", path, intake, nil, 403)
	require.NotContains(t, denied, "profile")
	require.NotContains(t, denied, "staff")
	call("GET", staffBase+"/documents?hr_intake=v1", intake, nil, 403)
	require.Empty(t, call("GET", scopesPath, intake, nil, 200)["data"], "revoked actor links remove HR discovery")
	call("GET", directory, intake, nil, 403)
	call("GET", masterChoices, intake, nil, 403)
	call("GET", masterPath+"/history?hr_intake=v1", intake, nil, 403)
	call("GET", staffBase+"?hr_intake=v1", intake, nil, 403)
	cutoff := request("GET", filePath+"/content?hr_intake=v1", intake, nil, "")
	require.Equal(t, 403, cutoff.Code, cutoff.Body.String())
	require.Empty(t, cutoff.Header().Get("Content-Disposition"))
	require.NotContains(t, cutoff.Body.String(), "synthetic-intake-contract.png")

	for _, scope := range []struct{ tenant, hospital string }{{testutil.TenantB, hospital}, {testutil.TenantA, uuid.NewString()}} {
		require.NoError(t, withContributorContext(scope.tenant, scope.hospital, func(tx *gorm.DB) error {
			var count int64
			if err := tx.Table("workforce_document_contributors").Where("document_id=?", file).Count(&count).Error; err != nil {
				return err
			}
			require.Zero(t, count, "employment-evidence contributors remain private to their tenant/hospital owner context")
			return nil
		}))
	}
	require.NoError(t, db.WithTenantCommand(tenantdb.WithActor(ctx, checker), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_person_review','review',true),set_config('app.workforce_person_review_documents',?,true)`, checker, hospital, staff, file).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Table("workforce_document_contributors").Where("document_id=?", file).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count, "identity-review-only context cannot disclose employment-evidence contributors")
		return nil
	}))

	type documentState struct {
		Status         string
		Audits, Events int64
	}
	inspect := func() documentState {
		t.Helper()
		var state documentState
		require.NoError(t, withContributorContext(testutil.TenantA, hospital, func(tx *gorm.DB) error {
			if err := tx.Table("workforce_documents").Select("status").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, hospital, file).Take(&state).Error; err != nil {
				return err
			}
			if err := tx.Table("audit_events").Where("tenant_id=? AND resource_type='workforce_documents' AND resource_id=?", testutil.TenantA, file).Count(&state.Audits).Error; err != nil {
				return err
			}
			return tx.Table("outbox_events").Where("tenant_id=? AND subject=? AND payload->'data'->>'file_id'=?", testutil.TenantA, workforce.DocumentChanged, file).Count(&state.Events).Error
		}))
		return state
	}
	before := inspect()
	require.Equal(t, "available", before.Status)
	require.Positive(t, before.Audits)
	require.Positive(t, before.Events, "inspection sees a known committed event rather than RLS-hidden zero counts")
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{
		ID:  "test_employment_contributor_fault_install_" + uuid.Must(uuid.NewV7()).String(),
		SQL: fmt.Sprintf(`CREATE FUNCTION test_employment_contributor_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.document_id='%s'::uuid AND NEW.subject='%s' THEN RAISE EXCEPTION 'synthetic employment contributor persistence failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_employment_contributor_fault BEFORE INSERT ON workforce_document_contributors FOR EACH ROW EXECUTE FUNCTION test_employment_contributor_fault()`, file, checker),
	}}))
	clearContributorFault := func() {
		t.Helper()
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Migrate(cleanup, []tenantdb.Migration{{
			ID:  "test_employment_contributor_fault_clear_" + uuid.Must(uuid.NewV7()).String(),
			SQL: `DROP FUNCTION IF EXISTS test_employment_contributor_fault() CASCADE`,
		}}))
	}
	t.Cleanup(clearContributorFault)
	call("PUT", filePath+"/revocation", checker, nil, 500)
	require.Equal(t, before, inspect(), "provenance persistence failure rolls back the document update, audit and event together")
	require.Equal(t, []string{intake}, readEmploymentContributors())
	clearContributorFault()
	revoked := call("PUT", filePath+"/revocation", checker, nil, 200)
	require.Equal(t, "revoked", revoked["status"])
	require.Equal(t, []string{checker, intake}, readEmploymentContributors(), "later human state changes preserve the original preparer and attributable new contributor")
	after := inspect()
	require.Equal(t, "revoked", after.Status)
	require.Equal(t, before.Audits+1, after.Audits)
	require.Equal(t, before.Events+1, after.Events)
	call("PUT", filePath+"/revocation", checker, nil, 200)
	require.Equal(t, after, inspect(), "replayed revocation creates no extra state, audit or event")
	require.Equal(t, []string{checker, intake}, readEmploymentContributors(), "duplicate commands do not duplicate provenance")
	require.ErrorContains(t, withContributorContext(testutil.TenantA, hospital, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE workforce_document_contributors SET subject='synthetic-forged-person' WHERE tenant_id=? AND hospital_id=? AND document_id=?`, testutil.TenantA, hospital, file).Error
	}), "provenance is immutable", "an otherwise authorized manager cannot rewrite preparer history")
	require.NoError(t, withContributorContext(testutil.TenantA, hospital, func(tx *gorm.DB) error {
		result := tx.Exec(`DELETE FROM workforce_document_contributors WHERE tenant_id=? AND hospital_id=? AND document_id=?`, testutil.TenantA, hospital, file)
		require.Zero(t, result.RowsAffected, "the restrictive delete policy preserves contributor history")
		return result.Error
	}))
	require.Equal(t, []string{checker, intake}, readEmploymentContributors())
}
