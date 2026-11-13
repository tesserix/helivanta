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
	for _, subject := range []string{"user-auditor", "user-reviewer"} {
		require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, subject, authz.RoleStaffMember))
	}
	modules := []platform.Module{iam.New(nil).SetMembershipIdentityReader(&governanceNativeIdentity{}), workforce.New()}
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Modules: modules, Documents: documents, Writer: graph, Roles: graph, Membership: graph,
		ScopedAuthorizer: func(db *tenantdb.DB) authz.ScopedAuthorizer { return iam.NewScopedAuthorizer(db, graph) },
		Tokens:           map[string]string{"admin": testutil.TenantA, "owner2": testutil.TenantA, "auditor": testutil.TenantA, "reviewer": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":    {iam.PermGovernanceManage, iam.PermGovernanceRead, iam.PermGovernanceReview, workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage, workforce.PermIdentityChecksManage, workforce.PermIdentityChecksReview, workforce.PermEmploymentPrepare, workforce.PermEmploymentReview},
			"owner2":   {iam.PermGovernanceReview, workforce.PermIdentityChecksManage, workforce.PermIdentityChecksReview, workforce.PermEmploymentPrepare, workforce.PermEmploymentReview},
			"auditor":  {workforce.PermIdentityChecksManage, workforce.PermIdentityChecksReview, workforce.PermEmploymentPrepare, workforce.PermEmploymentReview},
			"reviewer": {workforce.PermIdentityChecksManage, workforce.PermIdentityChecksReview, workforce.PermEmploymentPrepare, workforce.PermEmploymentReview},
		},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin'),(?,'user-owner2','tenant_admin'),(?,'user-auditor','staff_member'),(?,'user-reviewer','staff_member')", testutil.TenantA, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error
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
	input := map[string]any{"command_id": uuid.Must(uuid.NewV7()), "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "identity_hash": source["identity_hash"], "policy_reference": "SYN-PERSON-POLICY", "finding_reference": "Human identity examination", "observed_on": time.Now().UTC().Format(time.DateOnly), "valid_until": time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond), "access_intent": "non_digital", "declared_subject": "", "identity_examined": true, "known_associations_disclosed": true, "document_ids": []string{file}}
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
	t.Run("employment submission and independent decision need no Staff actor", func(t *testing.T) {
		request := func(method, path, token string, input any, status int) map[string]any {
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
			request(http.MethodPut, "/v1/iam/governance-grants/"+id.String(), "admin", iam.GovernanceGrantInput{Subject: subject, ExpectedMembershipEpoch: 1, TemplateKey: key, ScopeKind: "hospital", ScopeID: &root, Reason: "Synthetic nonclinical employment review", ValidFrom: time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond), ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}, 202)
			request(http.MethodPut, "/v1/iam/governance-grants/"+id.String()+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "owner2", map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent owner confirms exact employment purpose"}, 202)
			return id
		}
		period := uuid.Must(uuid.NewV7())
		terms := request(http.MethodPut, target+"/employment-periods/"+period.String(), "admin", map[string]any{"command_id": uuid.Must(uuid.NewV7()), "expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "engagement_kind": "employee", "duration": "ongoing", "starts_at": time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond), "reason": "Synthetic appointment", "evidence_reference": "SYN-APPOINTMENT"}, 200)
		employmentFile := uuid.Must(uuid.NewV7()).String()
		path := target + "/documents/" + employmentFile
		request(http.MethodPut, path, "admin", map[string]any{"record_kind": "enrollment", "record_id": staff[0], "category": "employment_evidence", "filename": "synthetic-appointment.png", "content_type": "image/png", "size": pngBytes.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(pngBytes.Bytes()))}, 201)
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
		binding := request(http.MethodPut, target+"/employment-periods/"+period.String()+"/revisions/1/evidence/"+bindingID.String(), "admin", map[string]any{"command_id": uuid.Must(uuid.NewV7()), "document_id": employmentFile, "reason": "Released appointment evidence"}, 200)
		employmentReview := uuid.Must(uuid.NewV7())
		employmentPath := target + "/employment-reviews/" + employmentReview.String()
		input := map[string]any{"command_id": uuid.Must(uuid.NewV7()), "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "identity_hash": source["identity_hash"], "period_id": period, "period_revision_id": terms["revision_id"], "period_version": terms["version"], "person_review_id": reviewID, "person_review_revision_id": approved["revision_id"], "person_review_version": approved["version"], "evidence": []map[string]any{{"id": bindingID, "revision_id": binding["revision_id"], "version": binding["version"]}}, "policy_reference": "SYN-EMPLOYMENT-POLICY", "reason": "Freeze exact reviewed person and appointment terms", "terms_examined": true, "known_associations_disclosed": true}
		request(http.MethodPut, employmentPath+"?hr_intake=v1", "auditor", input, 403)
		prepareGrant := grant("user-auditor", "workforce.employment_preparer.v1", roots[0])
		pending := request(http.MethodPut, employmentPath+"?hr_intake=v1", "auditor", input, 200)
		require.Equal(t, "pending", pending["status"])
		request(http.MethodPut, employmentPath+"?hr_intake=v1", "auditor", input, 200)
		request(http.MethodGet, employmentPath+"?employment_review=v1", "reviewer", nil, 403)
		employmentGrant := grant("user-reviewer", "workforce.employment_reviewer.v1", roots[0])
		view := request(http.MethodGet, employmentPath+"?employment_review=v1", "reviewer", nil, 200)
		require.Equal(t, true, view["can_approve"])
		decision := map[string]any{"command_id": uuid.Must(uuid.NewV7()), "expected_version": 1, "decision": "approve", "reason": "Independent appointment and evidence examination", "terms_examined": true, "independent_person_confirmed": true}
		request(http.MethodPost, employmentPath+"/decision?employment_review=v1", "auditor", decision, 403)
		request(http.MethodPost, employmentPath+"/decision?employment_review=v1", "admin", decision, 403)
		approved := request(http.MethodPost, employmentPath+"/decision?employment_review=v1", "reviewer", decision, 200)
		require.Equal(t, "approved", approved["status"])
		enrollment := request(http.MethodGet, target+"/enrollment", "admin", nil, 200)
		require.Equal(t, "inactive", enrollment["staff"].(map[string]any)["employment_status"], "approval is not employment activation or operating access")
		request(http.MethodPost, employmentPath+"/decision?employment_review=v1", "reviewer", decision, 200)
		stale := map[string]any{"command_id": uuid.Must(uuid.NewV7()), "expected_version": 1, "decision": "approve", "reason": "Stale review decision must not advance history", "terms_examined": true, "independent_person_confirmed": true}
		request(http.MethodPost, employmentPath+"/decision?employment_review=v1", "reviewer", stale, 409)
		request(http.MethodGet, "/v1/workforce/hospitals/"+roots[0].String()+"/staff/"+staff[1].String()+"/employment-reviews/"+employmentReview.String()+"?employment_review=v1", "reviewer", nil, 404)
		request(http.MethodGet, employmentPath+"/history?employment_review=v1", "reviewer", nil, 200)
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
		request(http.MethodGet, employmentPath+"?hr_intake=v1", "admin", nil, 200)
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
		request(http.MethodGet, employmentPath+"?employment_review=v1", "reviewer", nil, 403)
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
}
