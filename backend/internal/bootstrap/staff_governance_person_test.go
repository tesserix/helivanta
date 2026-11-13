package bootstrap_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
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
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestGovernancePersonReviewNeedsNoFabricatedStaffAndRechecksDisclosure(t *testing.T) {
	ctx := t.Context()
	cloud := testinfra.NewGCSProtocol(t)
	documents, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	for _, role := range []authz.Role{authz.RoleTenantAdmin, authz.RoleStaffMember} {
		require.NoError(t, graph.GrantTenantRole(ctx, testutil.TenantA, role))
	}
	for _, subject := range []string{"user-admin", "user-owner2"} {
		require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, subject, authz.RoleTenantAdmin))
	}
	for _, subject := range []string{"user-auditor", "user-reviewer", "user-ordinary"} {
		require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, subject, authz.RoleStaffMember))
	}
	modules := []platform.Module{iam.New(nil).SetMembershipIdentityReader(&governanceNativeIdentity{additionalSubjects: map[string]bool{"user-ordinary": true}}), workforce.New()}
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Modules: modules, Documents: documents, Writer: graph, Roles: graph, Membership: graph, EmploymentActivationEnabled: true,
		ScopedAuthorizer: func(db *tenantdb.DB) authz.ScopedAuthorizer { return iam.NewScopedAuthorizer(db, graph) },
		Tokens:           map[string]string{"admin": testutil.TenantA, "owner2": testutil.TenantA, "auditor": testutil.TenantA, "reviewer": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":    {iam.PermGovernanceManage, iam.PermGovernanceRead, iam.PermGovernanceReview, workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage, workforce.PermIdentityChecksManage, workforce.PermIdentityChecksReview, workforce.PermEmploymentPrepare, workforce.PermEmploymentReview, workforce.PermEmploymentActivate},
			"owner2":   {iam.PermGovernanceReview, workforce.PermIdentityChecksManage, workforce.PermIdentityChecksReview, workforce.PermEmploymentPrepare, workforce.PermEmploymentReview, workforce.PermEmploymentActivate},
			"auditor":  {workforce.PermIdentityChecksManage, workforce.PermIdentityChecksReview, workforce.PermEmploymentPrepare, workforce.PermEmploymentReview, workforce.PermEmploymentActivate},
			"reviewer": {workforce.PermIdentityChecksManage, workforce.PermIdentityChecksReview, workforce.PermEmploymentPrepare, workforce.PermEmploymentReview, workforce.PermEmploymentActivate},
		},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin'),(?,'user-owner2','tenant_admin'),(?,'user-auditor','staff_member'),(?,'user-reviewer','staff_member'),(?,'user-ordinary','staff_member')", testutil.TenantA, testutil.TenantA, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
	}))
	call := func(method, path, token string, input any, status int) map[string]any {
		t.Helper()
		var body []byte
		if input != nil {
			body, err = json.Marshal(input)
			require.NoError(t, err)
		}
		response := governanceRequest(router, method, path, token, string(body))
		require.Equal(t, status, response.Code, "%s %s: %s", method, path, response.Body.String())
		var result map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		return result
	}
	roots := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
	staff := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
	for i, root := range roots {
		base := "/v1/workforce/hospitals/" + root.String()
		call(http.MethodPut, base, "admin", map[string]any{"name": "Synthetic nonclinical review hospital"}, 200)
		call(http.MethodPut, base+"/access/user-admin", "admin", map[string]any{"level": "manager"}, 200)
		call(http.MethodPut, base+"/staff/"+staff[i].String(), "admin", map[string]any{"employee_code": "SYN-PERSON", "display_name": "Synthetic inactive applicant", "job_category": "other", "employment_status": "inactive"}, 200)
	}
	base := "/v1/workforce/hospitals/" + roots[0].String()
	target := base + "/staff/" + staff[0].String()
	identityPath := base + "/identity-staff/" + staff[0].String()
	call(http.MethodGet, identityPath, "reviewer", nil, 403)
	call(http.MethodGet, "/v1/workforce/hospitals/"+uuid.Must(uuid.NewV7()).String()+"/identity-staff/"+staff[0].String(), "owner2", nil, 404)
	ownerSource := call(http.MethodGet, identityPath, "owner2", nil, 200)
	require.Equal(t, staff[0].String(), ownerSource["staff_id"])
	issue := func(subject, key string, root uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		input := iam.GovernanceGrantInput{Subject: subject, ExpectedMembershipEpoch: 1, TemplateKey: key, ScopeKind: "hospital", ScopeID: &root, Reason: "Independent nonclinical person examination", ValidFrom: time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond), ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
		call(http.MethodPut, "/v1/iam/governance-grants/"+id.String(), "admin", input, 202)
		call(http.MethodPut, "/v1/iam/governance-grants/"+id.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "owner2", map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent owner confirms exact nonclinical scope"}, 202)
		return id
	}
	issue("user-auditor", "workforce.person_preparer.v1", roots[0])
	reviewGrant := issue("user-reviewer", "workforce.person_reviewer.v1", roots[0])
	t.Run("authority discovery without Staff links", func(t *testing.T) {
		route := "/v1/workforce/person-review-authority-scopes"
		ownerScopes := call(http.MethodGet, route, "owner2", nil, 200)["data"].([]any)
		require.Len(t, ownerScopes, 2)
		for _, scope := range ownerScopes {
			row := scope.(map[string]any)
			require.Equal(t, true, row["can_submit"])
			require.Equal(t, true, row["can_review"])
		}
		for _, subject := range []string{"auditor", "reviewer"} {
			scopes := call(http.MethodGet, route, subject, nil, 200)["data"].([]any)
			require.Len(t, scopes, 1)
			row := scopes[0].(map[string]any)
			require.Equal(t, roots[0].String(), row["hospital_id"])
			require.Equal(t, subject == "auditor", row["can_submit"])
			require.Equal(t, subject == "reviewer", row["can_review"])
			require.Empty(t, call(http.MethodGet, "/v1/workforce/person-review-scopes", subject, nil, 200)["data"], "legacy link discovery must retain its meaning")
		}
		first := call(http.MethodGet, route+"?limit=1", "reviewer", nil, 200)
		require.Empty(t, first["data"], "the newest root has no delegate authority")
		cursor := first["page"].(map[string]any)["next_cursor"].(string)
		require.True(t, first["page"].(map[string]any)["has_more"].(bool))
		second := call(http.MethodGet, route+"?limit=1&cursor="+cursor, "reviewer", nil, 200)
		require.Len(t, second["data"], 1)
		require.False(t, second["page"].(map[string]any)["has_more"].(bool))
		call(http.MethodGet, route+"?limit=1&cursor="+cursor, "auditor", nil, 400)
		call(http.MethodGet, "/v1/workforce/person-review-scopes?cursor="+cursor, "reviewer", nil, 400)
		call(http.MethodGet, route+"?limit=21", "reviewer", nil, 400)
		targets := call(http.MethodPost, base+"/person-review-targets/search", "auditor", map[string]any{"employee_code": "SYN-PERSON"}, 200)["data"].([]any)
		require.Len(t, targets, 1)
		require.Equal(t, staff[0].String(), targets[0].(map[string]any)["id"])
		call(http.MethodPost, base+"/person-review-targets/search", "reviewer", map[string]any{"employee_code": ""}, 403)
		call(http.MethodPost, "/v1/workforce/hospitals/"+roots[1].String()+"/person-review-targets/search", "auditor", map[string]any{"employee_code": ""}, 403)
	})
	source := call(http.MethodGet, identityPath, "auditor", nil, 200)
	call(http.MethodGet, identityPath+"?projection=identity_drafts", "auditor", nil, 403)
	call(http.MethodGet, "/v1/workforce/hospitals/"+roots[1].String()+"/identity-staff/"+staff[1].String(), "reviewer", nil, 403)
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	file := uuid.Must(uuid.NewV7()).String()
	filePath := target + "/documents/" + file
	call(http.MethodPut, filePath, "admin", map[string]any{"record_kind": "enrollment", "record_id": staff[0], "category": "identity_evidence", "filename": "synthetic-person.png", "content_type": "image/png", "size": pngBytes.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(pngBytes.Bytes()))}, 201)
	upload := httptest.NewRequest(http.MethodPut, filePath+"/content", bytes.NewReader(pngBytes.Bytes()))
	upload.Header.Set("Authorization", "Bearer admin")
	upload.Header.Set("Content-Type", "image/png")
	upload.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	upload.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, upload)
	require.Equal(t, 202, response.Code, response.Body.String())
	var database string
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error { return tx.Raw("SELECT current_database()").Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(ctx, testinfra.DocumentWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(ctx, worker, documents, scanner)
	require.NoError(t, err)
	state, err := processor.Process(ctx, workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: roots[0].String(), StaffID: staff[0].String(), FileID: file, ScanAttempt: 1})
	require.NoError(t, err)
	require.Equal(t, "available", state)
	reviewID := uuid.Must(uuid.NewV7())
	reviewPath := target + "/person-reviews/" + reviewID.String()
	input := map[string]any{"command_id": uuid.Must(uuid.NewV7()), "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "identity_hash": source["identity_hash"], "policy_reference": "SYN-PERSON-POLICY", "finding_reference": "Human identity examination", "observed_on": time.Now().UTC().Format(time.DateOnly), "valid_until": time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond), "access_intent": "digital", "declared_subject": "user-ordinary", "identity_examined": true, "known_associations_disclosed": true, "document_ids": []string{file}}
	pending := call(http.MethodPut, reviewPath, "auditor", input, 200)
	require.Equal(t, "pending", pending["status"])
	for _, subject := range []string{"owner2", "auditor", "reviewer"} {
		inbox := call(http.MethodGet, base+"/person-review-inbox", subject, nil, 200)["data"].([]any)
		require.Len(t, inbox, 1)
		require.Equal(t, reviewID.String(), inbox[0].(map[string]any)["id"])
	}

	decision := map[string]any{"command_id": uuid.Must(uuid.NewV7()), "expected_version": 1, "decision": "approve", "reason": "Independent human examination", "identity_examined": true, "independent_person_confirmed": true}
	call(http.MethodPost, reviewPath+"/decision", "auditor", decision, 403)
	call(http.MethodPost, reviewPath+"/decision", "admin", decision, 403)
	approved := call(http.MethodPost, reviewPath+"/decision", "reviewer", decision, 200)
	require.Equal(t, "approved", approved["status"])
	var readyEmployment workforce.EmploymentReview
	t.Run("employment submission and independent decision need no Staff actor", func(t *testing.T) {
		request := func(t *testing.T, method, path, token string, input any, status int) map[string]any {
			t.Helper()
			var body []byte
			if input != nil {
				var marshalErr error
				body, marshalErr = json.Marshal(input)
				require.NoError(t, marshalErr)
			}
			response := governanceRequest(router, method, path, token, string(body))
			require.Equal(t, status, response.Code, "%s %s: %s", method, path, response.Body.String())
			var result map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			return result
		}
		grant := func(subject, key string, root uuid.UUID) uuid.UUID {
			t.Helper()
			id := uuid.Must(uuid.NewV7())
			request(t, http.MethodPut, "/v1/iam/governance-grants/"+id.String(), "admin", iam.GovernanceGrantInput{Subject: subject, ExpectedMembershipEpoch: 1, TemplateKey: key, ScopeKind: "hospital", ScopeID: &root, Reason: "Synthetic nonclinical employment review", ValidFrom: time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond), ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}, 202)
			request(t, http.MethodPut, "/v1/iam/governance-grants/"+id.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "owner2", map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent owner confirms exact employment purpose"}, 202)
			return id
		}
		selectionPath := target + "/employment-person-finding-options"
		selectionQuery := fmt.Sprintf("?hr_intake=v1&staff_version=%v&employment_epoch=%v", source["staff_version"], source["employment_epoch"])
		request(t, http.MethodGet, selectionPath+selectionQuery, "auditor", nil, 403)
		prepareGrant := grant("user-auditor", "workforce.employment_preparer.v1", roots[0])
		period := uuid.Must(uuid.NewV7())
		terms := request(t, http.MethodPut, target+"/employment-preparation-periods/"+period.String(), "auditor", map[string]any{"command_id": uuid.Must(uuid.NewV7()), "expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "engagement_kind": "employee", "duration": "ongoing", "starts_at": time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond), "reason": "Synthetic appointment", "evidence_reference": "SYN-APPOINTMENT"}, 200)
		employmentFile := uuid.Must(uuid.NewV7()).String()
		path := target + "/documents/" + employmentFile
		request(t, http.MethodPut, path, "admin", map[string]any{"record_kind": "enrollment", "record_id": staff[0], "category": "employment_evidence", "filename": "synthetic-appointment.png", "content_type": "image/png", "size": pngBytes.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(pngBytes.Bytes()))}, 201)
		upload := httptest.NewRequest(http.MethodPut, path+"/content", bytes.NewReader(pngBytes.Bytes()))
		upload.Header.Set("Authorization", "Bearer admin")
		upload.Header.Set("Content-Type", "image/png")
		upload.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		upload.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, upload)
		require.Equal(t, 202, response.Code, response.Body.String())
		state, processErr := processor.Process(ctx, workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: roots[0].String(), StaffID: staff[0].String(), FileID: employmentFile, ScanAttempt: 1})
		require.NoError(t, processErr)
		require.Equal(t, "available", state)
		bindingID := uuid.Must(uuid.NewV7())
		binding := request(t, http.MethodPut, target+"/employment-periods/"+period.String()+"/revisions/1/evidence/"+bindingID.String(), "admin", map[string]any{"command_id": uuid.Must(uuid.NewV7()), "document_id": employmentFile, "reason": "Released appointment evidence"}, 200)
		employmentReview := uuid.Must(uuid.NewV7())
		employmentPath := target + "/employment-reviews/" + employmentReview.String()
		input := map[string]any{"command_id": uuid.Must(uuid.NewV7()), "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "identity_hash": source["identity_hash"], "period_id": period, "period_revision_id": terms["revision_id"], "period_version": terms["version"], "person_review_id": reviewID, "person_review_revision_id": approved["revision_id"], "person_review_version": approved["version"], "evidence": []map[string]any{{"id": bindingID, "revision_id": binding["revision_id"], "version": binding["version"]}}, "policy_reference": "SYN-EMPLOYMENT-POLICY", "reason": "Freeze exact reviewed person and appointment terms", "terms_examined": true, "known_associations_disclosed": true}
		request(t, http.MethodPut, employmentPath+"?hr_intake=v1", "auditor", input, 403)

		t.Run("exact governance authors terms without a Staff actor", func(t *testing.T) {
			for _, boundary := range []struct{ header, value string }{{"X-Helivanta-Expected-Tenant", ""}, {"X-Helivanta-Expected-Subject", ""}, {"X-Helivanta-Expected-Tenant", testutil.TenantB}, {"X-Helivanta-Expected-Subject", "user-owner2"}} {
				req := httptest.NewRequest(http.MethodGet, target+"/employment-preparation-source", nil)
				req.Header.Set("Authorization", "Bearer auditor")
				req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
				req.Header.Set("X-Helivanta-Expected-Subject", "user-auditor")
				req.Header.Set(boundary.header, boundary.value)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				require.Equal(t, 409, response.Code, response.Body.String())
				require.NotContains(t, response.Body.String(), "display_name")
			}
			current := request(t, http.MethodGet, target+"/employment-preparation-source", "auditor", nil, 200)
			require.Equal(t, source["staff_version"], current["staff_version"])
			id := uuid.Must(uuid.NewV7())
			draftPath := target + "/employment-preparation-periods/" + id.String()
			command := map[string]any{"command_id": uuid.Must(uuid.NewV7()), "expected_version": 0, "staff_version": current["staff_version"], "employment_epoch": current["employment_epoch"], "engagement_kind": "employee", "duration": "ongoing", "starts_at": time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond), "reason": "Synthetic exact governance terms", "evidence_reference": "SYN-GOV-TERMS"}
			request(t, http.MethodPut, draftPath, "reviewer", command, 403)
			first := request(t, http.MethodPut, draftPath, "auditor", command, 200)
			require.Equal(t, float64(1), first["version"])
			require.Equal(t, "user-auditor", first["actor_subject"])
			for _, key := range []string{"authority", "governance_source", "input_hash"} {
				require.NotContains(t, first, key)
			}
			require.Equal(t, first, request(t, http.MethodPut, draftPath, "auditor", command, 200))
			changed := map[string]any{}
			for key, value := range command {
				changed[key] = value
			}
			changed["reason"] = "Changed digest"
			request(t, http.MethodPut, draftPath, "auditor", changed, 409)
			changed["command_id"], changed["expected_version"] = uuid.Must(uuid.NewV7()), 1
			second := request(t, http.MethodPut, draftPath, "auditor", changed, 200)
			require.Equal(t, float64(2), second["version"])
			require.Equal(t, first, request(t, http.MethodPut, draftPath, "auditor", command, 200), "a replay retains the exact original revision after subsequent edits")
			request(t, http.MethodGet, draftPath, "admin", nil, 200)
			request(t, http.MethodGet, draftPath, "reviewer", nil, 403)
			require.Len(t, request(t, http.MethodGet, draftPath+"/history", "auditor", nil, 200)["data"], 2)
			request(t, http.MethodGet, target+"/employment-preparation-periods?limit=1", "auditor", nil, 200)
			request(t, http.MethodGet, draftPath+"?unknown=1", "auditor", nil, 400)
			request(t, http.MethodGet, target+"/employment-periods/"+id.String()+"?hr_intake=v1", "auditor", nil, 403)
			var recorded struct{ Authority json.RawMessage }
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)", roots[0].String()).Error; err != nil {
					return err
				}
				return tx.Raw("SELECT changes->'authority' AS authority FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND actor_subject='user-admin' AND resource_id=? AND action='workforce.employment_period.read' ORDER BY created_at DESC,id DESC LIMIT 1", testutil.TenantA, roots[0], id).Take(&recorded).Error
			}))
			assertReadPurposeBlocksStaffMasterEdit(t, db, roots[0], staff[0], "app.workforce_governance_draft_authority", recorded.Authority)
			next := map[string]any{}
			for key, value := range changed {
				next[key] = value
			}
			next["command_id"], next["expected_version"] = uuid.Must(uuid.NewV7()), 2
			next["staff_version"] = 999
			request(t, http.MethodPut, draftPath, "auditor", next, 409)
			next["staff_version"] = current["staff_version"]
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_governance_draft_source_disabled", SQL: "ALTER TABLE workforce_employment_period_history DISABLE TRIGGER workforce_governance_draft_provenance"}}))
			func() {
				defer func() {
					require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_governance_draft_source_restored", SQL: "ALTER TABLE workforce_employment_period_history ENABLE TRIGGER workforce_governance_draft_provenance"}}))
				}()
				request(t, http.MethodPut, draftPath, "auditor", next, 503)
			}()
			require.Len(t, request(t, http.MethodGet, draftPath+"/history", "auditor", nil, 200)["data"], 2)
			legacy := uuid.Must(uuid.NewV7())
			legacyInput := map[string]any{}
			for key, value := range command {
				legacyInput[key] = value
			}
			legacyInput["command_id"] = uuid.Must(uuid.NewV7())
			request(t, http.MethodPut, target+"/employment-periods/"+legacy.String(), "admin", legacyInput, 200)
			request(t, http.MethodPut, target+"/employment-preparation-periods/"+legacy.String(), "admin", legacyInput, 409)
		})
		t.Run("selected finding preparation needs no Staff actor", func(t *testing.T) {
			for _, actor := range []string{"owner2", "auditor", "admin"} {
				rows := request(t, http.MethodGet, selectionPath+selectionQuery, actor, nil, 200)["data"].([]any)
				require.Len(t, rows, 1)
				row := rows[0].(map[string]any)
				require.Equal(t, reviewID.String(), row["id"])
				require.Equal(t, approved["revision_id"], row["revision_id"])
				for _, key := range []string{"evidence", "governance_authority", "actor_link_id", "actor_staff_id"} {
					require.NotContains(t, row, key)
				}
			}
			request(t, http.MethodGet, selectionPath+selectionQuery, "reviewer", nil, 403)
			request(t, http.MethodGet, "/v1/workforce/hospitals/"+roots[1].String()+"/staff/"+staff[1].String()+"/employment-person-finding-options"+selectionQuery, "auditor", nil, 403)
			request(t, http.MethodGet, base+"/staff/"+uuid.Must(uuid.NewV7()).String()+"/employment-person-finding-options"+selectionQuery, "owner2", nil, 404)
			request(t, http.MethodGet, selectionPath+"?hr_intake=v1&staff_version=999&employment_epoch=1", "auditor", nil, 409)
			request(t, http.MethodGet, selectionPath+selectionQuery+"&limit=21", "auditor", nil, 400)
			request(t, http.MethodGet, selectionPath+selectionQuery+"&employment_epoch=1", "auditor", nil, 400)
			for _, policy := range []struct{ table, column, clause string }{{"workforce_documents", "polqual", "USING"}, {"workforce_audit", "polwithcheck", "WITH CHECK"}} {
				t.Run("missing finding support on "+policy.table, func(t *testing.T) {
					var saved struct{ Name, Expression string }
					require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
						return tx.Raw("SELECT polname AS name,pg_get_expr("+policy.column+",polrelid) AS expression FROM pg_policy WHERE polrelid=to_regclass(?) AND polpermissive", "public."+policy.table).Take(&saved).Error
					}))
					require.NotEmpty(t, saved.Expression)
					require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_finding_" + policy.table + "_disabled", SQL: "ALTER POLICY " + saved.Name + " ON " + policy.table + " " + policy.clause + " (false)"}}))
					defer func() {
						require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_finding_" + policy.table + "_restored", SQL: "ALTER POLICY " + saved.Name + " ON " + policy.table + " " + policy.clause + " (" + saved.Expression + ")"}}))
					}()
					request(t, http.MethodGet, selectionPath+selectionQuery, "auditor", nil, 503)
				})
			}
			var recorded struct{ Authority json.RawMessage }
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)", roots[0].String()).Error; err != nil {
					return err
				}
				return tx.Raw("SELECT changes->'authority' AS authority FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND actor_subject='user-admin' AND action='workforce.employment_review.governance_person_selection.read' ORDER BY created_at DESC,id DESC LIMIT 1", testutil.TenantA, roots[0]).Take(&recorded).Error
			}))
			require.NotEmpty(t, recorded.Authority)
			assertReadPurposeBlocksStaffMasterEdit(t, db, roots[0], staff[0], "app.workforce_governance_finding_authority", recorded.Authority)
		})
		pending := request(t, http.MethodPut, employmentPath+"?hr_intake=v1", "auditor", input, 200)
		require.Equal(t, "pending", pending["status"])
		request(t, http.MethodPut, employmentPath+"?hr_intake=v1", "auditor", input, 200)
		request(t, http.MethodGet, employmentPath+"?employment_review=v1", "reviewer", nil, 403)
		request(t, http.MethodGet, "/v1/workforce/hospitals/"+roots[0].String()+"/employment-review-authority-inbox", "reviewer", nil, 403)
		employmentGrant := grant("user-reviewer", "workforce.employment_reviewer.v1", roots[0])
		t.Run("employment authority discovery needs no Staff links", func(t *testing.T) {
			route := "/v1/workforce/employment-review-authority-scopes"
			owners := request(t, http.MethodGet, route, "owner2", nil, 200)["data"].([]any)
			require.Len(t, owners, 2)
			for _, value := range owners {
				row := value.(map[string]any)
				require.Equal(t, true, row["can_submit"])
				require.Equal(t, true, row["can_review"])
			}
			for _, subject := range []string{"auditor", "reviewer"} {
				rows := request(t, http.MethodGet, route, subject, nil, 200)["data"].([]any)
				require.Len(t, rows, 1)
				row := rows[0].(map[string]any)
				require.Equal(t, roots[0].String(), row["hospital_id"])
				require.Equal(t, subject == "auditor", row["can_submit"])
				require.Equal(t, subject == "reviewer", row["can_review"])
				require.Empty(t, request(t, http.MethodGet, "/v1/workforce/employment-review-scopes", subject, nil, 200)["data"], "the existing linked-Staff discovery contract stays distinct")
			}
			first := request(t, http.MethodGet, route+"?limit=1", "reviewer", nil, 200)
			require.Empty(t, first["data"])
			cursor := first["page"].(map[string]any)["next_cursor"].(string)
			require.Equal(t, true, first["page"].(map[string]any)["has_more"])
			second := request(t, http.MethodGet, route+"?limit=1&cursor="+cursor, "reviewer", nil, 200)
			require.Len(t, second["data"], 1)
			require.Equal(t, false, second["page"].(map[string]any)["has_more"])
			request(t, http.MethodGet, route+"?cursor="+cursor, "auditor", nil, 400)
			request(t, http.MethodGet, "/v1/workforce/employment-review-scopes?cursor="+cursor, "reviewer", nil, 400)
			request(t, http.MethodGet, route+"?limit=21", "reviewer", nil, 400)
		})
		t.Run("current employment governance opens only the employer inbox", func(t *testing.T) {
			inbox := "/v1/workforce/hospitals/" + roots[0].String() + "/employment-review-authority-inbox"
			for _, boundary := range []struct{ header, value string }{
				{"X-Helivanta-Expected-Tenant", ""}, {"X-Helivanta-Expected-Subject", ""},
				{"X-Helivanta-Expected-Tenant", testutil.TenantB}, {"X-Helivanta-Expected-Subject", "user-owner2"},
			} {
				req := httptest.NewRequest(http.MethodGet, inbox, nil)
				req.Header.Set("Authorization", "Bearer reviewer")
				req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
				req.Header.Set("X-Helivanta-Expected-Subject", "user-reviewer")
				if boundary.value == "" {
					req.Header.Del(boundary.header)
				} else {
					req.Header.Set(boundary.header, boundary.value)
				}
				refused := httptest.NewRecorder()
				router.ServeHTTP(refused, req)
				require.Equal(t, 409, refused.Code)
				require.NotContains(t, refused.Body.String(), "employee_code")
			}
			for _, subject := range []string{"owner2", "reviewer", "auditor"} {
				mode := "review"
				if subject == "auditor" {
					mode = "prepare"
				}
				rows := request(t, http.MethodGet, inbox+"?mode="+mode, subject, nil, 200)["data"].([]any)
				require.Len(t, rows, 1)
				row := rows[0].(map[string]any)
				require.Equal(t, employmentReview.String(), row["id"])
				require.Equal(t, roots[0].String(), row["hospital_id"])
				for _, key := range []string{"governance_authority", "evidence_ids", "declared_subject", "reason", "metadata", "identity_hash"} {
					require.NotContains(t, row, key)
				}
			}
			request(t, http.MethodGet, inbox+"?mode=review", "auditor", nil, 403)
			request(t, http.MethodGet, inbox+"?mode=prepare", "reviewer", nil, 403)
			request(t, http.MethodGet, "/v1/workforce/hospitals/"+roots[1].String()+"/employment-review-authority-inbox", "reviewer", nil, 403)
			missingInbox := "/v1/workforce/hospitals/" + uuid.Must(uuid.NewV7()).String() + "/employment-review-authority-inbox"
			request(t, http.MethodGet, missingInbox, "reviewer", nil, 403)
			request(t, http.MethodGet, missingInbox, "owner2", nil, 404)
			request(t, http.MethodGet, inbox+"?limit=51", "reviewer", nil, 400)
			request(t, http.MethodGet, inbox+"?status=pending&status=approved", "reviewer", nil, 400)
			require.Empty(t, request(t, http.MethodGet, inbox+"?status=approved", "reviewer", nil, 200)["data"])
			for _, policy := range []struct{ table, column, clause string }{
				{"workforce_staff", "polqual", "USING"},
				{"workforce_audit", "polwithcheck", "WITH CHECK"},
			} {
				t.Run("missing "+policy.table+" policy support fails closed", func(t *testing.T) {
					var saved struct{ Name, Expression string }
					require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
						return tx.Raw("SELECT polname AS name,pg_get_expr("+policy.column+",polrelid) AS expression FROM pg_policy WHERE polrelid=to_regclass(?) AND polpermissive", "public."+policy.table).Take(&saved).Error
					}))
					require.NotEmpty(t, saved.Expression)
					require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_inbox_" + policy.table + "_disabled", SQL: "ALTER POLICY " + saved.Name + " ON " + policy.table + " " + policy.clause + " (false)"}}))
					defer func() {
						require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_inbox_" + policy.table + "_restored", SQL: "ALTER POLICY " + saved.Name + " ON " + policy.table + " " + policy.clause + " (" + saved.Expression + ")"}}))
					}()
					request(t, http.MethodGet, inbox, "reviewer", nil, 503)
				})
			}
			request(t, http.MethodGet, inbox, "admin", nil, 200)
			var recorded struct{ Authority json.RawMessage }
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)", roots[0].String()).Error; err != nil {
					return err
				}
				return tx.Raw("SELECT changes->'authority' AS authority FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND actor_subject='user-admin' AND action='workforce.employment_authority_inbox.read' ORDER BY created_at DESC,id DESC LIMIT 1", testutil.TenantA, roots[0]).Take(&recorded).Error
			}))
			require.NotEmpty(t, recorded.Authority)
			assertReadPurposeBlocksStaffMasterEdit(t, db, roots[0], staff[0], "app.workforce_employment_inbox_authority", recorded.Authority)
		})
		view := request(t, http.MethodGet, employmentPath+"?employment_review=v1", "reviewer", nil, 200)
		require.Equal(t, true, view["can_approve"])
		decision := map[string]any{"command_id": uuid.Must(uuid.NewV7()), "expected_version": 1, "decision": "approve", "reason": "Independent appointment and evidence examination", "terms_examined": true, "independent_person_confirmed": true}
		request(t, http.MethodPost, employmentPath+"/decision?employment_review=v1", "auditor", decision, 403)
		request(t, http.MethodPost, employmentPath+"/decision?employment_review=v1", "admin", decision, 403)
		approved := request(t, http.MethodPost, employmentPath+"/decision?employment_review=v1", "reviewer", decision, 200)
		require.Equal(t, "approved", approved["status"])
		raw, marshalErr := json.Marshal(approved)
		require.NoError(t, marshalErr)
		require.NoError(t, json.Unmarshal(raw, &readyEmployment))
		enrollment := request(t, http.MethodGet, target+"/enrollment", "admin", nil, 200)
		require.Equal(t, "inactive", enrollment["staff"].(map[string]any)["employment_status"], "approval is not employment activation or operating access")
		request(t, http.MethodPost, employmentPath+"/decision?employment_review=v1", "reviewer", decision, 200)
		stale := map[string]any{"command_id": uuid.Must(uuid.NewV7()), "expected_version": 1, "decision": "approve", "reason": "Stale review decision must not advance history", "terms_examined": true, "independent_person_confirmed": true}
		request(t, http.MethodPost, employmentPath+"/decision?employment_review=v1", "reviewer", stale, 409)
		request(t, http.MethodGet, "/v1/workforce/hospitals/"+roots[0].String()+"/staff/"+staff[1].String()+"/employment-reviews/"+employmentReview.String()+"?employment_review=v1", "reviewer", nil, 404)
		request(t, http.MethodGet, employmentPath+"/history?employment_review=v1", "reviewer", nil, 200)
		var audit struct{ Changes json.RawMessage }
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Table("workforce_audit").Select("changes").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce.employment_review.decided'", testutil.TenantA, roots[0], employmentReview).Take(&audit).Error
		}))
		proof := append(json.RawMessage(nil), audit.Changes...)
		require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "user-reviewer"), testutil.TenantA, func(tx *gorm.DB) error {
			require.NoError(t, tx.Exec("SELECT set_config('app.workforce_subject','user-reviewer',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_employment_review','review',true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_employment_authority',?,true)", roots[0].String(), staff[0].String(), employmentReview.String(), string(proof)).Error)
			var history []struct {
				Version             int64
				GovernanceAuthority json.RawMessage
			}
			require.NoError(t, tx.Table("workforce_employment_review_history").Select("version,governance_authority").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, roots[0], employmentReview).Order("version").Find(&history).Error)
			require.Len(t, history, 2)
			for i, id := range []uuid.UUID{prepareGrant, employmentGrant} {
				var authority map[string]any
				require.NoError(t, json.Unmarshal(history[i].GovernanceAuthority, &authority))
				require.Equal(t, id.String(), authority["source_grant_id"])
				require.NotContains(t, authority, "actor_link_id")
				require.NotContains(t, authority, "actor_staff_id")
			}
			proof = append(json.RawMessage(nil), history[1].GovernanceAuthority...)
			return nil
		}))
		writeErr := db.WithTenant(tenantdb.WithActor(ctx, "user-reviewer"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.workforce_subject','user-reviewer',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_employment_review','review',true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_employment_authority',?,true)", roots[0].String(), staff[0].String(), employmentReview.String(), string(proof)).Error; err != nil {
				return err
			}
			return tx.Table("workforce_staff").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, roots[0], staff[0]).Update("display_name", "Forbidden employment-purpose master write").Error
		})
		var deniedWrite *pgconn.PgError
		require.ErrorAs(t, writeErr, &deniedWrite)
		require.Equal(t, "42501", deniedWrite.Code)
		request(t, http.MethodGet, employmentPath+"?hr_intake=v1", "admin", nil, 200)
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Table("workforce_audit").Select("changes").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND actor_subject='user-admin' AND action='workforce.employment_review.read'", testutil.TenantA, roots[0], employmentReview).Take(&audit).Error
		}))
		ownerWriteErr := db.WithTenant(tenantdb.WithActor(ctx, "user-admin"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_employment_review','manage',true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_employment_authority',?,true)", roots[0].String(), staff[0].String(), employmentReview.String(), string(audit.Changes)).Error; err != nil {
				return err
			}
			var other int64
			if err := tx.Table("workforce_staff").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, roots[1], staff[1]).Count(&other).Error; err != nil {
				return err
			}
			require.Zero(t, other, "an owner managing both hospitals cannot widen this selected purpose")
			deleted := tx.Exec("DELETE FROM workforce_enrollment_profile WHERE tenant_id=? AND hospital_id=? AND staff_id=?", testutil.TenantA, roots[0], staff[0])
			if deleted.Error != nil {
				return deleted.Error
			}
			require.Zero(t, deleted.RowsAffected, "the private purpose denies deletion despite inherited manager access")
			return tx.Table("workforce_staff").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, roots[0], staff[0]).Update("display_name", "Forbidden inherited-manager write").Error
		})
		require.ErrorAs(t, ownerWriteErr, &deniedWrite)
		require.Equal(t, "42501", deniedWrite.Code)
		for _, trigger := range []string{"workforce_employment_review_zz_authority", "workforce_employment_review_snapshot"} {
			t.Run("missing recording support "+trigger, func(t *testing.T) {
				require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_" + trigger + "_disabled", SQL: "ALTER TABLE workforce_employment_reviews DISABLE TRIGGER " + trigger}}))
				defer func() {
					require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_" + trigger + "_restored", SQL: "ALTER TABLE workforce_employment_reviews ENABLE TRIGGER " + trigger}}))
				}()
				response := governanceRequest(router, http.MethodGet, employmentPath+"?employment_review=v1", "reviewer", "")
				require.Equal(t, 503, response.Code, response.Body.String())
			})
		}
		t.Run("missing private purpose denies before disclosure", func(t *testing.T) {
			var policy struct{ Reading, Writing string }
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Raw(`SELECT pg_get_expr(polqual,polrelid) AS reading,pg_get_expr(polwithcheck,polrelid) AS writing FROM pg_policy WHERE polrelid='public.workforce_staff'::regclass AND polname='employment_governance_purpose'`).Take(&policy).Error
			}))
			require.NotEmpty(t, policy.Reading)
			require.NotEmpty(t, policy.Writing)
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_employment_purpose_removed", SQL: "DROP POLICY employment_governance_purpose ON workforce_staff"}}))
			defer func() {
				require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_employment_purpose_restored", SQL: "CREATE POLICY employment_governance_purpose ON workforce_staff AS RESTRICTIVE FOR ALL USING(" + policy.Reading + ") WITH CHECK(" + policy.Writing + ")"}}))
			}()
			response := governanceRequest(router, http.MethodGet, employmentPath+"?employment_review=v1", "reviewer", "")
			require.Equal(t, 503, response.Code, response.Body.String())
		})
		t.Run("truncate privilege denies private-purpose commands", func(t *testing.T) {
			var canTruncate bool
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Raw("SELECT has_table_privilege(current_user,'workforce_staff','TRUNCATE')").Scan(&canTruncate).Error
			}))
			require.False(t, canTruncate, "the application role must match production-shaped DML-only privileges")
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_employment_truncate_granted", SQL: "GRANT TRUNCATE ON workforce_staff TO hms_app"}}))
			defer func() {
				require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_employment_truncate_revoked", SQL: "REVOKE TRUNCATE ON workforce_staff FROM hms_app"}}))
			}()
			response := governanceRequest(router, http.MethodGet, employmentPath+"?employment_review=v1", "reviewer", "")
			require.Equal(t, 503, response.Code, response.Body.String())
		})
		evidencePath := employmentPath + "/evidence/" + employmentFile + "/content?employment_review=v1"
		download := governanceRequest(router, http.MethodGet, evidencePath, "reviewer", "")
		require.Equal(t, 200, download.Code, download.Body.String())
		require.Equal(t, pngBytes.Bytes(), download.Body.Bytes())
		request(t, http.MethodPut, "/v1/iam/governance-grants/"+prepareGrant.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", map[string]any{"expected_version": 2, "decision": "revoke", "reason": "Withdraw exact employment preparation source"}, 202)
		request(t, http.MethodGet, selectionPath+selectionQuery, "auditor", nil, 403)
		request(t, http.MethodGet, target+"/employment-preparation-source", "auditor", nil, 403)
		grant("user-reviewer", "workforce.employment_reviewer.v1", roots[1])
		cutoff := make(chan *httptest.ResponseRecorder, 1)
		cloud.AfterReleasedRead(func() {
			cutoff <- governanceRequest(router, http.MethodPut, "/v1/iam/governance-grants/"+employmentGrant.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", `{"expected_version":2,"decision":"revoke","reason":"Withdraw employment review while storage is read"}`)
		})
		denied := governanceRequest(router, http.MethodGet, evidencePath, "reviewer", "")
		select {
		case result := <-cutoff:
			require.Equal(t, 202, result.Code, result.Body.String())
		default:
			t.Fatal("employment evidence did not reach the storage cutoff")
		}
		require.Equal(t, 403, denied.Code, denied.Body.String())
		require.NotEqual(t, pngBytes.Bytes(), denied.Body.Bytes())
		cloud.AfterReleasedRead(nil)
		request(t, http.MethodGet, employmentPath+"?employment_review=v1", "reviewer", nil, 403)
	})
	var reviewProof json.RawMessage
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec("SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_person_review','read',true)", roots[0].String(), staff[0].String()).Error)
		var history []struct {
			Version             int64
			GovernanceAuthority json.RawMessage
		}
		require.NoError(t, tx.Table("workforce_person_review_history").Select("version,governance_authority").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, roots[0], reviewID).Order("version").Find(&history).Error)
		require.Len(t, history, 2)
		reviewProof = append(json.RawMessage(nil), history[1].GovernanceAuthority...)
		var proof map[string]any
		require.NoError(t, json.Unmarshal(history[1].GovernanceAuthority, &proof))
		require.Equal(t, reviewGrant.String(), proof["source_grant_id"])
		require.Equal(t, float64(2), proof["source_grant_version"])
		require.NotContains(t, proof, "actor_link_id")
		require.NotContains(t, proof, "actor_staff_id")
		var managers, links int64
		require.NoError(t, tx.Table("workforce_hospital_access").Where("tenant_id=? AND subject IN('user-reviewer','user-auditor','user-owner2')", testutil.TenantA).Count(&managers).Error)
		require.Zero(t, managers)
		require.NoError(t, tx.Table("workforce_login_links").Where("tenant_id=?", testutil.TenantA).Count(&links).Error)
		require.Zero(t, links)
		return nil
	}))

	writeErr := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.workforce_subject','user-reviewer',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_person_authority',?,true)", roots[0].String(), staff[0].String(), string(reviewProof)).Error; err != nil {
			return err
		}
		var visible int64
		require.NoError(t, tx.Table("workforce_staff").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, roots[0], staff[0]).Count(&visible).Error)
		require.EqualValues(t, 1, visible)
		return tx.Table("workforce_staff").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, roots[0], staff[0]).Update("display_name", "Forbidden nonclinical-purpose write").Error
	})
	var deniedWrite *pgconn.PgError
	require.ErrorAs(t, writeErr, &deniedWrite)
	require.Equal(t, "42501", deniedWrite.Code)
	t.Run("missing authority recorder fails closed", func(t *testing.T) {
		require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_person_authority_disabled", SQL: "ALTER TABLE workforce_person_reviews DISABLE TRIGGER workforce_person_review_zz_authority"}}))
		defer func() {
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_person_authority_restored", SQL: "ALTER TABLE workforce_person_reviews ENABLE TRIGGER workforce_person_review_zz_authority"}}))
		}()
		call(http.MethodGet, identityPath, "reviewer", nil, 503)
	})
	evidencePath := reviewPath + "/evidence/" + file + "/content"
	downloaded := governanceRequest(router, http.MethodGet, evidencePath, "reviewer", "")
	require.Equal(t, 200, downloaded.Code, downloaded.Body.String())
	require.Equal(t, pngBytes.Bytes(), downloaded.Body.Bytes())
	issue("user-reviewer", "workforce.person_reviewer.v1", roots[1])
	cutoff := make(chan *httptest.ResponseRecorder, 1)
	cloud.AfterReleasedRead(func() {
		cutoff <- governanceRequest(router, http.MethodPut, "/v1/iam/governance-grants/"+reviewGrant.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", `{"expected_version":2,"decision":"revoke","reason":"Withdraw while file storage is being read"}`)
	})
	denied := governanceRequest(router, http.MethodGet, evidencePath, "reviewer", "")
	select {
	case result := <-cutoff:
		require.Equal(t, 202, result.Code, result.Body.String())
	default:
		t.Fatal("the released file read did not reach its cutoff boundary")
	}
	require.Equal(t, 403, denied.Code, denied.Body.String())
	require.NotEqual(t, pngBytes.Bytes(), denied.Body.Bytes())
	call(http.MethodGet, identityPath, "reviewer", nil, 403)
	call(http.MethodGet, base+"/person-review-inbox", "reviewer", nil, 403)
	remainingScopes := call(http.MethodGet, "/v1/workforce/person-review-authority-scopes", "reviewer", nil, 200)["data"].([]any)
	require.Len(t, remainingScopes, 1)
	require.Equal(t, roots[1].String(), remainingScopes[0].(map[string]any)["hospital_id"], "the sibling source admits only its own hospital; it cannot revive the revoked root")

	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var routing int64
		require.NoError(t, tx.Table("iam_members").Where("tenant_id=? AND subject='user-reviewer' AND role_key='governance_delegate'", testutil.TenantA).Count(&routing).Error)
		require.EqualValues(t, 1, routing)
		return nil
	}))
	t.Run("first digital hire activation consumes exact governance basis", func(t *testing.T) {
		request := func(t *testing.T, method, path string, input any, status int) map[string]any {
			t.Helper()
			var body []byte
			if input != nil {
				var marshalErr error
				body, marshalErr = json.Marshal(input)
				require.NoError(t, marshalErr)
			}
			response := governanceRequest(router, method, path, "reviewer", string(body))
			require.Equal(t, status, response.Code, "%s %s: %s", method, path, response.Body.String())
			var result map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			return result
		}
		sourcePath := target + "/employment-reviews/" + readyEmployment.ID.String() + "/activation-source?employment_activation=v1"
		request(t, http.MethodGet, sourcePath, nil, 403)
		activationGrant := issue("user-reviewer", "workforce.employment_activator.v1", roots[0])
		ready := request(t, http.MethodGet, sourcePath, nil, 200)
		require.Equal(t, "eligible", ready["current_state"])
		require.Equal(t, true, ready["can_activate"])
		generation := uuid.Must(uuid.NewV7())
		activationPath := target + "/employment-activations/" + generation.String() + "?employment_activation=v1"
		intent := map[string]any{"command_id": uuid.Must(uuid.NewV7()), "review_id": readyEmployment.ID, "review_revision_id": readyEmployment.RevisionID, "review_version": readyEmployment.Version, "staff_version": readyEmployment.StaffVersion, "employment_epoch": readyEmployment.EmploymentEpoch, "reason": "Synthetic first digital hire with independently approved basis", "activation_examined": true}
		for _, prerequisite := range []struct{ table, trigger string }{
			{"workforce_staff", "zz_workforce_governance_activation"},
			{"workforce_staff", "workforce_reviewed_activation_required"},
			{"workforce_employment_generations", "workforce_employment_generation_write"},
			{"workforce_employment_generations", "workforce_employment_generation_immutable"},
			{"workforce_employment_generations", "workforce_employment_generation_zz_authority"},
		} {
			t.Run("missing activation prerequisite "+prerequisite.trigger, func(t *testing.T) {
				require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_activation_" + prerequisite.trigger + "_disabled", SQL: "ALTER TABLE " + prerequisite.table + " DISABLE TRIGGER " + prerequisite.trigger}}))
				defer func() {
					require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_activation_" + prerequisite.trigger + "_restored", SQL: "ALTER TABLE " + prerequisite.table + " ENABLE TRIGGER " + prerequisite.trigger}}))
				}()
				request(t, http.MethodGet, sourcePath, nil, 503)
				request(t, http.MethodPut, activationPath, intent, 503)
			})
		}
		t.Run("late generation failure rolls back the whole digital activation", func(t *testing.T) {
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_activation_commit_failure", SQL: `CREATE FUNCTION test_governance_activation_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic deferred activation failure' USING ERRCODE='23514'; END $$;
CREATE CONSTRAINT TRIGGER test_governance_activation_failure AFTER INSERT ON workforce_employment_generations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION test_governance_activation_failure();`}}))
			defer func() {
				require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_activation_commit_failure_removed", SQL: "DROP TRIGGER test_governance_activation_failure ON workforce_employment_generations; DROP FUNCTION test_governance_activation_failure();"}}))
			}()
			refused := request(t, http.MethodPut, activationPath, intent, 503)
			require.Equal(t, "scoped_authorization_unavailable", refused["error"], "a late authority transaction failure is retryable, not invalid input")
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT set_config('app.assignment_access','authorized',true),set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_generation','read',true),set_config('app.workforce_generation_staff',?,true)", roots[0].String(), staff[0].String()).Error; err != nil {
					return err
				}
				var current struct {
					EmploymentStatus         string
					Version, EmploymentEpoch int64
				}
				require.NoError(t, tx.Table("workforce_staff").Select("employment_status,version,employment_epoch").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, roots[0], staff[0]).Take(&current).Error)
				require.Equal(t, "inactive", current.EmploymentStatus)
				require.Equal(t, readyEmployment.StaffVersion, current.Version)
				require.Equal(t, readyEmployment.EmploymentEpoch, current.EmploymentEpoch)
				for _, table := range []string{"iam_employment_bases", "workforce_employment_generations"} {
					var count int64
					require.NoError(t, tx.Table(table).Where("tenant_id=? AND hospital_id=? AND staff_id=?", testutil.TenantA, roots[0], staff[0]).Count(&count).Error)
					require.Zero(t, count, "a failed commit leaves no basis or generation")
				}
				var audits int64
				require.NoError(t, tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND actor_subject=? AND action IN ?", testutil.TenantA, roots[0], "user-reviewer", []string{"workforce_staff.update", "workforce.employment.activated"}).Count(&audits).Error)
				require.Zero(t, audits, "the failed physical transition and activation audits roll back together")
				return nil
			}))
		})
		result := request(t, http.MethodPut, activationPath, intent, 200)
		require.Equal(t, "effective", result["current_state"])
		created := result["generation"].(map[string]any)
		require.Equal(t, "user-ordinary", created["declared_subject"])
		require.Equal(t, generation.String(), created["id"])
		request(t, http.MethodPut, activationPath, intent, 200)
		consumed := request(t, http.MethodGet, sourcePath, nil, 200)
		require.Equal(t, "consumed", consumed["current_state"])
		require.Equal(t, false, consumed["can_activate"])
		var activationProof json.RawMessage
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.assignment_access','authorized',true)").Error; err != nil {
				return err
			}
			var basis struct {
				StaffID, GenerationID uuid.UUID
				EmploymentEpoch       int64
			}
			if err := tx.Table("iam_employment_bases").Select("staff_id,generation_id,employment_epoch").Where("tenant_id=? AND hospital_id=? AND subject='user-ordinary'", testutil.TenantA, roots[0]).Take(&basis).Error; err != nil {
				return err
			}
			require.Equal(t, staff[0], basis.StaffID)
			require.Equal(t, generation, basis.GenerationID)
			require.Equal(t, readyEmployment.EmploymentEpoch+1, basis.EmploymentEpoch)
			var retained struct{ Authority json.RawMessage }
			if err := tx.Table("iam_employment_basis_authorities").Select("authority").Where("tenant_id=? AND generation_id=?", testutil.TenantA, generation).Take(&retained).Error; err != nil {
				return err
			}
			var proof map[string]any
			require.NoError(t, json.Unmarshal(retained.Authority, &proof))
			require.Equal(t, activationGrant.String(), proof["source_grant_id"])
			require.Equal(t, "workforce.employment.activate", proof["capability"])
			if err := tx.Exec("SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_generation','read',true),set_config('app.workforce_generation_staff',?,true)", roots[0].String(), staff[0].String()).Error; err != nil {
				return err
			}
			var generationProof struct{ GovernanceAuthority json.RawMessage }
			if err := tx.Table("workforce_employment_generations").Select("governance_authority").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", testutil.TenantA, roots[0], staff[0], generation).Take(&generationProof).Error; err != nil {
				return err
			}
			var retainedGeneration map[string]any
			require.NoError(t, json.Unmarshal(generationProof.GovernanceAuthority, &retainedGeneration))
			require.Equal(t, activationGrant.String(), retainedGeneration["source_grant_id"])
			require.Equal(t, generation.String(), retainedGeneration["generation_id"])
			require.Equal(t, "employment_activation", retainedGeneration["purpose"])
			require.NotContains(t, retainedGeneration, "actor_staff_id")
			require.NotContains(t, retainedGeneration, "actor_link_id")
			activationProof = append(json.RawMessage(nil), generationProof.GovernanceAuthority...)
			var transitionAudits, activationAudits int64
			require.NoError(t, tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND actor_subject=? AND action=? AND changes=?::jsonb", testutil.TenantA, roots[0], staff[0], "user-reviewer", "workforce_staff.update", `{"old_level":null,"new_level":null,"old_status":"inactive","new_status":"active","target_subject":null}`).Count(&transitionAudits).Error)
			require.EqualValues(t, 1, transitionAudits, "activation and replay retain exactly one automatic staff transition audit")
			require.NoError(t, tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND actor_subject=? AND action=? AND changes @> ?::jsonb", testutil.TenantA, roots[0], generation, "user-reviewer", "workforce.employment.activated", string(generationProof.GovernanceAuthority)).Count(&activationAudits).Error)
			require.EqualValues(t, 1, activationAudits, "activation retains its separate current-source proof")
			var roles []string
			if err := tx.Table("iam_members").Where("tenant_id=? AND subject='user-ordinary'", testutil.TenantA).Order("role_key").Pluck("role_key", &roles).Error; err != nil {
				return err
			}
			require.Equal(t, []string{"staff_member"}, roles, "activation must not grant legacy operating roles")
			var links int64
			if err := tx.Table("workforce_login_links").Where("tenant_id=? AND hospital_id=? AND staff_id=?", testutil.TenantA, roots[0], staff[0]).Count(&links).Error; err != nil {
				return err
			}
			require.Zero(t, links, "employment is separate from reviewed account linking")
			return nil
		}))
		for _, test := range []struct {
			name, actor, action, changes string
			resource                     uuid.UUID
			sourceOnly                   bool
		}{
			{"other staff", "user-reviewer", "workforce_staff.update", `{"old_level":null,"new_level":null,"old_status":"inactive","new_status":"active","target_subject":null}`, staff[1], false},
			{"other actor", "user-admin", "workforce_staff.update", `{"old_level":null,"new_level":null,"old_status":"inactive","new_status":"active","target_subject":null}`, staff[0], false},
			{"other action", "user-reviewer", "workforce_staff.insert", `{"old_level":null,"new_level":null,"old_status":"inactive","new_status":"active","target_subject":null}`, staff[0], false},
			{"other change", "user-reviewer", "workforce_staff.update", `{"old_level":null,"new_level":null,"old_status":"active","new_status":"inactive","target_subject":null}`, staff[0], false},
			{"account target", "user-reviewer", "workforce_staff.update", `{"old_level":null,"new_level":null,"old_status":"inactive","new_status":"active","target_subject":"user-ordinary"}`, staff[0], false},
			{"missing canonical field", "user-reviewer", "workforce_staff.update", `{"old_level":null,"new_level":null,"old_status":"inactive","new_status":"active"}`, staff[0], false},
			{"extra private field", "user-reviewer", "workforce_staff.update", `{"old_level":null,"new_level":null,"old_status":"inactive","new_status":"active","target_subject":null,"display_name":"unrelated"}`, staff[0], false},
			{"read purpose", "user-reviewer", "workforce_staff.update", `{"old_level":null,"new_level":null,"old_status":"inactive","new_status":"active","target_subject":null}`, staff[0], true},
		} {
			t.Run("audit rejects "+test.name, func(t *testing.T) {
				proof := activationProof
				if test.sourceOnly {
					var values map[string]any
					require.NoError(t, json.Unmarshal(proof, &values))
					values["operation"] = "source"
					var err error
					proof, err = json.Marshal(values)
					require.NoError(t, err)
				}
				writeErr := db.WithTenant(tenantdb.WithActor(ctx, "user-reviewer"), testutil.TenantA, func(tx *gorm.DB) error {
					if err := tx.Exec("SELECT set_config('app.workforce_subject','user-reviewer',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_activation_authority',?,true)", roots[0].String(), staff[0].String(), readyEmployment.ID.String(), string(proof)).Error; err != nil {
						return err
					}
					return tx.Exec("INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,?::jsonb)", testutil.TenantA, roots[0], test.actor, test.action, test.resource, test.changes).Error
				})
				var denied *pgconn.PgError
				require.ErrorAs(t, writeErr, &denied)
				require.Equal(t, "42501", denied.Code)
			})
		}
		call(http.MethodPut, "/v1/iam/governance-grants/"+activationGrant.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "admin", map[string]any{"expected_version": 2, "decision": "revoke", "reason": "Withdraw activation authority after the completed digital hire"}, 202)
		request(t, http.MethodGet, sourcePath, nil, 403)
		request(t, http.MethodPut, activationPath, intent, 403)
	})

}

func assertReadPurposeBlocksStaffMasterEdit(t *testing.T, db *tenantdb.DB, hospital, staff uuid.UUID, setting string, proof json.RawMessage) {
	t.Helper()
	var sourceQuery string
	switch setting {
	case "app.workforce_governance_draft_authority":
		sourceQuery = "SELECT workforce_governance_draft_source(?::uuid,?::uuid,?::uuid)"
	case "app.workforce_governance_finding_authority":
		sourceQuery = "SELECT workforce_governance_finding_source(?::uuid,?::uuid,?::uuid)"
	case "app.workforce_employment_inbox_authority":
		sourceQuery = "SELECT workforce_employment_inbox_source(?::uuid,?::uuid)"
	default:
		t.Fatal("Unknown read-purpose fixture")
	}
	ctx := tenantdb.WithActor(t.Context(), "user-admin")
	scoped := func(fn func(*gorm.DB) error) error {
		return db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_staff',?,true)", hospital.String(), staff.String()).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	var before struct{ DisplayName string }
	require.NoError(t, scoped(func(tx *gorm.DB) error {
		var manager bool
		require.NoError(t, tx.Raw("SELECT workforce_staff_allowed(?::uuid,?::uuid,true)", testutil.TenantA, hospital).Scan(&manager).Error)
		require.True(t, manager, "the retained source probe must start with actual manager access")
		return tx.Table("workforce_staff").Select("display_name").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, hospital, staff).Take(&before).Error
	}))
	var affected int64
	writeErr := scoped(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config(?,?,true)", setting, string(proof)).Error; err != nil {
			return err
		}
		args := []any{testutil.TenantA, hospital}
		if setting != "app.workforce_employment_inbox_authority" {
			args = append(args, staff)
		}
		var valid bool
		require.NoError(t, tx.Raw(sourceQuery, args...).Scan(&valid).Error)
		require.True(t, valid, "an expired or mismatched proof cannot establish purpose isolation")
		var visible int64
		require.NoError(t, tx.Table("workforce_staff").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, hospital, staff).Count(&visible).Error)
		require.EqualValues(t, 1, visible, "read purpose must expose the genuine selected Staff before testing writes")
		result := tx.Exec("UPDATE workforce_staff SET display_name='Forbidden read-purpose master edit' WHERE tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, hospital, staff)
		affected = result.RowsAffected
		return result.Error
	})
	if writeErr != nil {
		var denied *pgconn.PgError
		require.ErrorAs(t, writeErr, &denied)
		require.Equal(t, "42501", denied.Code)
	} else {
		require.Zero(t, affected, "a read purpose must never update a master row despite manager access")
	}
	var after struct{ DisplayName string }
	require.NoError(t, scoped(func(tx *gorm.DB) error {
		return tx.Table("workforce_staff").Select("display_name").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, hospital, staff).Take(&after).Error
	}))
	require.Equal(t, before, after, "the denied write must preserve the stored Staff master")
}
