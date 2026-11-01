package globalapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"maps"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
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

type workforceBeforeDocumentProvenance struct{ *workforce.Module }

func (m *workforceBeforeDocumentProvenance) Migrations() []tenantdb.Migration {
	var before []tenantdb.Migration
	for _, migration := range m.Module.Migrations() {
		if migration.ID == "0059_workforce_employment_document_provenance" {
			break
		}
		before = append(before, migration)
	}
	return before
}

func TestPreEmploymentPersonReviewPreservesInactiveJoiner(t *testing.T) {
	ctx := t.Context()
	cloud := testinfra.NewGCSProtocol(t)
	documents, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	modules := []platform.Module{&scopedLeaveIAMModule{iam.New(nil)}, &workforceBeforeDocumentProvenance{workforce.New()}}
	registry := platform.NewRegistry()
	for _, module := range modules {
		require.NoError(t, registry.Register(module))
	}
	require.NoError(t, platform.ReconcileTenant(ctx, registry, graph, testutil.TenantA))
	for _, role := range []authz.Role{authz.RoleTenantAdmin, authz.RoleStaffMember} {
		require.NoError(t, graph.GrantTenantRole(ctx, testutil.TenantA, role))
	}
	const maker, checker, manager, reviewer = "synthetic-person-maker", "synthetic-person-checker", "synthetic-person-manager", "synthetic-person-reviewer"
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
	for _, subject := range []string{maker, checker, manager, reviewer} {
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
	pausedDownload := func(t *testing.T, contentPath string, mutate func()) *httptest.ResponseRecorder {
		t.Helper()
		paused, resume := make(chan struct{}), make(chan struct{})
		release := sync.OnceFunc(func() { close(resume) })
		defer release()
		cloud.AfterReleasedRead(func() { close(paused); <-resume })
		defer cloud.AfterReleasedRead(nil)
		response := make(chan *httptest.ResponseRecorder, 1)
		go func() { response <- request("GET", contentPath, reviewer, nil, "application/json") }()
		select {
		case <-paused:
		case result := <-response:
			t.Fatalf("download ended before the storage pause: status %d", result.Code)
		case <-time.After(5 * time.Second):
			t.Fatal("download did not reach the storage boundary")
		}
		mutate()
		select {
		case result := <-response:
			t.Fatalf("download ended before the authority mutation completed: status %d", result.Code)
		default:
		}
		release()
		select {
		case result := <-response:
			return result
		case <-time.After(5 * time.Second):
			t.Fatal("download did not finish after releasing storage")
			return nil
		}
	}
	call := func(t *testing.T, method, path, subject string, input any, status int) map[string]any {
		t.Helper()
		var body []byte
		if input != nil {
			body, err = json.Marshal(input)
			require.NoError(t, err)
		}
		w := request(method, path, subject, body, "application/json")
		require.Equal(t, status, w.Code, "%s %s: %s", method, path, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	for _, member := range []struct{ subject, role string }{{maker, "tenant_admin"}, {checker, "tenant_admin"}, {manager, "staff_member"}, {reviewer, "staff_member"}} {
		call(t, "POST", "/v1/iam/members", maker, map[string]any{"subject": member.subject, "role_key": member.role}, 202)
	}
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	call(t, "PUT", base, maker, map[string]any{"name": "Synthetic Pre-employment Review Hospital"}, 200)
	for _, subject := range []string{maker, checker} {
		call(t, "PUT", base+"/access/"+subject, maker, map[string]any{"level": "manager"}, 200)
	}
	authority := iam.NewScopedAuthorizer(db, graph)
	type officerReference struct {
		StaffID, LinkPath, AssignmentPath string
		LinkVersion                       float64
	}
	officers := map[string]officerReference{}
	for i, officer := range []struct {
		subject, template string
		permission        authz.Permission
	}{{manager, "workforce.identity_manager.v1", workforce.PermIdentityChecksManage}, {reviewer, "workforce.identity_reviewer.v1", workforce.PermIdentityChecksReview}} {
		require.Eventually(t, func() bool {
			member, e := graph.IsMember(ctx, officer.subject, testutil.TenantA)
			return e == nil && member
		}, 10*time.Second, 20*time.Millisecond)
		actor := uuid.NewString()
		call(t, "PUT", base+"/staff/"+actor, maker, map[string]any{"employee_code": fmt.Sprintf("SYN-PERSON-OFFICER-%d", i), "display_name": officer.subject, "job_category": "other", "employment_status": "active"}, 200)
		link := base + "/staff/" + actor + "/login-links/" + uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", link, maker, map[string]any{"subject": officer.subject, "reason": "Synthetic reviewed identity officer"}, 200)
		linked := call(t, "POST", link+"/decision", checker, map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic officer identity"}, 200)
		proposal := "/v1/iam/assignment-proposals/" + uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", proposal, maker, map[string]any{"hospital_id": hospital, "subject": officer.subject, "template_key": officer.template, "reason": "Synthetic independent person review", "valid_from": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, 200)
		require.Eventually(t, func() bool { return call(t, "GET", proposal, maker, nil, 200)["status"] == "pending_review" }, 10*time.Second, 20*time.Millisecond)
		call(t, "POST", proposal+"/decision", checker, map[string]any{"expected_version": 2, "decision": "approve", "reason": "Independent scoped officer assignment"}, 200)
		require.Eventually(t, func() bool {
			grant, e := authority.AuthorizeScope(ctx, authz.ScopeRequest{TenantID: testutil.TenantA, Subject: officer.subject, HospitalID: hospital, Permission: officer.permission})
			return e == nil && grant.AssignmentID != ""
		}, 10*time.Second, 20*time.Millisecond)
		officers[officer.subject] = officerReference{StaffID: actor, LinkPath: link, AssignmentPath: proposal, LinkVersion: linked["version"].(float64)}
	}
	t.Run("person review scope discovery preserves separate capabilities", func(t *testing.T) {
		for _, actor := range []struct {
			subject        string
			submit, review bool
		}{{manager, true, false}, {reviewer, false, true}} {
			page := call(t, "GET", "/v1/workforce/person-review-scopes?limit=20", actor.subject, nil, 200)
			rows := page["data"].([]any)
			require.Len(t, rows, 1)
			require.Equal(t, map[string]any{"hospital_id": hospital, "name": "Synthetic Pre-employment Review Hospital", "can_submit": actor.submit, "can_review": actor.review}, rows[0])
			require.Equal(t, map[string]any{"has_more": false, "next_cursor": nil}, page["page"])
		}
		page := call(t, "GET", "/v1/workforce/person-review-scopes", maker, nil, 200)
		require.Empty(t, page["data"], "legacy hospital administration does not manufacture identity review authority")
		call(t, "GET", "/v1/workforce/person-review-scopes?limit=21", reviewer, nil, 400)
		for _, missing := range []string{"X-Helivanta-Expected-Tenant", "X-Helivanta-Expected-Subject"} {
			r := httptest.NewRequest("GET", "/v1/workforce/person-review-scopes", nil)
			r.Header.Set("Authorization", "Bearer "+tokens[reviewer])
			r.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
			r.Header.Set("X-Helivanta-Expected-Subject", reviewer)
			r.Header.Del(missing)
			w := httptest.NewRecorder()
			gateway.ServeHTTP(w, r)
			require.Equal(t, 409, w.Code, w.Body.String())
		}
		otherHospital := uuid.NewString()
		otherBase := "/v1/workforce/hospitals/" + otherHospital
		call(t, "PUT", otherBase, maker, map[string]any{"name": "Synthetic Unassigned Hospital"}, 200)
		for _, subject := range []string{maker, checker} {
			call(t, "PUT", otherBase+"/access/"+subject, maker, map[string]any{"level": "manager"}, 200)
		}
		otherActor := uuid.NewString()
		otherStaff := otherBase + "/staff/" + otherActor
		call(t, "PUT", otherStaff, maker, map[string]any{"employee_code": "SYN-OTHER-REVIEWER", "display_name": reviewer, "job_category": "other", "employment_status": "active"}, 200)
		otherLink := otherStaff + "/login-links/" + uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", otherLink, maker, map[string]any{"subject": reviewer, "reason": "Synthetic link without identity assignment"}, 200)
		call(t, "POST", otherLink+"/decision", checker, map[string]any{"expected_version": 1, "decision": "approve", "reason": "Synthetic separate hospital actor link"}, 200)
		first := call(t, "GET", "/v1/workforce/person-review-scopes?limit=1", reviewer, nil, 200)
		require.Empty(t, first["data"], "an actor link without a scoped identity assignment discloses no hospital")
		firstPage := first["page"].(map[string]any)
		require.Equal(t, true, firstPage["has_more"])
		cursor, ok := firstPage["next_cursor"].(string)
		require.True(t, ok)
		require.NotEmpty(t, cursor)
		second := call(t, "GET", "/v1/workforce/person-review-scopes?limit=1&cursor="+cursor, reviewer, nil, 200)
		require.Len(t, second["data"].([]any), 1)
		require.Equal(t, hospital, second["data"].([]any)[0].(map[string]any)["hospital_id"])
		call(t, "GET", "/v1/workforce/person-review-scopes?limit=1&cursor="+cursor, manager, nil, 400)
		call(t, "GET", "/v1/workforce/hr-intake-scopes?limit=1&cursor="+cursor, reviewer, nil, 400)
		require.NoError(t, platform.ReconcileTenant(ctx, registry, graph, testutil.TenantB))
		require.NoError(t, graph.GrantTenantRole(ctx, testutil.TenantB, authz.RoleStaffMember))
		require.NoError(t, graph.GrantRole(ctx, testutil.TenantB, reviewer, authz.RoleStaffMember))
		foreignToken, e := signer.Mint(reviewer, testutil.TenantB, time.Now(), time.Now().Add(time.Hour))
		require.NoError(t, e)
		r := httptest.NewRequest("GET", "/v1/workforce/person-review-scopes", nil)
		r.Header.Set("Authorization", "Bearer "+foreignToken)
		r.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
		r.Header.Set("X-Helivanta-Expected-Subject", reviewer)
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, r)
		require.Equal(t, 200, w.Code, w.Body.String())
		var foreignPage map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &foreignPage))
		require.Empty(t, foreignPage["data"], "the same identity's links and assignments in another tenant remain invisible")
		var audits int64
		require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, maker), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true)`, maker, hospital).Error; err != nil {
				return err
			}
			return tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND actor_subject=? AND action='workforce.person_review_scope.read'", testutil.TenantA, hospital, reviewer).Count(&audits).Error
		}))
		require.Positive(t, audits, "successful discovery must leave attributable private-read evidence")
	})
	staff := uuid.NewString()
	staffBase := base + "/staff/" + staff
	call(t, "PUT", staffBase, maker, map[string]any{"employee_code": "SYN-INACTIVE-PERSON", "display_name": "Synthetic Non-digital Joiner", "job_category": "other", "employment_status": "inactive"}, 200)
	t.Run("manager discovers inactive targets without HR editing authority", func(t *testing.T) {
		page := call(t, "POST", base+"/person-review-targets/search?limit=1", manager, map[string]any{"employee_code": "syn-inactive-person"}, 200)
		rows := page["data"].([]any)
		require.Len(t, rows, 1)
		row := rows[0].(map[string]any)
		require.Equal(t, staff, row["id"])
		require.Equal(t, "SYN-INACTIVE-PERSON", row["employee_code"])
		require.Equal(t, "inactive", row["employment_status"])
		require.NotContains(t, row, "date_of_birth")
		call(t, "POST", base+"/person-review-targets/search", reviewer, map[string]any{"employee_code": ""}, 403)
		call(t, "POST", base+"/person-review-targets/search", maker, map[string]any{"employee_code": ""}, 403)
		require.Empty(t, call(t, "POST", base+"/person-review-targets/search", manager, map[string]any{"employee_code": "missing-staff-code"}, 200)["data"])
		seen := map[string]bool{}
		cursor := ""
		for i := range 3 {
			page := call(t, "POST", base+"/person-review-targets/search?limit=1"+cursor, manager, map[string]any{"employee_code": ""}, 200)
			rows := page["data"].([]any)
			require.Len(t, rows, 1)
			id := rows[0].(map[string]any)["id"].(string)
			require.False(t, seen[id], "target pages must not repeat staff")
			seen[id] = true
			info := page["page"].(map[string]any)
			require.Equal(t, i < 2, info["has_more"])
			if i < 2 {
				cursor = "&cursor=" + info["next_cursor"].(string)
			} else {
				require.Nil(t, info["next_cursor"])
			}
		}
		require.Len(t, seen, 3, "browse stays within the assigned hospital")
		for _, invalid := range []map[string]any{{}, {"employee_code": nil}, {"employee_code": " "}, {"employee_code": "", "tenant_id": testutil.TenantB}} {
			call(t, "POST", base+"/person-review-targets/search", manager, invalid, 400)
		}
		withTargets := func(action func(*gorm.DB) error) error {
			return db.WithTenantCommand(tenantdb.WithActor(ctx, manager), testutil.TenantA, func(tx *gorm.DB) error {
				ref := officers[manager]
				if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_person_browse','targets',true),set_config('app.workforce_person_browse_actor',?,true),set_config('app.workforce_person_browse_link',?,true)`, manager, hospital, ref.StaffID, ref.LinkPath[strings.LastIndex(ref.LinkPath, "/")+1:]).Error; err != nil {
					return err
				}
				return action(tx)
			})
		}
		writeErr := withTargets(func(tx *gorm.DB) error {
			return tx.Table("workforce_staff").Where("tenant_id=? AND hospital_id=? AND id=?", testutil.TenantA, hospital, staff).Update("display_name", "Synthetic forbidden browse-context change").Error
		})
		var deniedWrite *pgconn.PgError
		require.ErrorAs(t, writeErr, &deniedWrite)
		require.Equal(t, "42501", deniedWrite.Code)
		deleteErr := withTargets(func(tx *gorm.DB) error {
			return tx.Exec(`DELETE FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=?`, testutil.TenantA, hospital, staff).Error
		})
		var deniedDelete *pgconn.PgError
		require.ErrorAs(t, deleteErr, &deniedDelete)
		require.Equal(t, "23514", deniedDelete.Code, "the existing destructive-operation guard remains active")
	})
	source := call(t, "GET", base+"/identity-staff/"+staff, manager, nil, 200)
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	legacyFiles := map[string]string{}
	for _, category := range []string{"identity_evidence", "employment_evidence"} {
		id := uuid.Must(uuid.NewV7()).String()
		legacyFiles[category] = id
		call(t, "PUT", staffBase+"/documents/"+id, maker, map[string]any{"record_kind": "enrollment", "record_id": staff, "category": category, "filename": "synthetic-legacy-evidence.png", "content_type": "image/png", "size": imageBytes.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(imageBytes.Bytes()))}, 201)
	}
	var provenanceMigration []tenantdb.Migration
	upgrade := false
	for _, migration := range workforce.New().Migrations() {
		upgrade = upgrade || migration.ID == "0059_workforce_employment_document_provenance"
		if upgrade {
			provenanceMigration = append(provenanceMigration, migration)
		}
	}
	require.NotEmpty(t, provenanceMigration)
	require.Equal(t, "0059_workforce_employment_document_provenance", provenanceMigration[0].ID)
	require.NoError(t, db.Migrate(ctx, provenanceMigration))
	file := uuid.Must(uuid.NewV7()).String()
	filePath := staffBase + "/documents/" + file
	call(t, "PUT", filePath, maker, map[string]any{"record_kind": "enrollment", "record_id": staff, "category": "identity_evidence", "filename": "synthetic-joiner-identity.png", "content_type": "image/png", "size": imageBytes.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(imageBytes.Bytes()))}, 201)
	require.Equal(t, 202, request("PUT", filePath+"/content", maker, imageBytes.Bytes(), "image/png").Code)
	var database string
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(ctx, testinfra.DocumentWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(ctx, worker, documents, scanner)
	require.NoError(t, err)
	state, err := processor.Process(ctx, workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: hospital, StaffID: staff, FileID: file, ScanAttempt: 1})
	require.NoError(t, err)
	require.Equal(t, "available", state)
	t.Run("identity manager selects source-bound released evidence without general document access", func(t *testing.T) {
		selection := map[string]any{"staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "identity_hash": source["identity_hash"]}
		endpoint := base + "/identity-staff/" + staff + "/person-review-evidence/search"
		page := call(t, "POST", endpoint, manager, selection, 200)
		rows := page["data"].([]any)
		require.Len(t, rows, 1)
		row := rows[0].(map[string]any)
		require.Equal(t, file, row["id"])
		require.Equal(t, "synthetic-joiner-identity.png", row["filename"])
		require.Equal(t, source["staff_version"], row["record_version"])
		require.Equal(t, hospital, row["hospital_id"])
		require.Equal(t, staff, row["staff_id"])
		for _, private := range []string{"source_generation", "released_generation", "actor_subject", "storage_key"} {
			require.NotContains(t, row, private)
		}
		call(t, "POST", endpoint, reviewer, selection, 403)
		call(t, "GET", staffBase+"/documents", manager, nil, 403)
		changed := map[string]any{"staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "identity_hash": strings.Repeat("f", 64)}
		call(t, "POST", endpoint, manager, changed, 409)
		for _, invalid := range []map[string]any{{}, {"staff_version": nil, "employment_epoch": 1, "identity_hash": source["identity_hash"]}, {"staff_version": source["staff_version"], "employment_epoch": nil, "identity_hash": source["identity_hash"]}} {
			call(t, "POST", endpoint, manager, invalid, 400)
		}
		addFile := func(target, category string, release bool) string {
			t.Helper()
			id := uuid.Must(uuid.NewV7()).String()
			filePath := base + "/staff/" + target + "/documents/" + id
			call(t, "PUT", filePath, maker, map[string]any{"record_kind": "enrollment", "record_id": target, "category": category, "filename": "synthetic-selection-exclusion.png", "content_type": "image/png", "size": imageBytes.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(imageBytes.Bytes()))}, 201)
			if release {
				require.Equal(t, 202, request("PUT", filePath+"/content", maker, imageBytes.Bytes(), "image/png").Code)
				state, e := processor.Process(ctx, workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: hospital, StaffID: target, FileID: id, ScanAttempt: 1})
				require.NoError(t, e)
				require.Equal(t, "available", state)
			}
			return id
		}
		addFile(staff, "identity_evidence", false)
		addFile(staff, "employment_evidence", true)
		otherStaff := uuid.NewString()
		call(t, "PUT", base+"/staff/"+otherStaff, maker, map[string]any{"employee_code": "SYN-SELECTION-OTHER", "display_name": "Synthetic Other Applicant", "job_category": "other", "employment_status": "inactive"}, 200)
		addFile(otherStaff, "identity_evidence", true)
		filtered := call(t, "POST", endpoint, manager, selection, 200)["data"].([]any)
		require.Len(t, filtered, 1, "unreleased, unrelated-category and other-Staff documents stay out of selection")
		require.Equal(t, file, filtered[0].(map[string]any)["id"])
		withSelection := func(tenant string, version any, action func(*gorm.DB) error) error {
			return db.WithTenantCommand(tenantdb.WithActor(ctx, manager), tenant, func(tx *gorm.DB) error {
				if e := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_person_review','manage',true),set_config('app.workforce_person_evidence_selection','authorized',true),set_config('app.workforce_person_evidence_version',?,true)`, manager, hospital, staff, fmt.Sprint(version)).Error; e != nil {
					return e
				}
				return action(tx)
			})
		}
		require.NoError(t, withSelection(testutil.TenantA, source["staff_version"], func(tx *gorm.DB) error {
			var visible []string
			if e := tx.Table("workforce_documents").Pluck("id", &visible).Error; e != nil {
				return e
			}
			require.Equal(t, []string{file}, visible, "metadata-purpose RLS itself excludes unrelated files")
			var audits int64
			if e := tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND actor_subject=? AND action='workforce.person_review.evidence_selection.read'", testutil.TenantA, hospital, file, manager).Count(&audits).Error; e != nil {
				return e
			}
			require.Positive(t, audits)
			return nil
		}))
		for _, scope := range []struct {
			tenant  string
			version any
		}{{testutil.TenantB, source["staff_version"]}, {testutil.TenantA, 999}} {
			require.NoError(t, withSelection(scope.tenant, scope.version, func(tx *gorm.DB) error {
				var count int64
				if e := tx.Table("workforce_documents").Count(&count).Error; e != nil {
					return e
				}
				require.Zero(t, count)
				return nil
			}))
		}
		writeErr := withSelection(testutil.TenantA, source["staff_version"], func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE workforce_documents SET updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=?`, testutil.TenantA, hospital, file).Error
		})
		var deniedWrite *pgconn.PgError
		require.ErrorAs(t, writeErr, &deniedWrite)
		require.Equal(t, "42501", deniedWrite.Code)
		deleteErr := withSelection(testutil.TenantA, source["staff_version"], func(tx *gorm.DB) error {
			return tx.Exec(`DELETE FROM workforce_documents WHERE tenant_id=? AND hospital_id=? AND id=?`, testutil.TenantA, hospital, file).Error
		})
		var deniedDelete *pgconn.PgError
		require.ErrorAs(t, deleteErr, &deniedDelete)
		require.Equal(t, "23514", deniedDelete.Code, "metadata selection retains the existing destructive-operation denial")
		ref := officers[manager]
		call(t, "POST", ref.LinkPath+"/decision", maker, map[string]any{"expected_version": ref.LinkVersion, "decision": "revoke", "reason": "Synthetic manager link cutoff for metadata selection"}, 200)
		call(t, "POST", endpoint, manager, selection, 403)
		newLink := base + "/staff/" + ref.StaffID + "/login-links/" + uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", newLink, maker, map[string]any{"subject": manager, "reason": "Synthetic independently reviewed manager link recovery"}, 200)
		linked := call(t, "POST", newLink+"/decision", checker, map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic recovered manager identity"}, 200)
		ref.LinkPath, ref.LinkVersion = newLink, linked["version"].(float64)
		officers[manager] = ref
		require.Len(t, call(t, "POST", endpoint, manager, selection, 200)["data"].([]any), 1)
	})
	path := staffBase + "/person-reviews/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{
		"command_id": uuid.Must(uuid.NewV7()).String(), "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "identity_hash": source["identity_hash"],
		"policy_reference": "SYN-HUMAN-IDENTITY-POLICY", "finding_reference": "Synthetic in-person examination; no government-provider assertion",
		"observed_on": time.Now().UTC().Format(time.DateOnly), "valid_until": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		"access_intent": "non_digital", "declared_subject": "", "identity_examined": true, "known_associations_disclosed": true, "document_ids": []string{file},
	}
	t.Run("later writes cannot repair missing creation provenance", func(t *testing.T) {
		for _, category := range []string{"identity_evidence", "employment_evidence"} {
			id := legacyFiles[category]
			legacyPath := staffBase + "/documents/" + id
			require.Equal(t, 202, request("PUT", legacyPath+"/content", checker, imageBytes.Bytes(), "image/png").Code)
			state, err := processor.Process(ctx, workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: hospital, StaffID: staff, FileID: id, ScanAttempt: 1})
			require.NoError(t, err)
			require.Equal(t, "available", state)
			require.NoError(t, db.WithTenantCommand(tenantdb.WithActor(ctx, maker), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, maker, hospital).Error; err != nil {
					return err
				}
				var complete, visible int64
				if err := tx.Table("workforce_document_contributors").Where("tenant_id=? AND hospital_id=? AND document_id=? AND creation_captured", testutil.TenantA, hospital, file).Count(&complete).Error; err != nil {
					return err
				}
				require.EqualValues(t, 1, complete, "the same inspection context sees the new identity file's captured creator")
				if err := tx.Table("workforce_document_contributors").Where("tenant_id=? AND hospital_id=? AND document_id=?", testutil.TenantA, hospital, id).Count(&visible).Error; err != nil {
					return err
				}
				require.Positive(t, visible, "a later write records a contributor but cannot prove complete legacy history")
				if err := tx.Table("workforce_document_contributors").Where("tenant_id=? AND hospital_id=? AND document_id=? AND creation_captured", testutil.TenantA, hospital, id).Count(&complete).Error; err != nil {
					return err
				}
				require.Zero(t, complete, "migration and later writes must not mark historical provenance complete")
				return nil
			}))
			if category == "identity_evidence" {
				legacyInput := map[string]any{}
				for key, value := range input {
					legacyInput[key] = value
				}
				legacyInput["command_id"] = uuid.Must(uuid.NewV7()).String()
				legacyInput["document_ids"] = []string{id}
				denied := call(t, "PUT", staffBase+"/person-reviews/"+uuid.Must(uuid.NewV7()).String(), manager, legacyInput, 409)
				require.Equal(t, "person_review_provenance_unavailable", denied["error"])
			}
		}
	})
	pending := call(t, "PUT", path, manager, input, 200)
	require.Equal(t, "pending", pending["status"])
	require.Equal(t, float64(1), pending["version"])
	require.Equal(t, pending, call(t, "PUT", path, manager, input, 200), "exact submission retry retains the immutable source")
	t.Run("SQL approval cannot omit complete provenance disclosure", func(t *testing.T) {
		err := db.WithTenantCommand(tenantdb.WithActor(ctx, reviewer), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_person_review','review',true)`, reviewer, hospital, staff).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE workforce_person_reviews SET status='approved',version=version+1,revision_id=?,actor_subject=?,decision_reason='Synthetic approval without document provenance context',reviewer_identity_examined=true,independent_person_confirmed=true,input_hash=? WHERE tenant_id=? AND hospital_id=? AND id=?`, uuid.Must(uuid.NewV7()), reviewer, strings.Repeat("a", 64), testutil.TenantA, hospital, pending["id"]).Error
		})
		var denied *pgconn.PgError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, "23514", denied.Code)
		require.Contains(t, denied.Message, "complete contributor provenance")
		require.Equal(t, pending, call(t, "GET", path, manager, nil, 200)["review"], "failed SQL approval leaves the pending head unchanged")
	})
	t.Run("person review commands roll back audit and outbox failures", func(t *testing.T) {
		state := func(t *testing.T, id string) (map[string]int64, int64, int64) {
			t.Helper()
			counts := map[string]int64{}
			var audits, notifications int64
			require.NoError(t, db.WithTenantCommand(tenantdb.WithActor(ctx, manager), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_person_review','manage',true),set_config('app.workforce_person_review_documents',?,true)`, manager, hospital, staff, file).Error; err != nil {
					return err
				}
				for _, table := range []struct{ name, column string }{{"workforce_person_reviews", "id"}, {"workforce_person_review_history", "id"}, {"workforce_person_review_evidence", "review_id"}, {"workforce_person_review_contributors", "review_id"}} {
					var count int64
					if e := tx.Table(table.name).Where("tenant_id=? AND hospital_id=? AND "+table.column+"=?", testutil.TenantA, hospital, id).Count(&count).Error; e != nil {
						return e
					}
					counts[table.name] = count
				}
				if e := tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND action IN ('workforce.person_review.submitted','workforce.person_review.decided')", testutil.TenantA, hospital, id).Count(&audits).Error; e != nil {
					return e
				}
				return tx.Table("outbox_events").Where("tenant_id=? AND subject LIKE ? AND payload->'data'->>'review_id'=?", testutil.TenantA, "%workforce.person_review_changed.v1", id).Count(&notifications).Error
			}))
			return counts, audits, notifications
		}
		visible, _, visibleEvents := state(t, pending["id"].(string))
		require.EqualValues(t, 1, visible["workforce_person_reviews"])
		require.EqualValues(t, 1, visible["workforce_person_review_history"])
		require.EqualValues(t, 1, visible["workforce_person_review_evidence"])
		require.Positive(t, visible["workforce_person_review_contributors"])
		require.EqualValues(t, 1, visibleEvents, "the same inspection context must see a known committed event")
		for _, stage := range []string{"audit", "outbox"} {
			t.Run(stage, func(t *testing.T) {
				id := uuid.Must(uuid.NewV7()).String()
				candidate := staffBase + "/person-reviews/" + id
				intent := map[string]any{}
				for key, value := range input {
					intent[key] = value
				}
				intent["command_id"] = uuid.Must(uuid.NewV7()).String()
				install := func(action string) {
					t.Helper()
					table, condition := "workforce_audit", fmt.Sprintf("NEW.action='%s' AND NEW.resource_id='%s'::uuid", action, id)
					if stage == "outbox" {
						table = "outbox_events"
						condition = fmt.Sprintf("NEW.subject LIKE '%%workforce.person_review_changed.v1' AND NEW.payload->'data'->>'review_id'='%s'", id)
					}
					require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{
						ID:  "test_person_review_fault_install_" + uuid.Must(uuid.NewV7()).String(),
						SQL: fmt.Sprintf(`CREATE FUNCTION test_person_review_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF %s THEN RAISE EXCEPTION 'synthetic person review persistence failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_person_review_fault BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION test_person_review_fault()`, condition, table),
					}}))
				}
				clear := func() {
					t.Helper()
					cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
					defer done()
					require.NoError(t, db.Migrate(cleanup, []tenantdb.Migration{{
						ID:  "test_person_review_fault_clear_" + uuid.Must(uuid.NewV7()).String(),
						SQL: `DROP FUNCTION IF EXISTS test_person_review_fault() CASCADE`,
					}}))
				}
				t.Cleanup(clear)
				install("workforce.person_review.submitted")
				call(t, "PUT", candidate, manager, intent, 500)
				call(t, "GET", candidate, manager, nil, 404)
				counts, audits, notifications := state(t, id)
				for name, count := range counts {
					require.Zero(t, count, "failed submission must not persist %s", name)
				}
				require.Zero(t, audits)
				require.Zero(t, notifications)
				clear()
				created := call(t, "PUT", candidate, manager, intent, 200)
				require.Equal(t, created, call(t, "PUT", candidate, manager, intent, 200), "recovery retries the exact original command")
				before, beforeAudits, beforeNotifications := state(t, id)
				require.EqualValues(t, 1, before["workforce_person_reviews"])
				require.EqualValues(t, 1, before["workforce_person_review_history"])
				require.EqualValues(t, 1, before["workforce_person_review_evidence"])
				require.Positive(t, before["workforce_person_review_contributors"])
				require.EqualValues(t, 1, beforeAudits)
				require.EqualValues(t, 1, beforeNotifications)
				decision := map[string]any{"expected_version": 1, "command_id": uuid.Must(uuid.NewV7()).String(), "decision": "approve", "reason": "Synthetic independent atomic approval recovery", "identity_examined": true, "independent_person_confirmed": true}
				install("workforce.person_review.decided")
				call(t, "POST", candidate+"/decision", reviewer, decision, 500)
				current := call(t, "GET", candidate, reviewer, nil, 200)["review"].(map[string]any)
				require.Equal(t, created, current, "failed approval retains the exact pending head")
				after, afterAudits, afterNotifications := state(t, id)
				require.Equal(t, before, after)
				require.Equal(t, beforeAudits, afterAudits)
				require.Equal(t, beforeNotifications, afterNotifications)
				clear()
				approved := call(t, "POST", candidate+"/decision", reviewer, decision, 200)
				require.Equal(t, approved, call(t, "POST", candidate+"/decision", reviewer, decision, 200))
				final, finalAudits, finalNotifications := state(t, id)
				require.EqualValues(t, 2, final["workforce_person_review_history"])
				require.EqualValues(t, 2, finalAudits)
				require.EqualValues(t, 2, finalNotifications)
				call(t, "POST", candidate+"/revocation", reviewer, map[string]any{"expected_version": 2, "command_id": uuid.Must(uuid.NewV7()).String(), "reason": "Synthetic atomicity probe completed; retire the finding without deleting history"}, 200)
			})
		}
	})
	t.Run("person review inbox exposes bounded summaries rather than private findings", func(t *testing.T) {
		for _, subject := range []string{manager, reviewer} {
			page := call(t, "GET", base+"/person-review-inbox?limit=1", subject, nil, 200)
			rows := page["data"].([]any)
			require.Len(t, rows, 1)
			row := rows[0].(map[string]any)
			require.Equal(t, pending["id"], row["id"])
			require.Equal(t, staff, row["staff_id"])
			require.Equal(t, "Synthetic Non-digital Joiner", row["display_name"])
			require.Equal(t, "pending", row["status"])
			for _, private := range []string{"finding_reference", "policy_reference", "declared_subject", "evidence", "decision_reason"} {
				require.NotContains(t, row, private)
			}
		}
		call(t, "GET", base+"/person-review-inbox", maker, nil, 403)
		call(t, "GET", base+"/person-review-inbox?status=unknown", reviewer, nil, 400)
		call(t, "GET", base+"/person-review-inbox?status=pending&status=approved", reviewer, nil, 400)
		withInbox := func(tenant string, action func(*gorm.DB) error) error {
			return db.WithTenantCommand(tenantdb.WithActor(ctx, manager), tenant, func(tx *gorm.DB) error {
				ref := officers[manager]
				if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_person_browse','inbox',true),set_config('app.workforce_person_browse_actor',?,true),set_config('app.workforce_person_browse_link',?,true)`, manager, hospital, ref.StaffID, ref.LinkPath[strings.LastIndex(ref.LinkPath, "/")+1:]).Error; err != nil {
					return err
				}
				return action(tx)
			})
		}
		require.NoError(t, withInbox(testutil.TenantA, func(tx *gorm.DB) error {
			for _, table := range []string{"workforce_documents", "workforce_person_review_history", "workforce_person_review_evidence", "workforce_person_review_contributors"} {
				var count int64
				if err := tx.Table(table).Count(&count).Error; err != nil {
					return err
				}
				require.Zero(t, count, "inbox purpose cannot read %s", table)
			}
			return nil
		}))
		require.NoError(t, withInbox(testutil.TenantB, func(tx *gorm.DB) error {
			for _, table := range []string{"workforce_staff", "workforce_person_reviews"} {
				var count int64
				if err := tx.Table(table).Count(&count).Error; err != nil {
					return err
				}
				require.Zero(t, count, "foreign tenant cannot browse %s", table)
			}
			return nil
		}))
		writeErr := withInbox(testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_person_review','manage',true)`).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE workforce_person_reviews SET version=version+1,revision_id=?,status='withdrawn',decision_reason='Synthetic forbidden browse-context withdrawal' WHERE tenant_id=? AND hospital_id=? AND id=?`, uuid.Must(uuid.NewV7()), testutil.TenantA, hospital, pending["id"]).Error
		})
		var deniedWrite *pgconn.PgError
		require.ErrorAs(t, writeErr, &deniedWrite)
		require.Equal(t, "42501", deniedWrite.Code, "inbox context cannot satisfy the owner write policy even with a manage-mode flag")
		secondInput := map[string]any{}
		for key, value := range input {
			secondInput[key] = value
		}
		secondInput["command_id"] = uuid.Must(uuid.NewV7()).String()
		secondInput["finding_reference"] = "Synthetic second separately recorded human observation"
		secondReview := call(t, "PUT", staffBase+"/person-reviews/"+uuid.Must(uuid.NewV7()).String(), manager, secondInput, 200)
		first := call(t, "GET", base+"/person-review-inbox?limit=1", reviewer, nil, 200)
		require.Equal(t, secondReview["id"], first["data"].([]any)[0].(map[string]any)["id"])
		info := first["page"].(map[string]any)
		require.Equal(t, true, info["has_more"])
		next := call(t, "GET", base+"/person-review-inbox?limit=1&cursor="+info["next_cursor"].(string), reviewer, nil, 200)
		require.Equal(t, pending["id"], next["data"].([]any)[0].(map[string]any)["id"])
		require.Equal(t, false, next["page"].(map[string]any)["has_more"])
		require.Empty(t, call(t, "GET", base+"/person-review-inbox?status=approved", reviewer, nil, 200)["data"])
	})
	evidencePath := path + "/evidence/" + file + "/content"
	released := request("GET", evidencePath, reviewer, nil, "application/json")
	require.Equal(t, 200, released.Code, released.Body.String())
	require.Equal(t, imageBytes.Bytes(), released.Body.Bytes())
	require.Contains(t, released.Header().Get("Cache-Control"), "no-store")
	require.NotContains(t, released.Header().Get("Cache-Control"), "public")
	require.Equal(t, "nosniff", released.Header().Get("X-Content-Type-Options"))
	decision := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "decision": "approve", "reason": "Independent synthetic in-person finding", "identity_examined": true, "independent_person_confirmed": true}
	call(t, "POST", path+"/decision", manager, decision, 403)
	approved := call(t, "POST", path+"/decision", reviewer, decision, 200)
	t.Run("employment submission freezes reviewed person terms and evidence without activating access", func(t *testing.T) {
		proposalID := uuid.Must(uuid.NewV7()).String()
		proposal := "/v1/iam/assignment-proposals/" + proposalID
		call(t, "PUT", proposal, maker, map[string]any{"hospital_id": hospital, "subject": manager, "template_key": "workforce.hr_intake.v1", "reason": "Synthetic scoped employment preparation", "valid_from": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, 200)
		require.Eventually(t, func() bool { return call(t, "GET", proposal, maker, nil, 200)["status"] == "pending_review" }, 10*time.Second, 20*time.Millisecond)
		call(t, "POST", proposal+"/decision", checker, map[string]any{"expected_version": 2, "decision": "approve", "reason": "Independent synthetic employment preparer authority"}, 200)
		for _, permission := range []authz.Permission{workforce.PermEnrollmentManage, workforce.PermDocumentManage} {
			require.Eventually(t, func() bool {
				grant, err := authority.AuthorizeScope(ctx, authz.ScopeRequest{TenantID: testutil.TenantA, Subject: manager, HospitalID: hospital, Permission: permission})
				return err == nil && grant.AssignmentID == proposalID
			}, 10*time.Second, 20*time.Millisecond)
		}
		currentSource := call(t, "GET", base+"/identity-staff/"+staff, manager, nil, 200)
		periodID := uuid.Must(uuid.NewV7()).String()
		periodPath := staffBase + "/employment-periods/" + periodID
		terms := call(t, "PUT", periodPath+"?hr_intake=v1", manager, map[string]any{
			"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0,
			"staff_version": currentSource["staff_version"], "employment_epoch": currentSource["employment_epoch"],
			"engagement_kind": "employee", "duration": "ongoing", "starts_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"contracting_party": "", "sponsor_id": "", "reason": "Synthetic independently reviewable appointment", "evidence_reference": "SYN-EMPLOYMENT-APPOINTMENT",
		}, 200)
		employmentFile := uuid.Must(uuid.NewV7()).String()
		employmentFilePath := staffBase + "/documents/" + employmentFile
		call(t, "PUT", employmentFilePath+"?hr_intake=v1", manager, map[string]any{"record_kind": "enrollment", "record_id": staff, "category": "employment_evidence", "filename": "synthetic-appointment.png", "content_type": "image/png", "size": imageBytes.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(imageBytes.Bytes()))}, 201)
		require.Equal(t, 202, request("PUT", employmentFilePath+"/content?hr_intake=v1", manager, imageBytes.Bytes(), "image/png").Code)
		state, err := processor.Process(ctx, workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: hospital, StaffID: staff, FileID: employmentFile, ScanAttempt: 1})
		require.NoError(t, err)
		require.Equal(t, "available", state)
		bindingID := uuid.Must(uuid.NewV7()).String()
		binding := call(t, "PUT", periodPath+"/revisions/1/evidence/"+bindingID+"?hr_intake=v1", manager, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "document_id": employmentFile, "reason": "Synthetic released appointment evidence"}, 200)
		submissionID := uuid.Must(uuid.NewV7()).String()
		submissionPath := staffBase + "/employment-reviews/" + submissionID
		command := map[string]any{
			"command_id": uuid.Must(uuid.NewV7()).String(), "staff_version": currentSource["staff_version"], "employment_epoch": currentSource["employment_epoch"], "identity_hash": currentSource["identity_hash"],
			"period_id": periodID, "period_revision_id": terms["revision_id"], "period_version": terms["version"],
			"person_review_id": approved["id"], "person_review_revision_id": approved["revision_id"], "person_review_version": approved["version"],
			"evidence":         []map[string]any{{"id": bindingID, "revision_id": binding["revision_id"], "version": binding["version"]}},
			"policy_reference": "SYN-APPOINTMENT-POLICY", "reason": "Synthetic frozen employment submission", "terms_examined": true, "known_associations_disclosed": true,
		}
		call(t, "PUT", submissionPath, manager, command, 400)
		call(t, "PUT", submissionPath+"?hr_intake=v1&hr_intake=v1", manager, command, 400)
		for _, subject := range []string{maker, reviewer} {
			denied := call(t, "PUT", submissionPath+"?hr_intake=v1", subject, command, 403)
			require.NotContains(t, denied, "terms", "legacy administration or identity review does not confer employment preparation")
		}
		for _, field := range []string{"staff_version", "employment_epoch", "period_version", "person_review_version", "terms_examined", "known_associations_disclosed"} {
			invalid := maps.Clone(command)
			delete(invalid, field)
			call(t, "PUT", submissionPath+"?hr_intake=v1", manager, invalid, 400)
		}
		invalid := maps.Clone(command)
		invalid["evidence"] = []map[string]any{{"id": bindingID, "revision_id": binding["revision_id"], "version": binding["version"]}, {"id": bindingID, "revision_id": binding["revision_id"], "version": binding["version"]}}
		call(t, "PUT", submissionPath+"?hr_intake=v1", manager, invalid, 400)
		submitted := call(t, "PUT", submissionPath+"?hr_intake=v1", manager, command, 200)
		require.Equal(t, "pending", submitted["status"])
		require.Equal(t, terms, submitted["terms"])
		require.Equal(t, approved["id"], submitted["person_review_id"])
		require.Equal(t, approved["revision_id"], submitted["person_review_revision_id"])
		require.Len(t, submitted["evidence"].([]any), 1)
		require.Equal(t, binding, submitted["evidence"].([]any)[0].(map[string]any)["binding"])
		require.Equal(t, submitted, call(t, "PUT", submissionPath+"?hr_intake=v1", manager, command, 200), "exact retry returns the frozen original submission")
		view := call(t, "GET", submissionPath+"?hr_intake=v1", manager, nil, 200)
		require.Equal(t, submitted, view["review"])
		require.Equal(t, "pending", view["current_state"])
		changed := maps.Clone(command)
		changed["reason"] = "Synthetic conflicting command intent"
		call(t, "PUT", submissionPath+"?hr_intake=v1", manager, changed, 409)
		changed = maps.Clone(command)
		changed["command_id"] = uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", submissionPath+"?hr_intake=v1", manager, changed, 409)
		call(t, "PUT", staffBase+"/employment-reviews/"+uuid.Must(uuid.NewV7()).String()+"?hr_intake=v1", manager, command, 409)
		for _, subject := range []string{maker, reviewer} {
			call(t, "GET", submissionPath+"?hr_intake=v1", subject, nil, 403)
		}

		withSubmission := func(id string, action func(*gorm.DB) error) error {
			return db.WithTenantCommand(tenantdb.WithActor(ctx, manager), testutil.TenantA, func(tx *gorm.DB) error {
				ref := officers[manager]
				if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_hr_intake','authorized',true),set_config('app.workforce_hr_intake_actor',?,true),set_config('app.workforce_hr_intake_link',?,true),set_config('app.workforce_enrollment','authorized',true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_employment_review','manage',true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_employment_person_review',?,true),set_config('app.workforce_employment_evidence','read',true)`, manager, hospital, ref.StaffID, ref.LinkPath[strings.LastIndex(ref.LinkPath, "/")+1:], staff, id, approved["id"]).Error; err != nil {
					return err
				}
				return action(tx)
			})
		}
		counts := func(id string) map[string]int64 {
			t.Helper()
			result := map[string]int64{}
			require.NoError(t, withSubmission(id, func(tx *gorm.DB) error {
				for _, table := range []struct{ name, column string }{{"workforce_employment_reviews", "id"}, {"workforce_employment_review_history", "id"}, {"workforce_employment_review_evidence", "review_id"}, {"workforce_employment_review_contributors", "review_id"}} {
					var n int64
					if err := tx.Table(table.name).Where("tenant_id=? AND hospital_id=? AND "+table.column+"=?", testutil.TenantA, hospital, id).Count(&n).Error; err != nil {
						return err
					}
					result[table.name] = n
				}
				var n int64
				if err := tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce.employment_review.submitted'", testutil.TenantA, hospital, id).Count(&n).Error; err != nil {
					return err
				}
				result["audit"] = n
				if err := tx.Table("outbox_events").Where("tenant_id=? AND subject=? AND payload->'data'->>'review_id'=?", testutil.TenantA, workforcecontract.SubjectEmploymentReviewChanged, id).Count(&n).Error; err != nil {
					return err
				}
				result["event"] = n
				return nil
			}))
			return result
		}
		positive := counts(submissionID)
		for _, key := range []string{"workforce_employment_reviews", "workforce_employment_review_history", "workforce_employment_review_evidence", "audit", "event"} {
			require.EqualValues(t, 1, positive[key], key)
		}
		require.NoError(t, withSubmission(submissionID, func(tx *gorm.DB) error {
			var subjects []string
			if err := tx.Table("workforce_employment_review_contributors").Where("review_id=?", submissionID).Order("subject").Pluck("subject", &subjects).Error; err != nil {
				return err
			}
			require.Equal(t, []string{maker, manager}, subjects, "freeze original Staff/identity preparer and employment preparer without conflating identity reviewer with a preparer")
			var findings int64
			if err := tx.Table("workforce_person_reviews").Count(&findings).Error; err != nil {
				return err
			}
			require.EqualValues(t, 1, findings, "employment purpose sees only the selected person finding")
			return nil
		}))
		require.ErrorContains(t, withSubmission(submissionID, func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE workforce_employment_reviews SET reason='Synthetic forbidden rewrite' WHERE id=?`, submissionID).Error
		}), "immutable")
		require.ErrorContains(t, withSubmission(submissionID, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO workforce_employment_review_contributors(tenant_id,hospital_id,staff_id,review_id,subject) VALUES(?,?,?,?,?)`, testutil.TenantA, hospital, staff, submissionID, "synthetic-late-preparer").Error
		}), "frozen")
		require.Error(t, withSubmission(submissionID, func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE workforce_person_reviews SET finding_reference='Synthetic forbidden employment-purpose finding change' WHERE id=?`, approved["id"]).Error
		}))
		require.Equal(t, positive, counts(submissionID))

		adminStaff := uuid.NewString()
		call(t, "PUT", base+"/staff/"+adminStaff, maker, map[string]any{"employee_code": "SYN-EMP-ADMIN", "display_name": "Synthetic reviewed Staff Admin", "job_category": "other", "employment_status": "active"}, 200)
		adminLink := base + "/staff/" + adminStaff + "/login-links/" + uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", adminLink, maker, map[string]any{"subject": maker, "reason": "Synthetic reviewed admin identity"}, 200)
		call(t, "POST", adminLink+"/decision", checker, map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic Staff Admin identity"}, 200)
		adminProposalID := uuid.Must(uuid.NewV7()).String()
		adminProposal := "/v1/iam/assignment-proposals/" + adminProposalID
		call(t, "PUT", adminProposal, maker, map[string]any{"hospital_id": hospital, "subject": maker, "template_key": "workforce.hr_intake.v1", "reason": "Synthetic scoped Staff Admin preparation", "valid_from": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, 200)
		require.Eventually(t, func() bool { return call(t, "GET", adminProposal, maker, nil, 200)["status"] == "pending_review" }, 10*time.Second, 20*time.Millisecond)
		call(t, "POST", adminProposal+"/decision", checker, map[string]any{"expected_version": 2, "decision": "approve", "reason": "Independent synthetic scoped admin authority"}, 200)
		for _, permission := range []authz.Permission{workforce.PermEnrollmentManage, workforce.PermDocumentManage} {
			require.Eventually(t, func() bool {
				grant, err := authority.AuthorizeScope(ctx, authz.ScopeRequest{TenantID: testutil.TenantA, Subject: maker, HospitalID: hospital, Permission: permission})
				return err == nil && grant.AssignmentID == adminProposalID
			}, 10*time.Second, 20*time.Millisecond)
		}
		require.Equal(t, submitted, call(t, "GET", submissionPath+"?hr_intake=v1", maker, nil, 200)["review"], "an admin with a separately reviewed scoped grant must use it instead of a legacy fallback")

		historicalPreparerID := uuid.Must(uuid.NewV7()).String()
		historicalPreparerProposal := "/v1/iam/assignment-proposals/" + historicalPreparerID
		call(t, "PUT", historicalPreparerProposal, maker, map[string]any{"hospital_id": hospital, "subject": reviewer, "template_key": "workforce.hr_intake.v1", "reason": "Synthetic historical terms preparation", "valid_from": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, 200)
		require.Eventually(t, func() bool {
			return call(t, "GET", historicalPreparerProposal, maker, nil, 200)["status"] == "pending_review"
		}, 10*time.Second, 20*time.Millisecond)
		call(t, "POST", historicalPreparerProposal+"/decision", checker, map[string]any{"expected_version": 2, "decision": "approve", "reason": "Independent synthetic temporary HR authority"}, 200)
		require.Eventually(t, func() bool {
			grant, err := authority.AuthorizeScope(ctx, authz.ScopeRequest{TenantID: testutil.TenantA, Subject: reviewer, HospitalID: hospital, Permission: workforce.PermEnrollmentManage})
			return err == nil && grant.AssignmentID == historicalPreparerID
		}, 10*time.Second, 20*time.Millisecond)
		historicalPeriodID := uuid.Must(uuid.NewV7()).String()
		historicalPeriodPath := staffBase + "/employment-periods/" + historicalPeriodID
		historicalTermsInput := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "staff_version": currentSource["staff_version"], "employment_epoch": currentSource["employment_epoch"], "engagement_kind": "employee", "duration": "ongoing", "starts_at": terms["starts_at"], "contracting_party": "", "sponsor_id": "", "reason": "Synthetic first preparer terms", "evidence_reference": "SYN-HISTORICAL-TERMS"}
		firstTerms := call(t, "PUT", historicalPeriodPath+"?hr_intake=v1", reviewer, historicalTermsInput, 200)
		oldAuthority := call(t, "GET", historicalPreparerProposal, maker, nil, 200)
		call(t, "POST", historicalPreparerProposal+"/decision", maker, map[string]any{"expected_version": oldAuthority["version"], "decision": "withdraw", "reason": "Synthetic historical preparer role ended"}, 200)
		require.Eventually(t, func() bool {
			grant, err := authority.AuthorizeScope(ctx, authz.ScopeRequest{TenantID: testutil.TenantA, Subject: reviewer, HospitalID: hospital, Permission: workforce.PermIdentityChecksReview})
			return err == nil && grant.AssignmentID == strings.TrimPrefix(officers[reviewer].AssignmentPath, "/v1/iam/assignment-proposals/")
		}, 10*time.Second, 20*time.Millisecond)
		call(t, "GET", submissionPath+"?hr_intake=v1", reviewer, nil, 403)
		historicalTermsInput["command_id"] = uuid.Must(uuid.NewV7()).String()
		historicalTermsInput["expected_version"] = firstTerms["version"]
		historicalTermsInput["reason"] = "Synthetic replacement preparer terms"
		latestTerms := call(t, "PUT", historicalPeriodPath+"?hr_intake=v1", manager, historicalTermsInput, 200)
		historicalBindingID := uuid.Must(uuid.NewV7()).String()
		historicalBinding := call(t, "PUT", historicalPeriodPath+"/revisions/2/evidence/"+historicalBindingID+"?hr_intake=v1", manager, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "document_id": employmentFile, "reason": "Synthetic evidence after terms handover"}, 200)
		historicalSubmission := maps.Clone(command)
		historicalSubmission["command_id"] = uuid.Must(uuid.NewV7()).String()
		historicalSubmission["period_id"] = historicalPeriodID
		historicalSubmission["period_revision_id"] = latestTerms["revision_id"]
		historicalSubmission["period_version"] = latestTerms["version"]
		historicalSubmission["evidence"] = []map[string]any{{"id": historicalBindingID, "revision_id": historicalBinding["revision_id"], "version": historicalBinding["version"]}}
		historicalReviewID := uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", staffBase+"/employment-reviews/"+historicalReviewID+"?hr_intake=v1", manager, historicalSubmission, 200)
		require.NoError(t, withSubmission(historicalReviewID, func(tx *gorm.DB) error {
			var subjects []string
			if err := tx.Table("workforce_employment_review_contributors").Where("review_id=?", historicalReviewID).Order("subject").Pluck("subject", &subjects).Error; err != nil {
				return err
			}
			require.Equal(t, []string{maker, manager, reviewer}, subjects, "an earlier terms author remains excluded from independence after handover and role withdrawal")
			return nil
		}))

		for _, faultTable := range []string{"workforce_audit", "outbox_events"} {
			candidateID := uuid.Must(uuid.NewV7()).String()
			candidate := staffBase + "/employment-reviews/" + candidateID
			intent := maps.Clone(command)
			intent["command_id"] = uuid.Must(uuid.NewV7()).String()
			condition := fmt.Sprintf("NEW.action='workforce.employment_review.submitted' AND NEW.resource_id='%s'::uuid", candidateID)
			if faultTable == "outbox_events" {
				condition = fmt.Sprintf("NEW.subject='%s' AND NEW.payload->'data'->>'review_id'='%s'", workforcecontract.SubjectEmploymentReviewChanged, candidateID)
			}
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_employment_review_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: fmt.Sprintf(`CREATE FUNCTION test_employment_review_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF %s THEN RAISE EXCEPTION 'synthetic employment review persistence failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER test_employment_review_fault BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION test_employment_review_fault()`, condition, faultTable)}}))
			clear := func() {
				t.Helper()
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				require.NoError(t, db.Migrate(cleanup, []tenantdb.Migration{{ID: "test_employment_review_fault_clear_" + uuid.Must(uuid.NewV7()).String(), SQL: `DROP FUNCTION IF EXISTS test_employment_review_fault() CASCADE`}}))
			}
			t.Cleanup(clear)
			call(t, "PUT", candidate+"?hr_intake=v1", manager, intent, 500)
			for key, n := range counts(candidateID) {
				require.Zero(t, n, "%s must roll back on %s failure", key, faultTable)
			}
			clear()
			recovered := call(t, "PUT", candidate+"?hr_intake=v1", manager, intent, 200)
			require.Equal(t, recovered, call(t, "PUT", candidate+"?hr_intake=v1", manager, intent, 200))
			require.Equal(t, positive, counts(candidateID), "recovery creates exactly one frozen state, success audit and event")
		}
		require.Equal(t, "inactive", call(t, "GET", base+"/identity-staff/"+staff, manager, nil, 200)["employment_status"], "submission cannot activate operational employment")
		require.Empty(t, call(t, "GET", staffBase+"/login-links", maker, nil, 200)["data"], "non-digital employment submission creates no login binding")
		sponsorID := uuid.NewString()
		sponsorBody := map[string]any{"employee_code": "SYN-CONTRACT-SPONSOR", "display_name": "Synthetic legacy sponsor source", "job_category": "other", "employment_status": "active"}
		call(t, "PUT", base+"/staff/"+sponsorID, maker, sponsorBody, 200)
		contractID := uuid.Must(uuid.NewV7()).String()
		contractPath := staffBase + "/employment-periods/" + contractID
		contract := call(t, "PUT", contractPath+"?hr_intake=v1", manager, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "staff_version": currentSource["staff_version"], "employment_epoch": currentSource["employment_epoch"], "engagement_kind": "agency_worker", "duration": "fixed", "starts_at": terms["starts_at"], "ends_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339), "contracting_party": "Synthetic agency", "sponsor_id": sponsorID, "reason": "Synthetic accountable contractor preparation", "evidence_reference": "SYN-CONTRACT"}, 200)
		contractBindingID := uuid.Must(uuid.NewV7()).String()
		contractBinding := call(t, "PUT", contractPath+"/revisions/1/evidence/"+contractBindingID+"?hr_intake=v1", manager, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "document_id": employmentFile, "reason": "Synthetic agency appointment evidence"}, 200)
		contractSubmission := maps.Clone(command)
		contractSubmission["command_id"] = uuid.Must(uuid.NewV7()).String()
		contractSubmission["period_id"] = contractID
		contractSubmission["period_revision_id"] = contract["revision_id"]
		contractSubmission["period_version"] = contract["version"]
		contractSubmission["evidence"] = []map[string]any{{"id": contractBindingID, "revision_id": contractBinding["revision_id"], "version": contractBinding["version"]}}
		contractReviewPath := staffBase + "/employment-reviews/" + uuid.Must(uuid.NewV7()).String() + "?hr_intake=v1"
		contractReceipt := call(t, "PUT", contractReviewPath, manager, contractSubmission, 200)
		sponsorBody["employment_status"] = "inactive"
		call(t, "PUT", base+"/staff/"+sponsorID, maker, sponsorBody, 200)
		contractView := call(t, "GET", contractReviewPath, manager, nil, 200)
		require.Equal(t, "source_changed", contractView["current_state"], "a contractor's inactive sponsor must not retain a current preparation state")
		require.Equal(t, contractReceipt, contractView["review"], "source cutoff retains historical appointment terms")
		contractSubmission["command_id"] = uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", staffBase+"/employment-reviews/"+uuid.Must(uuid.NewV7()).String()+"?hr_intake=v1", manager, contractSubmission, 409)
		auxiliaryFindingID := uuid.Must(uuid.NewV7()).String()
		auxiliaryFindingPath := staffBase + "/person-reviews/" + auxiliaryFindingID
		auxiliaryPersonInput := maps.Clone(input)
		auxiliaryPersonInput["command_id"] = uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", auxiliaryFindingPath, manager, auxiliaryPersonInput, 200)
		auxiliaryApproved := call(t, "POST", auxiliaryFindingPath+"/decision", reviewer, map[string]any{"expected_version": 1, "command_id": uuid.Must(uuid.NewV7()).String(), "decision": "approve", "reason": "Synthetic independent employment person basis", "identity_examined": true, "independent_person_confirmed": true}, 200)
		auxiliarySubmission := maps.Clone(command)
		auxiliarySubmission["command_id"] = uuid.Must(uuid.NewV7()).String()
		auxiliarySubmission["person_review_id"] = auxiliaryApproved["id"]
		auxiliarySubmission["person_review_revision_id"] = auxiliaryApproved["revision_id"]
		auxiliarySubmission["person_review_version"] = auxiliaryApproved["version"]
		auxiliarySubmissionPath := staffBase + "/employment-reviews/" + uuid.Must(uuid.NewV7()).String() + "?hr_intake=v1"
		auxiliaryReceipt := call(t, "PUT", auxiliarySubmissionPath, manager, auxiliarySubmission, 200)
		call(t, "POST", auxiliaryFindingPath+"/revocation", reviewer, map[string]any{"expected_version": 2, "command_id": uuid.Must(uuid.NewV7()).String(), "reason": "Synthetic person finding cutoff"}, 200)
		unavailable := call(t, "GET", auxiliarySubmissionPath, manager, nil, 200)
		require.Equal(t, "person_unavailable", unavailable["current_state"])
		require.Equal(t, auxiliaryReceipt, unavailable["review"])
		auxiliarySubmission["command_id"] = uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", staffBase+"/employment-reviews/"+uuid.Must(uuid.NewV7()).String()+"?hr_intake=v1", manager, auxiliarySubmission, 409)
		catalogResponse := request("GET", "/v1/iam/staff-access-templates?catalog=employment-v1", maker, nil, "application/json")
		require.Equal(t, 200, catalogResponse.Code, catalogResponse.Body.String())
		var employmentCatalog []map[string]any
		require.NoError(t, json.Unmarshal(catalogResponse.Body.Bytes(), &employmentCatalog))
		require.Len(t, employmentCatalog, 5)
		employmentRoles := map[string]string{}
		for _, target := range []string{manager, reviewer} {
			roleID := uuid.Must(uuid.NewV7()).String()
			rolePath := "/v1/iam/assignment-proposals/" + roleID
			call(t, "PUT", rolePath, maker, map[string]any{"hospital_id": hospital, "subject": target, "template_key": "workforce.employment_reviewer.v1", "reason": "Synthetic independent employment terms reviewer", "valid_from": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, 200)
			require.Eventually(t, func() bool { return call(t, "GET", rolePath, maker, nil, 200)["status"] == "pending_review" }, 10*time.Second, 20*time.Millisecond)
			call(t, "POST", rolePath+"/decision", checker, map[string]any{"expected_version": 2, "decision": "approve", "reason": "Independent synthetic reviewer authority"}, 200)
			require.Eventually(t, func() bool {
				grant, err := authority.AuthorizeScope(ctx, authz.ScopeRequest{TenantID: testutil.TenantA, Subject: target, HospitalID: hospital, Permission: authz.Permission("workforce.employment.review")})
				return err == nil && grant.AssignmentID == roleID
			}, 10*time.Second, 20*time.Millisecond)
			employmentRoles[target] = rolePath
		}
		t.Run("employment workspaces discover only current reviewed hospital capabilities", func(t *testing.T) {
			for _, expected := range []struct {
				subject        string
				submit, review bool
			}{{reviewer, false, true}, {manager, true, true}, {maker, true, false}} {
				var found []any
				path := "/v1/workforce/employment-review-scopes?limit=1"
				for pages := 0; path != ""; pages++ {
					require.Less(t, pages, 10)
					page := call(t, "GET", path, expected.subject, nil, 200)
					found = append(found, page["data"].([]any)...)
					path = ""
					info := page["page"].(map[string]any)
					if info["has_more"] == true {
						path = "/v1/workforce/employment-review-scopes?limit=1&cursor=" + url.QueryEscape(info["next_cursor"].(string))
					}
				}
				require.Len(t, found, 1)
				row := found[0].(map[string]any)
				require.Equal(t, hospital, row["hospital_id"])
				require.Equal(t, expected.submit, row["can_submit"])
				require.Equal(t, expected.review, row["can_review"])
			}
		})

		t.Run("employment inbox is bounded private and scoped to the original workspace", func(t *testing.T) {
			page := call(t, "GET", base+"/employment-review-inbox?employment_review=v1&limit=1", reviewer, nil, 200)
			rows := page["data"].([]any)
			require.Len(t, rows, 1)
			row := rows[0].(map[string]any)
			require.Equal(t, hospital, row["hospital_id"])
			require.Equal(t, staff, row["staff_id"])
			require.Equal(t, "pending", row["status"])
			require.Len(t, row, 8, "summary excludes terms, evidence, identity hashes and human reasons")
			call(t, "GET", base+"/employment-review-inbox?employment_review=v1", maker, nil, 403)
			call(t, "GET", base+"/employment-review-inbox?hr_intake=v1", manager, nil, 200)
			call(t, "GET", base+"/employment-review-inbox?hr_intake=v1&employment_review=v1", reviewer, nil, 400)
			call(t, "GET", base+"/employment-review-inbox?employment_review=v1&status=active", reviewer, nil, 400)
		})

		t.Run("HR preparation selects current approved person findings without identity review access", func(t *testing.T) {
			selectionPath := staffBase + "/employment-person-findings?hr_intake=v1&limit=20&staff_version=" + fmt.Sprint(currentSource["staff_version"]) + "&employment_epoch=" + fmt.Sprint(currentSource["employment_epoch"])
			page := call(t, "GET", selectionPath, manager, nil, 200)
			var ids []any
			for _, item := range page["data"].([]any) {
				row := item.(map[string]any)
				ids = append(ids, row["id"])
				require.Equal(t, hospital, row["hospital_id"])
				require.Equal(t, staff, row["staff_id"])
				require.NotContains(t, row, "evidence")
				require.NotContains(t, row, "actor_subject")
			}
			require.Contains(t, ids, approved["id"])
			call(t, "GET", selectionPath, reviewer, nil, 403)
			stale := strings.Replace(selectionPath, "staff_version="+fmt.Sprint(currentSource["staff_version"]), "staff_version=999", 1)
			call(t, "GET", stale, manager, nil, 409)
		})

		t.Run("employment discovery refuses missing workspace and audit failure without private data", func(t *testing.T) {
			paths := []string{"/v1/workforce/employment-review-scopes", base + "/employment-review-inbox?employment_review=v1", staffBase + "/employment-person-findings?hr_intake=v1&staff_version=" + fmt.Sprint(currentSource["staff_version"]) + "&employment_epoch=" + fmt.Sprint(currentSource["employment_epoch"])}
			for index, path := range paths {
				subject := reviewer
				if index == 2 {
					subject = manager
				}
				for _, missing := range []string{"X-Helivanta-Expected-Tenant", "X-Helivanta-Expected-Subject"} {
					r := httptest.NewRequest("GET", path, nil)
					r.Header.Set("Authorization", "Bearer "+tokens[subject])
					r.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
					r.Header.Set("X-Helivanta-Expected-Subject", subject)
					r.Header.Del(missing)
					w := httptest.NewRecorder()
					gateway.ServeHTTP(w, r)
					require.Equal(t, 409, w.Code, w.Body.String())
				}
			}
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_employment_discovery_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: `CREATE FUNCTION test_employment_discovery_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action IN ('workforce.employment_review.inbox.read','workforce.employment_review.person_selection.read','workforce.employment_review_scope.read') THEN RAISE EXCEPTION 'synthetic discovery audit failure';END IF;RETURN NEW;END $$;CREATE TRIGGER test_employment_discovery_fault BEFORE INSERT ON workforce_audit FOR EACH ROW EXECUTE FUNCTION test_employment_discovery_fault()`}}))
			clear := func() {
				t.Helper()
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				require.NoError(t, db.Migrate(cleanup, []tenantdb.Migration{{ID: "test_employment_discovery_clear_" + uuid.Must(uuid.NewV7()).String(), SQL: `DROP FUNCTION IF EXISTS test_employment_discovery_fault() CASCADE`}}))
			}
			t.Cleanup(clear)
			for index, path := range paths {
				subject := reviewer
				if index == 2 {
					subject = manager
				}
				response := request("GET", path, subject, nil, "application/json")
				require.Equal(t, 500, response.Code, response.Body.String())
				require.NotContains(t, response.Body.String(), "identity_hash")
				require.NotContains(t, response.Body.String(), "employee_code")
			}
			clear()
		})
		t.Run("employment inbox read context cannot cross tenant or mutate frozen reviews", func(t *testing.T) {
			for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
				require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
					if err := tx.Exec(`SELECT set_config('app.workforce_hospital',?,true),set_config('app.workforce_subject',?,true),set_config('app.workforce_employment_browse','inbox',true),set_config('app.workforce_employment_browse_actor',?,true),set_config('app.workforce_employment_browse_link',?,true)`, hospital, reviewer, officers[reviewer].StaffID, strings.Split(officers[reviewer].LinkPath, "/")[len(strings.Split(officers[reviewer].LinkPath, "/"))-1]).Error; err != nil {
						return err
					}
					var count int64
					if err := tx.Table("workforce_employment_reviews").Where("tenant_id=? AND hospital_id=?", testutil.TenantA, hospital).Count(&count).Error; err != nil {
						return err
					}
					if tenant == testutil.TenantB {
						require.Zero(t, count)
					} else {
						require.Positive(t, count)
					}
					return nil
				}))
			}
			require.Error(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.workforce_hospital',?,true),set_config('app.workforce_subject',?,true),set_config('app.workforce_employment_browse','inbox',true),set_config('app.workforce_employment_browse_actor',?,true),set_config('app.workforce_employment_browse_link','synthetic-link',true)`, hospital, reviewer, officers[reviewer].StaffID).Error; err != nil {
					return err
				}
				return tx.Exec(`UPDATE workforce_employment_reviews SET reason='synthetic forbidden browse rewrite' WHERE id=?`, submissionID).Error
			}))
		})

		sponsorBody["employment_status"] = "active"
		call(t, "PUT", base+"/staff/"+sponsorID, maker, sponsorBody, 200)
		contractReviewerPath := strings.Replace(contractReviewPath, "?hr_intake=v1", "?employment_review=v1", 1)
		require.Equal(t, "pending", call(t, "GET", contractReviewerPath, reviewer, nil, 200)["current_state"], "reviewer must validate the selected contractor sponsor without general Staff access")
		sponsorBody["employment_status"] = "inactive"
		call(t, "PUT", base+"/staff/"+sponsorID, maker, sponsorBody, 200)
		require.Equal(t, "source_changed", call(t, "GET", contractReviewerPath, reviewer, nil, 200)["current_state"])
		approvalPeriodID := uuid.Must(uuid.NewV7()).String()
		approvalPeriodPath := staffBase + "/employment-periods/" + approvalPeriodID
		approvalTermsInput := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "staff_version": currentSource["staff_version"], "employment_epoch": currentSource["employment_epoch"], "engagement_kind": "employee", "duration": "ongoing", "starts_at": terms["starts_at"], "contracting_party": "", "sponsor_id": "", "reason": "Synthetic independent terms review", "evidence_reference": "SYN-REVIEWED-TERMS"}
		approvalTerms := call(t, "PUT", approvalPeriodPath+"?hr_intake=v1", manager, approvalTermsInput, 200)
		approvalBindingID := uuid.Must(uuid.NewV7()).String()
		approvalBinding := call(t, "PUT", approvalPeriodPath+"/revisions/1/evidence/"+approvalBindingID+"?hr_intake=v1", manager, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "document_id": employmentFile, "reason": "Synthetic reviewed terms evidence"}, 200)
		approvalInput := maps.Clone(command)
		approvalInput["command_id"] = uuid.Must(uuid.NewV7()).String()
		approvalInput["period_id"] = approvalPeriodID
		approvalInput["period_revision_id"] = approvalTerms["revision_id"]
		approvalInput["period_version"] = approvalTerms["version"]
		approvalInput["evidence"] = []map[string]any{{"id": approvalBindingID, "revision_id": approvalBinding["revision_id"], "version": approvalBinding["version"]}}
		approvalID := uuid.Must(uuid.NewV7()).String()
		approvalPath := staffBase + "/employment-reviews/" + approvalID
		approvalPending := call(t, "PUT", approvalPath+"?hr_intake=v1", manager, approvalInput, 200)
		withEmploymentReviewer := func(review string, action func(*gorm.DB) error) error {
			return db.WithTenantCommand(tenantdb.WithActor(ctx, reviewer), testutil.TenantA, func(tx *gorm.DB) error {
				ref := officers[reviewer]
				if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_employment_review','review',true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_employment_review_actor',?,true),set_config('app.workforce_employment_review_link',?,true),set_config('app.workforce_employment_person_review',?,true)`, reviewer, hospital, staff, review, ref.StaffID, ref.LinkPath[strings.LastIndex(ref.LinkPath, "/")+1:], approved["id"]).Error; err != nil {
					return err
				}
				return action(tx)
			})
		}
		require.NoError(t, withEmploymentReviewer(approvalID, func(tx *gorm.DB) error {
			for _, table := range []string{"workforce_employment_reviews", "workforce_employment_period_drafts", "workforce_employment_period_history", "workforce_employment_evidence_bindings", "workforce_employment_evidence_history", "workforce_person_reviews"} {
				var visible int64
				if err := tx.Table(table).Count(&visible).Error; err != nil {
					return err
				}
				require.EqualValues(t, 1, visible, "reviewer sees only selected source: "+table)
			}
			var enrollment bool
			if err := tx.Raw(`SELECT workforce_enrollment_allowed(?::uuid,?::uuid)`, testutil.TenantA, hospital).Scan(&enrollment).Error; err != nil {
				return err
			}
			require.False(t, enrollment, "review authority grants no general enrollment access")
			return nil
		}))
		require.Error(t, withEmploymentReviewer(approvalID, func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE workforce_employment_period_drafts SET reason='Synthetic forbidden reviewer edit' WHERE id=?`, approvalPeriodID).Error
		}))
		call(t, "GET", approvalPath+"?employment_review=v1", reviewer, nil, 200)
		employmentDecision := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "decision": "approve", "reason": "Synthetic independent appointment terms finding", "terms_examined": true, "independent_person_confirmed": true}
		call(t, "POST", approvalPath+"/decision?employment_review=v1", manager, employmentDecision, 403)
		call(t, "POST", staffBase+"/employment-reviews/"+historicalReviewID+"/decision?employment_review=v1", reviewer, employmentDecision, 403)
		beforeDecision := counts(approvalID)
		for _, faultTable := range []string{"workforce_audit", "outbox_events"} {
			condition := fmt.Sprintf("NEW.action='workforce.employment_review.decided' AND NEW.resource_id='%s'::uuid", approvalID)
			if faultTable == "outbox_events" {
				condition = fmt.Sprintf("NEW.subject='%s' AND NEW.payload->'data'->>'review_id'='%s' AND NEW.payload->'data'->>'version'='2'", workforcecontract.SubjectEmploymentReviewChanged, approvalID)
			}
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_employment_decision_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: fmt.Sprintf(`CREATE FUNCTION test_employment_decision_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF %s THEN RAISE EXCEPTION 'synthetic employment decision persistence failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER test_employment_decision_fault BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION test_employment_decision_fault()`, condition, faultTable)}}))
			clear := func() {
				t.Helper()
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				require.NoError(t, db.Migrate(cleanup, []tenantdb.Migration{{ID: "test_employment_decision_fault_clear_" + uuid.Must(uuid.NewV7()).String(), SQL: `DROP FUNCTION IF EXISTS test_employment_decision_fault() CASCADE`}}))
			}
			t.Cleanup(clear)
			call(t, "POST", approvalPath+"/decision?employment_review=v1", reviewer, employmentDecision, 500)
			require.Equal(t, beforeDecision, counts(approvalID), "head/history/evidence and outbox roll back together on "+faultTable+" failure")
			require.Equal(t, approvalPending, call(t, "GET", approvalPath+"?employment_review=v1", reviewer, nil, 200)["review"])
			clear()
		}
		employmentApproved := call(t, "POST", approvalPath+"/decision?employment_review=v1", reviewer, employmentDecision, 200)
		require.Equal(t, "approved", employmentApproved["status"])
		require.Equal(t, float64(2), employmentApproved["version"])
		t.Run("employment history preserves original receipts under current scoped authority", func(t *testing.T) {
			historyPath := approvalPath + "/history?employment_review=v1&limit=1"
			history := call(t, "GET", historyPath, reviewer, nil, 200)
			require.Equal(t, []any{employmentApproved}, history["data"])
			page := history["page"].(map[string]any)
			require.Equal(t, true, page["has_more"])
			older := call(t, "GET", historyPath+"&cursor="+page["next_cursor"].(string), reviewer, nil, 200)
			require.Equal(t, []any{approvalPending}, older["data"])
			require.Equal(t, map[string]any{"has_more": false, "next_cursor": nil}, older["page"])
			require.Equal(t, []any{employmentApproved, approvalPending}, call(t, "GET", approvalPath+"/history?hr_intake=v1", manager, nil, 200)["data"])
			call(t, "GET", historyPath, maker, nil, 403)
			call(t, "GET", approvalPath+"/history?employment_review=v1&hr_intake=v1", reviewer, nil, 400)
			call(t, "GET", approvalPath+"/history?employment_review=v1&limit=201", reviewer, nil, 400)
			call(t, "GET", approvalPath+"/history?employment_review=v1&limit=1&limit=2", reviewer, nil, 400)
			call(t, "GET", approvalPath+"/history?employment_review=v1&unexpected=1", reviewer, nil, 400)
			call(t, "GET", approvalPath+"?employment_review=v1&limit=1", reviewer, nil, 400)
			foreignStaff := strings.Replace(approvalPath, "/staff/"+staff+"/", "/staff/"+uuid.NewString()+"/", 1)
			call(t, "GET", foreignStaff+"/history?employment_review=v1", reviewer, nil, 404)
			call(t, "GET", staffBase+"/employment-reviews/"+uuid.Must(uuid.NewV7()).String()+"/history?employment_review=v1", reviewer, nil, 404)
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_employment_history_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: `CREATE FUNCTION test_employment_history_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='workforce.employment_review.history.read' THEN RAISE EXCEPTION 'synthetic private history audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_employment_history_fault BEFORE INSERT ON workforce_audit FOR EACH ROW EXECUTE FUNCTION test_employment_history_fault()`}}))
			clear := func() {
				t.Helper()
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				require.NoError(t, db.Migrate(cleanup, []tenantdb.Migration{{ID: "test_employment_history_clear_" + uuid.Must(uuid.NewV7()).String(), SQL: `DROP FUNCTION IF EXISTS test_employment_history_fault() CASCADE`}}))
			}
			t.Cleanup(clear)
			failed := call(t, "GET", historyPath, reviewer, nil, 500)
			require.NotContains(t, failed, "data", "no private history is released without its audit")
			clear()
			require.Equal(t, history, call(t, "GET", historyPath, reviewer, nil, 200))
		})
		evidenceDownloads := func(t *testing.T, id string) int64 {
			t.Helper()
			var n int64
			require.NoError(t, withSubmission(id, func(tx *gorm.DB) error {
				return tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce.employment_review.evidence.downloaded'", testutil.TenantA, hospital, id).Count(&n).Error
			}))
			return n
		}
		t.Run("selected employment evidence uses protected current authority and verified bytes", func(t *testing.T) {
			contentPath := approvalPath + "/evidence/" + employmentFile + "/content"
			for subject, mode := range map[string]string{reviewer: "employment_review", manager: "hr_intake"} {
				response := request("GET", contentPath+"?"+mode+"=v1", subject, nil, "application/json")
				require.Equal(t, 200, response.Code, response.Body.String())
				require.Equal(t, imageBytes.Bytes(), response.Body.Bytes())
				require.Contains(t, response.Header().Get("Cache-Control"), "no-store")
				require.NotContains(t, response.Header().Get("Cache-Control"), "public")
				require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
				require.Contains(t, response.Header().Get("Content-Disposition"), "synthetic-appointment.png")
			}
			call(t, "GET", contentPath+"?employment_review=v1", maker, nil, 403)
			call(t, "GET", contentPath+"?employment_review=v1&hr_intake=v1", reviewer, nil, 400)
			call(t, "GET", contentPath+"?employment_review=v1&limit=1", reviewer, nil, 400)
			call(t, "GET", approvalPath+"/evidence/"+file+"/content?employment_review=v1", reviewer, nil, 404)
			cloud.DenyReads(true)
			defer cloud.DenyReads(false)
			call(t, "GET", contentPath+"?employment_review=v1", reviewer, nil, 503)
			cloud.DenyReads(false)
			corrupted := bytes.Clone(imageBytes.Bytes())
			corrupted[len(corrupted)-1] ^= 1
			cloud.CorruptReleased(corrupted)
			defer cloud.CorruptReleased(imageBytes.Bytes())
			call(t, "GET", contentPath+"?employment_review=v1", reviewer, nil, 503)
			cloud.CorruptReleased(imageBytes.Bytes())
		})
		t.Run("employment transfers have bounded admission without retaining owner locks over storage", func(t *testing.T) {
			paused, resume := make(chan struct{}, 1), make(chan struct{})
			release := sync.OnceFunc(func() { close(resume) })
			defer release()
			defer cloud.AfterReleasedRead(nil)
			responses := make(chan *httptest.ResponseRecorder, 4)
			contentPath := approvalPath + "/evidence/" + employmentFile + "/content?employment_review=v1"
			for range 4 {
				cloud.AfterReleasedRead(func() { paused <- struct{}{}; <-resume })
				go func() { responses <- request("GET", contentPath, reviewer, nil, "application/json") }()
				select {
				case <-paused:
				case response := <-responses:
					t.Fatalf("transfer ended before the storage barrier: %d", response.Code)
				case <-time.After(5 * time.Second):
					t.Fatal("transfer did not release the owner transaction before storage")
				}
			}
			call(t, "GET", contentPath, reviewer, nil, 503)
			release()
			for range 4 {
				select {
				case response := <-responses:
					require.Equal(t, 200, response.Code, response.Body.String())
					require.Equal(t, imageBytes.Bytes(), response.Body.Bytes())
				case <-time.After(5 * time.Second):
					t.Fatal("admitted transfer did not finish after storage resumed")
				}
			}
			response := request("GET", contentPath, reviewer, nil, "application/json")
			require.Equal(t, 200, response.Code, "completed transfers release admission capacity")
		})
		t.Run("employment completion audit failure withholds bytes and its receipt", func(t *testing.T) {
			before := evidenceDownloads(t, approvalID)
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_employment_byte_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: `CREATE FUNCTION test_employment_byte_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='workforce.employment_review.evidence.downloaded' THEN RAISE EXCEPTION 'synthetic download audit failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER test_employment_byte_fault BEFORE INSERT ON workforce_audit FOR EACH ROW EXECUTE FUNCTION test_employment_byte_fault()`}}))
			clear := func() {
				t.Helper()
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				require.NoError(t, db.Migrate(cleanup, []tenantdb.Migration{{ID: "test_employment_byte_clear_" + uuid.Must(uuid.NewV7()).String(), SQL: `DROP FUNCTION IF EXISTS test_employment_byte_fault() CASCADE`}}))
			}
			t.Cleanup(clear)
			call(t, "GET", approvalPath+"/evidence/"+employmentFile+"/content?employment_review=v1", reviewer, nil, 500)
			require.Equal(t, before, evidenceDownloads(t, approvalID))
			clear()
		})
		t.Run("replacement actor link cannot authorize an in-flight employment disclosure", func(t *testing.T) {
			before := evidenceDownloads(t, approvalID)
			contentPath := approvalPath + "/evidence/" + employmentFile + "/content?employment_review=v1"
			response := pausedDownload(t, contentPath, func() {
				ref := officers[reviewer]
				call(t, "POST", ref.LinkPath+"/decision", maker, map[string]any{"expected_version": ref.LinkVersion, "decision": "revoke", "reason": "Synthetic employment disclosure actor cutoff"}, 200)
				link := base + "/staff/" + ref.StaffID + "/login-links/" + uuid.Must(uuid.NewV7()).String()
				call(t, "PUT", link, maker, map[string]any{"subject": reviewer, "reason": "Synthetic independent actor identity recovery"}, 200)
				linked := call(t, "POST", link+"/decision", checker, map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic actor recovery"}, 200)
				ref.LinkPath, ref.LinkVersion = link, linked["version"].(float64)
				officers[reviewer] = ref
			})
			require.Equal(t, 403, response.Code, response.Body.String())
			require.NotEqual(t, imageBytes.Bytes(), response.Body.Bytes())
			require.Equal(t, before, evidenceDownloads(t, approvalID))
			fresh := request("GET", contentPath, reviewer, nil, "application/json")
			require.Equal(t, 200, fresh.Code, fresh.Body.String())
			require.Equal(t, imageBytes.Bytes(), fresh.Body.Bytes())
		})
		t.Run("ending a review during storage prevents employment byte release", func(t *testing.T) {
			id := uuid.Must(uuid.NewV7()).String()
			racePath := staffBase + "/employment-reviews/" + id
			intent := maps.Clone(command)
			intent["command_id"] = uuid.Must(uuid.NewV7()).String()
			call(t, "PUT", racePath+"?hr_intake=v1", manager, intent, 200)
			response := pausedDownload(t, racePath+"/evidence/"+employmentFile+"/content?employment_review=v1", func() {
				call(t, "POST", racePath+"/withdrawal?hr_intake=v1", manager, map[string]any{"expected_version": 1, "command_id": uuid.Must(uuid.NewV7()).String(), "reason": "Synthetic review ended during storage"}, 200)
			})
			require.Equal(t, 409, response.Code, response.Body.String())
			require.NotEqual(t, imageBytes.Bytes(), response.Body.Bytes())
			require.Zero(t, evidenceDownloads(t, id))
		})
		require.Equal(t, employmentApproved, call(t, "POST", approvalPath+"/decision?employment_review=v1", reviewer, employmentDecision, 200), "independent decision retry preserves its immutable receipt")
		require.Equal(t, approvalPending, call(t, "PUT", approvalPath+"?hr_intake=v1", manager, approvalInput, 200), "submission replay remains the original pending receipt after a decision")
		changedDecision := maps.Clone(employmentDecision)
		changedDecision["reason"] = "Synthetic different intent under an existing command"
		call(t, "POST", approvalPath+"/decision?employment_review=v1", reviewer, changedDecision, 409)
		changedDecision["command_id"] = uuid.Must(uuid.NewV7()).String()
		call(t, "POST", approvalPath+"/decision?employment_review=v1", reviewer, changedDecision, 409)
		changedDecision["terms_examined"] = false
		call(t, "POST", approvalPath+"/decision?employment_review=v1", reviewer, changedDecision, 400)
		call(t, "GET", approvalPath+"?employment_review=v1&hr_intake=v1", reviewer, nil, 400)
		call(t, "GET", approvalPath+"?employment_review=v1&employment_review=v1", reviewer, nil, 400)
		call(t, "GET", approvalPath+"?employment_review=v1", maker, nil, 403)
		call(t, "GET", staffBase+"/employment-reviews/"+uuid.Must(uuid.NewV7()).String()+"?employment_review=v1", reviewer, nil, 404)
		freshSubmission := func() (string, map[string]any) {
			t.Helper()
			fresh := maps.Clone(command)
			fresh["command_id"] = uuid.Must(uuid.NewV7()).String()
			freshPath := staffBase + "/employment-reviews/" + uuid.Must(uuid.NewV7()).String()
			return freshPath, call(t, "PUT", freshPath+"?hr_intake=v1", manager, fresh, 200)
		}
		withdrawPath, withdrawnPending := freshSubmission()
		withdrawal := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "reason": "Synthetic preparer withdrawal"}
		call(t, "POST", withdrawPath+"/withdrawal?hr_intake=v1", reviewer, withdrawal, 403)
		withdrawn := call(t, "POST", withdrawPath+"/withdrawal?hr_intake=v1", manager, withdrawal, 200)
		require.Equal(t, "withdrawn", withdrawn["status"])
		require.Equal(t, float64(2), withdrawn["version"])
		require.Equal(t, withdrawnPending["terms"], withdrawn["terms"])
		require.Equal(t, false, withdrawn["reviewer_terms_examined"])
		require.Equal(t, false, withdrawn["independent_person_confirmed"])
		require.Equal(t, withdrawn, call(t, "POST", withdrawPath+"/withdrawal?hr_intake=v1", manager, withdrawal, 200))
		call(t, "POST", withdrawPath+"/decision?employment_review=v1", reviewer, employmentDecision, 409)
		closedView := call(t, "GET", withdrawPath+"?employment_review=v1", reviewer, nil, 200)
		require.Equal(t, "withdrawn", closedView["current_state"])
		for _, capability := range []string{"can_approve", "can_reject", "can_withdraw", "can_revoke"} {
			require.Equal(t, false, closedView[capability], capability)
		}
		rejectPath, _ := freshSubmission()
		rejection := maps.Clone(employmentDecision)
		rejection["command_id"] = uuid.Must(uuid.NewV7()).String()
		rejection["decision"] = "reject"
		rejected := call(t, "POST", rejectPath+"/decision?employment_review=v1", reviewer, rejection, 200)
		require.Equal(t, "rejected", rejected["status"])
		require.Equal(t, rejected, call(t, "POST", rejectPath+"/decision?employment_review=v1", reviewer, rejection, 200))
		require.Equal(t, "rejected", call(t, "GET", rejectPath+"?employment_review=v1", reviewer, nil, 200)["current_state"])
		approvalTermsInput["command_id"] = uuid.Must(uuid.NewV7()).String()
		approvalTermsInput["expected_version"] = approvalTerms["version"]
		approvalTermsInput["reason"] = "Synthetic later preparation draft"
		call(t, "PUT", approvalPeriodPath+"?hr_intake=v1", manager, approvalTermsInput, 200)
		approvedView := call(t, "GET", approvalPath+"?employment_review=v1", reviewer, nil, 200)
		require.Equal(t, "approved", approvedView["current_state"], "editing another preparation version must not revoke the independently approved historical terms")
		require.Equal(t, employmentApproved, approvedView["review"])
		require.Equal(t, "inactive", call(t, "GET", base+"/identity-staff/"+staff, manager, nil, 200)["employment_status"], "terms approval grants no operational employment or login")
		call(t, "PUT", employmentFilePath+"/revocation?hr_intake=v1", manager, nil, 200)
		require.Error(t, withEmploymentReviewer(submissionID, func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE workforce_employment_reviews SET status='approved',revision_id=?,version=2,actor_subject=?,decision_reason='Synthetic forbidden stale evidence approval',reviewer_terms_examined=true,independent_person_confirmed=true,input_hash=? WHERE id=?`, uuid.Must(uuid.NewV7()), reviewer, strings.Repeat("d", 64), submissionID).Error
		}), "database approval guard must reject revoked evidence even if a caller omits application revalidation")
		unavailableApproved := call(t, "GET", approvalPath+"?employment_review=v1", reviewer, nil, 200)
		require.Equal(t, "evidence_unavailable", unavailableApproved["current_state"])
		require.Equal(t, true, unavailableApproved["can_revoke"], "unavailable source must not prevent ending an approved record")
		revocation := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 2, "reason": "Synthetic independent withdrawal of approved terms"}
		call(t, "POST", approvalPath+"/revocation?employment_review=v1", manager, revocation, 403)
		revoked := call(t, "POST", approvalPath+"/revocation?employment_review=v1", reviewer, revocation, 200)
		require.Equal(t, "revoked", revoked["status"])
		require.Equal(t, float64(3), revoked["version"])
		require.Equal(t, false, revoked["reviewer_terms_examined"], "revocation must not borrow the earlier approval's attestations")
		require.Equal(t, false, revoked["independent_person_confirmed"])
		require.Equal(t, revoked, call(t, "POST", approvalPath+"/revocation?employment_review=v1", reviewer, revocation, 200))
		require.Equal(t, employmentApproved, call(t, "POST", approvalPath+"/decision?employment_review=v1", reviewer, employmentDecision, 200), "original approval receipt remains historical after revocation")
		require.Equal(t, "revoked", call(t, "GET", approvalPath+"?employment_review=v1", reviewer, nil, 200)["current_state"])
		require.Equal(t, "evidence_unavailable", call(t, "GET", submissionPath+"?hr_intake=v1", manager, nil, 200)["current_state"])
		require.Equal(t, submitted, call(t, "PUT", submissionPath+"?hr_intake=v1", manager, command, 200), "historical receipt retry never restores revoked evidence")
		changed = maps.Clone(command)
		changed["command_id"] = uuid.Must(uuid.NewV7()).String()
		call(t, "PUT", staffBase+"/employment-reviews/"+uuid.Must(uuid.NewV7()).String()+"?hr_intake=v1", manager, changed, 409)
		call(t, "PUT", periodPath+"?hr_intake=v1", manager, map[string]any{
			"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": terms["version"], "staff_version": currentSource["staff_version"], "employment_epoch": currentSource["employment_epoch"],
			"engagement_kind": terms["engagement_kind"], "duration": terms["duration"], "starts_at": terms["starts_at"], "contracting_party": terms["contracting_party"], "sponsor_id": terms["sponsor_id"], "reason": "Synthetic amended preparation", "evidence_reference": terms["evidence_reference"],
		}, 200)
		stale := call(t, "GET", submissionPath+"?hr_intake=v1", manager, nil, 200)
		require.Equal(t, "source_changed", stale["current_state"])
		require.Equal(t, submitted, stale["review"], "a later draft must not overwrite frozen terms or evidence")
		require.Equal(t, positive, counts(submissionID), "read/retry/source changes do not mutate submitted facts or publish another success event")
		require.Equal(t, []any{revoked, employmentApproved, approvalPending}, call(t, "GET", approvalPath+"/history?employment_review=v1", reviewer, nil, 200)["data"], "ended and stale evidence preserves the original attributable receipts")
		role := call(t, "GET", employmentRoles[reviewer], maker, nil, 200)
		call(t, "POST", employmentRoles[reviewer]+"/decision", maker, map[string]any{"expected_version": role["version"], "decision": "withdraw", "reason": "Synthetic employment history authority cutoff"}, 200)
		call(t, "GET", approvalPath+"/history?employment_review=v1", reviewer, nil, 403)
		call(t, "GET", base+"/employment-review-inbox?employment_review=v1", reviewer, nil, 403)

		require.Eventually(t, func() bool {
			grant, err := authority.AuthorizeScope(ctx, authz.ScopeRequest{TenantID: testutil.TenantA, Subject: reviewer, HospitalID: hospital, Permission: workforce.PermIdentityChecksReview})
			return err == nil && grant.AssignmentID == strings.TrimPrefix(officers[reviewer].AssignmentPath, "/v1/iam/assignment-proposals/")
		}, 10*time.Second, 20*time.Millisecond)
	})
	require.Equal(t, "approved", approved["status"])
	require.Equal(t, float64(2), approved["version"])
	approvedInbox := call(t, "GET", base+"/person-review-inbox?status=approved", reviewer, nil, 200)["data"].([]any)
	require.Len(t, approvedInbox, 1)
	require.Equal(t, approved["id"], approvedInbox[0].(map[string]any)["id"])
	current := call(t, "GET", path, reviewer, nil, 200)
	require.Equal(t, approved, current["review"])
	require.Equal(t, "approved", current["current_state"])
	require.Equal(t, "inactive", call(t, "GET", staffBase, maker, nil, 200)["employment_status"], "a reviewed person finding does not activate employment")
	require.Empty(t, call(t, "GET", staffBase+"/login-links", maker, nil, 200)["data"], "non-digital workers do not receive fabricated login links")
	permissions, err := graph.Resolve(ctx, reviewer, testutil.TenantA)
	require.NoError(t, err)
	require.Empty(t, permissions, "review authority does not grant tenant-wide business permissions")
	downloadAudits := func(t *testing.T, review any) int64 {
		t.Helper()
		var count int64
		require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, maker), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true)`, maker, hospital).Error; err != nil {
				return err
			}
			return tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce.person_review.evidence.downloaded'", testutil.TenantA, hospital, review).Count(&count).Error
		}))
		return count
	}

	require.Equal(t, int64(1), downloadAudits(t, pending["id"]), "the initial private disclosure is attributable")

	t.Run("replacement assignment cannot authorize an in-flight disclosure", func(t *testing.T) {
		before := downloadAudits(t, approved["id"])
		replacementID := uuid.Must(uuid.NewV7()).String()
		replacement := "/v1/iam/assignment-proposals/" + replacementID
		call(t, "PUT", replacement, maker, map[string]any{"hospital_id": hospital, "subject": reviewer, "template_key": "workforce.identity_reviewer.v1", "reason": "Synthetic replacement reviewer assignment", "valid_from": time.Now().UTC().Format(time.RFC3339), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, 200)
		require.Eventually(t, func() bool { return call(t, "GET", replacement, maker, nil, 200)["status"] == "pending_review" }, 10*time.Second, 20*time.Millisecond)
		denied := pausedDownload(t, evidencePath, func() {
			old := call(t, "GET", officers[reviewer].AssignmentPath, maker, nil, 200)
			call(t, "POST", officers[reviewer].AssignmentPath+"/decision", maker, map[string]any{"expected_version": old["version"], "decision": "withdraw", "reason": "Synthetic withdrawal during private storage I/O"}, 200)
			call(t, "POST", replacement+"/decision", checker, map[string]any{"expected_version": 2, "decision": "approve", "reason": "Independent synthetic replacement approval"}, 200)
			require.Eventually(t, func() bool {
				current, err := authority.AuthorizeScope(ctx, authz.ScopeRequest{TenantID: testutil.TenantA, Subject: reviewer, HospitalID: hospital, Permission: workforce.PermIdentityChecksReview})
				return err == nil && current.AssignmentID == replacementID
			}, 3*time.Second, 10*time.Millisecond)
		})
		require.Equal(t, 403, denied.Code)
		require.False(t, bytes.Equal(imageBytes.Bytes(), denied.Body.Bytes()), "no private bytes after assignment replacement")
		require.Equal(t, before, downloadAudits(t, approved["id"]))
		fresh := request("GET", evidencePath, reviewer, nil, "application/json")
		require.Equal(t, 200, fresh.Code, fresh.Body.String())
		require.Equal(t, imageBytes.Bytes(), fresh.Body.Bytes())
		require.Equal(t, before+1, downloadAudits(t, approved["id"]))
		ref := officers[reviewer]
		ref.AssignmentPath = replacement
		officers[reviewer] = ref
	})
	t.Run("replacement staff link cannot authorize an in-flight disclosure", func(t *testing.T) {
		before := downloadAudits(t, approved["id"])
		ref := officers[reviewer]
		newLink := base + "/staff/" + ref.StaffID + "/login-links/" + uuid.Must(uuid.NewV7()).String()
		denied := pausedDownload(t, evidencePath, func() {
			call(t, "POST", ref.LinkPath+"/decision", maker, map[string]any{"expected_version": ref.LinkVersion, "decision": "revoke", "reason": "Synthetic reviewed-link replacement during private storage I/O"}, 200)
			require.Empty(t, call(t, "GET", "/v1/workforce/person-review-scopes", reviewer, nil, 200)["data"], "revoked actor link removes discoverable authority")
			call(t, "GET", base+"/person-review-inbox", reviewer, nil, 403)
			cloud.AfterReleasedRead(nil)
			withoutActor := request("GET", evidencePath, reviewer, nil, "application/json")
			require.Equal(t, 403, withoutActor.Code, "no private disclosure while the reviewer has no current reviewed staff link")
			call(t, "PUT", newLink, maker, map[string]any{"subject": reviewer, "reason": "Synthetic fresh actor-link proposal"}, 200)
			linked := call(t, "POST", newLink+"/decision", checker, map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic replacement identity"}, 200)
			ref.LinkPath, ref.LinkVersion = newLink, linked["version"].(float64)
		})
		require.Equal(t, 403, denied.Code)
		require.False(t, bytes.Equal(imageBytes.Bytes(), denied.Body.Bytes()), "no private bytes after actor-link replacement")
		require.Equal(t, before, downloadAudits(t, approved["id"]))
		fresh := request("GET", evidencePath, reviewer, nil, "application/json")
		require.Equal(t, 200, fresh.Code, fresh.Body.String())
		require.Equal(t, before+1, downloadAudits(t, approved["id"]))
		officers[reviewer] = ref
	})
	t.Run("storage failure and corrupted bytes never record a disclosure", func(t *testing.T) {
		before := downloadAudits(t, approved["id"])
		cloud.DenyReads(true)
		unavailable := request("GET", evidencePath, reviewer, nil, "application/json")
		cloud.DenyReads(false)
		require.Equal(t, 503, unavailable.Code)
		require.False(t, bytes.Equal(imageBytes.Bytes(), unavailable.Body.Bytes()))
		require.Equal(t, before, downloadAudits(t, approved["id"]))
		cloud.CorruptReleased([]byte("synthetic-corrupt-identity-content"))
		corrupted := request("GET", evidencePath, reviewer, nil, "application/json")
		cloud.CorruptReleased(imageBytes.Bytes())
		require.Equal(t, 503, corrupted.Code)
		require.Equal(t, before, downloadAudits(t, approved["id"]))
		fresh := request("GET", evidencePath, reviewer, nil, "application/json")
		require.Equal(t, 200, fresh.Code, fresh.Body.String())
		require.Equal(t, imageBytes.Bytes(), fresh.Body.Bytes())
		require.Equal(t, before+1, downloadAudits(t, approved["id"]))
	})
	t.Run("review revision changes and revocation invalidate an in-flight disclosure", func(t *testing.T) {
		thirdPath := staffBase + "/person-reviews/" + uuid.Must(uuid.NewV7()).String()
		thirdInput := map[string]any{}
		for key, value := range input {
			thirdInput[key] = value
		}
		thirdInput["command_id"] = uuid.Must(uuid.NewV7()).String()
		third := call(t, "PUT", thirdPath, manager, thirdInput, 200)
		contentPath := thirdPath + "/evidence/" + file + "/content"
		var accepted map[string]any
		changed := pausedDownload(t, contentPath, func() {
			accepted = call(t, "POST", thirdPath+"/decision", reviewer, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "decision": "approve", "reason": "Synthetic independent finding during private I/O", "identity_examined": true, "independent_person_confirmed": true}, 200)
		})
		require.Equal(t, "approved", accepted["status"])
		require.Equal(t, 409, changed.Code)
		require.Zero(t, downloadAudits(t, third["id"]))
		fresh := request("GET", contentPath, reviewer, nil, "application/json")
		require.Equal(t, 200, fresh.Code, fresh.Body.String())
		require.Equal(t, int64(1), downloadAudits(t, third["id"]))
		revoked := pausedDownload(t, contentPath, func() {
			call(t, "POST", thirdPath+"/revocation", reviewer, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 2, "reason": "Synthetic finding revocation during private storage I/O"}, 200)
		})
		require.Equal(t, 409, revoked.Code)
		require.Equal(t, int64(1), downloadAudits(t, third["id"]), "no success audit for the revoked in-flight disclosure")
	})
	t.Run("evidence revocation during storage blocks the original bytes", func(t *testing.T) {
		secondFile := uuid.Must(uuid.NewV7()).String()
		secondFilePath := staffBase + "/documents/" + secondFile
		call(t, "PUT", secondFilePath, maker, map[string]any{"record_kind": "enrollment", "record_id": staff, "category": "identity_evidence", "filename": "synthetic-second-identity.png", "content_type": "image/png", "size": imageBytes.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(imageBytes.Bytes()))}, 201)
		require.Equal(t, 202, request("PUT", secondFilePath+"/content", maker, imageBytes.Bytes(), "image/png").Code)
		state, err := processor.Process(ctx, workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: hospital, StaffID: staff, FileID: secondFile, ScanAttempt: 1})
		require.NoError(t, err)
		require.Equal(t, "available", state)
		casePath := staffBase + "/person-reviews/" + uuid.Must(uuid.NewV7()).String()
		caseInput := map[string]any{}
		for key, value := range input {
			caseInput[key] = value
		}
		caseInput["command_id"], caseInput["document_ids"] = uuid.Must(uuid.NewV7()).String(), []string{secondFile}
		pending := call(t, "PUT", casePath, manager, caseInput, 200)
		denied := pausedDownload(t, casePath+"/evidence/"+secondFile+"/content", func() {
			call(t, "PUT", secondFilePath+"/revocation", maker, nil, 200)
		})
		require.Equal(t, 409, denied.Code)
		require.Zero(t, downloadAudits(t, pending["id"]))
		require.Equal(t, "evidence_unavailable", call(t, "GET", casePath, reviewer, nil, 200)["current_state"])
	})

	t.Run("independent decision retry and immutable history", func(t *testing.T) {
		require.Equal(t, approved, call(t, "POST", path+"/decision", reviewer, decision, 200))
		conflicting := map[string]any{}
		for key, value := range decision {
			conflicting[key] = value
		}
		conflicting["reason"] = "Changed intent under the existing command ID"
		call(t, "POST", path+"/decision", reviewer, conflicting, 409)
		history := call(t, "GET", path+"/history?limit=1", reviewer, nil, 200)
		require.Len(t, history["data"], 1)
		require.Equal(t, approved, history["data"].([]any)[0])
		page := history["page"].(map[string]any)
		require.Equal(t, true, page["has_more"])
		older := call(t, "GET", path+"/history?limit=1&cursor="+page["next_cursor"].(string), reviewer, nil, 200)
		require.Equal(t, pending, older["data"].([]any)[0])
	})
	t.Run("missing action and wrong object deny without private data", func(t *testing.T) {
		call(t, "GET", filePath+"/content", reviewer, nil, 403)
		unknown := strings.Replace(path, staff, uuid.NewString(), 1)
		denied := call(t, "GET", unknown, reviewer, nil, 404)
		require.NotContains(t, denied, "review")
		unbound := httptest.NewRequest("GET", path, nil)
		unbound.Header.Set("Authorization", "Bearer "+tokens[reviewer])
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, unbound)
		require.Equal(t, 409, response.Code)
		require.NotContains(t, response.Body.String(), "SYN-HUMAN-IDENTITY-POLICY")
	})
	t.Run("account intent must explicitly include its subject field", func(t *testing.T) {
		for _, missing := range []bool{true, false} {
			invalid := map[string]any{}
			for key, value := range input {
				invalid[key] = value
			}
			invalid["command_id"] = uuid.Must(uuid.NewV7()).String()
			if missing {
				delete(invalid, "declared_subject")
			} else {
				invalid["declared_subject"] = nil
			}
			call(t, "PUT", staffBase+"/person-reviews/"+uuid.Must(uuid.NewV7()).String(), manager, invalid, 400)
		}
	})
	t.Run("purpose bound RLS excludes other files and cannot write", func(t *testing.T) {
		for _, category := range []string{"employment_evidence", "identity_evidence"} {
			other := staffBase + "/documents/" + uuid.Must(uuid.NewV7()).String()
			call(t, "PUT", other, maker, map[string]any{"record_kind": "enrollment", "record_id": staff, "category": category, "filename": "synthetic-unbound-evidence.png", "content_type": "image/png", "size": imageBytes.Len(), "sha256": fmt.Sprintf("%x", sha256.Sum256(imageBytes.Bytes()))}, 201)
		}
		withReview := func(tenant string, action func(*gorm.DB) error) error {
			return db.WithTenantCommand(tenantdb.WithActor(ctx, reviewer), tenant, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_person_review','review',true),set_config('app.workforce_person_review_documents',?,true)`, reviewer, hospital, staff, file).Error; err != nil {
					return err
				}
				return action(tx)
			})
		}
		require.NoError(t, withReview(testutil.TenantA, func(tx *gorm.DB) error {
			var visible []string
			if err := tx.Table("workforce_documents").Where("hospital_id=? AND staff_id=?", hospital, staff).Pluck("id", &visible).Error; err != nil {
				return err
			}
			require.Equal(t, []string{file}, visible, "review context sees only its frozen identity file")
			return nil
		}))
		for _, table := range []string{"workforce_person_reviews", "workforce_person_review_history", "workforce_person_review_evidence", "workforce_person_review_contributors", "workforce_document_contributors", "workforce_documents"} {
			require.NoError(t, withReview(testutil.TenantB, func(tx *gorm.DB) error {
				var count int64
				if err := tx.Table(table).Count(&count).Error; err != nil {
					return err
				}
				require.Zero(t, count, "foreign tenant sees no review or document provenance in %s", table)
				return nil
			}))
		}
		require.ErrorContains(t, withReview(testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE workforce_documents SET updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=?`, testutil.TenantA, hospital, file).Error
		}), "row-level security", "review purpose does not confer document editing")
		require.ErrorContains(t, withReview(testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE workforce_person_review_history SET decision_reason='synthetic forged reason' WHERE tenant_id=? AND hospital_id=? AND id=?`, testutil.TenantA, hospital, approved["id"]).Error
		}), "person review history is immutable")
	})
	t.Run("pending withdrawal is retained and cannot approve", func(t *testing.T) {
		secondPath := staffBase + "/person-reviews/" + uuid.Must(uuid.NewV7()).String()
		secondInput := map[string]any{}
		for key, value := range input {
			secondInput[key] = value
		}
		secondInput["command_id"] = uuid.Must(uuid.NewV7()).String()
		submitted := call(t, "PUT", secondPath, manager, secondInput, 200)
		lateContributor := db.WithTenantCommand(tenantdb.WithActor(ctx, manager), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_person_review','manage',true)`, manager, hospital, staff).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO workforce_person_review_contributors(tenant_id,hospital_id,staff_id,review_id,subject) VALUES(?,?,?,?,?)`, testutil.TenantA, hospital, staff, submitted["id"], "synthetic-late-contributor").Error
		})
		require.ErrorContains(t, lateContributor, "person review evidence and contributors are frozen", "submitted provenance cannot grow in another transaction")
		withdraw := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "reason": "Synthetic reasoned withdrawal before employment review"}
		withdrawn := call(t, "POST", secondPath+"/withdrawal", manager, withdraw, 200)
		require.Equal(t, "withdrawn", withdrawn["status"])
		require.Equal(t, submitted, call(t, "PUT", secondPath, manager, secondInput, 200))
		require.Equal(t, withdrawn, call(t, "POST", secondPath+"/withdrawal", manager, withdraw, 200))
		call(t, "POST", secondPath+"/decision", reviewer, decision, 409)
		require.Equal(t, "withdrawn", call(t, "GET", secondPath, reviewer, nil, 200)["current_state"])
	})
	t.Run("source changes invalidate current finding without rewriting its receipt", func(t *testing.T) {
		before := downloadAudits(t, approved["id"])
		denied := pausedDownload(t, evidencePath, func() {
			call(t, "PUT", staffBase, maker, map[string]any{"employee_code": "SYN-INACTIVE-PERSON", "display_name": "Synthetic Corrected Non-digital Joiner", "job_category": "other", "employment_status": "inactive"}, 200)
		})
		require.Equal(t, 409, denied.Code, denied.Body.String())
		require.Equal(t, before, downloadAudits(t, approved["id"]))
		changed := call(t, "GET", path, reviewer, nil, 200)
		require.Equal(t, "source_changed", changed["current_state"])
		require.Equal(t, approved, changed["review"])
	})
	t.Run("revocation remains available for a changed source", func(t *testing.T) {
		revoke := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 2, "reason": "Synthetic review revoked after identity source correction"}
		revoked := call(t, "POST", path+"/revocation", reviewer, revoke, 200)
		require.Equal(t, "revoked", revoked["status"])
		require.Equal(t, float64(3), revoked["version"])
		require.Equal(t, revoked, call(t, "POST", path+"/revocation", reviewer, revoke, 200))
		require.Equal(t, "revoked", call(t, "GET", path, reviewer, nil, 200)["current_state"])
		require.Equal(t, "inactive", call(t, "GET", staffBase, maker, nil, 200)["employment_status"])
		var reads atomic.Int64
		cloud.AfterReleasedRead(func() { reads.Add(1) })
		defer cloud.AfterReleasedRead(nil)
		require.Equal(t, 409, request("GET", evidencePath, reviewer, nil, "application/json").Code)
		require.Zero(t, reads.Load(), "revoked findings do not reach storage")
	})
}
