package journey

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	fgaclient "github.com/openfga/go-sdk/client"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/iam"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestStaffFileRechecksIAMAfterStorage(t *testing.T) {
	for _, mode := range []string{"download", "upload", "employment_documents", "employment_enrollment", "graph_outage"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			cloud := testinfra.NewGCSProtocol(t)
			files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
			require.NoError(t, err)
			upstream, err := url.Parse(testinfra.StartOpenFGA(t))
			require.NoError(t, err)
			proxy := httputil.NewSingleHostReverseProxy(upstream)
			var graphUnavailable atomic.Bool
			graphServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if graphUnavailable.Load() {
					http.Error(w, "synthetic graph outage", http.StatusServiceUnavailable)
					return
				}
				proxy.ServeHTTP(w, r)
			}))
			t.Cleanup(graphServer.Close)
			graphURL, storeName := graphServer.URL, testinfra.IsolationKey(t)
			graph, err := authz.NewClient(ctx, graphURL, storeName)
			require.NoError(t, err)
			modules := []platform.Module{iam.New(nil), workforce.New()}
			registry := platform.NewRegistry()
			for _, m := range modules {
				require.NoError(t, registry.Register(m))
			}
			require.NoError(t, platform.ReconcileTenant(ctx, registry, graph, testutil.TenantA))
			setup, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
				Tokens:  map[string]string{"manager": testutil.TenantA},
				Perms:   map[string][]authz.Permission{"manager": {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage}},
				Modules: modules, Documents: files,
			})
			for _, role := range []authz.Role{authz.RoleTenantAdmin, authz.RoleStaffMember} {
				require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
					return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-manager',?)`, testutil.TenantA, role).Error
				}))
				require.NoError(t, graph.GrantTenantRole(ctx, testutil.TenantA, role))
				require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, "user-manager", role))
			}
			data, err := json.Marshal(iamcontract.MemberChangedData{Subject: "user-manager", RoleKey: "staff_member"})
			require.NoError(t, err)
			evt := events.Event{ID: uuid.Must(uuid.NewV7()).String(), Type: "MemberGranted", Version: 1, TenantID: testutil.TenantA, Data: data}
			projected := false
			for _, consumer := range iam.New(nil).Consumers(platform.Deps{DB: db, Authz: graph}) {
				if consumer.Subject == iamcontract.SubjectMemberGranted {
					require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return consumer.Handle(ctx, tx, evt) }))
					require.NotNil(t, consumer.AfterCommit)
					require.NoError(t, consumer.AfterCommit(ctx, evt))
					projected = true
				}
			}
			require.True(t, projected)
			hospital, staffID, fileID := uuid.NewString(), uuid.NewString(), uuid.NewString()
			base := "/v1/workforce/hospitals/" + hospital
			require.Equal(t, 200, do(setup, "PUT", base, "manager", `{"name":"Synthetic current authority hospital"}`).Code)
			require.Equal(t, 200, do(setup, "PUT", base+"/access/user-manager", "manager", `{"level":"manager"}`).Code)
			staffPath := base + "/staff/" + staffID
			require.Equal(t, 200, do(setup, "PUT", staffPath, "manager", `{"employee_code":"SYN-PRIVATE-001","display_name":"Synthetic private-file worker","job_category":"nurse","employment_status":"active"}`).Code)
			var content bytes.Buffer
			require.NoError(t, png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 2, 2))))
			path := staffPath + "/documents/" + fileID
			intent := fmt.Sprintf(`{"record_kind":"enrollment","record_id":%q,"category":"employment_evidence","filename":"synthetic-private.png","content_type":"image/png","size":%d,"sha256":"%x"}`, staffID, content.Len(), sha256.Sum256(content.Bytes()))
			res := do(setup, "PUT", path, "manager", intent)
			require.Equal(t, 201, res.Code, res.Body.String())
			upload := httptest.NewRequest("PUT", path+"/content", bytes.NewReader(content.Bytes()))
			upload.Header.Set("Authorization", "Bearer manager")
			upload.Header.Set("Content-Type", "image/png")
			out := httptest.NewRecorder()
			setup.ServeHTTP(out, upload)
			require.Equal(t, 202, out.Code, out.Body.String())
			var database string
			require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
			worker, err := tenantdb.OpenRuntime(ctx, testinfra.DocumentWorkerDSN(t, database))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, worker.Close()) })
			scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
			require.NoError(t, err)
			processor, err := workforce.NewDocumentProcessor(ctx, worker, files, scanner)
			require.NoError(t, err)
			status, err := processor.Process(ctx, workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: hospital, StaffID: staffID, FileID: fileID, ScanAttempt: 1})
			require.NoError(t, err)
			require.Equal(t, "available", status)

			downloadPath, downloadAction := path+"/content", "workforce.document.downloaded"
			if strings.HasPrefix(mode, "employment_") {
				source := do(setup, "GET", staffPath+"/employment-source", "manager", "")
				require.Equal(t, 200, source.Code, source.Body.String())
				var basis map[string]any
				require.NoError(t, json.Unmarshal(source.Body.Bytes(), &basis))
				period := staffPath + "/employment-periods/" + uuid.Must(uuid.NewV7()).String()
				terms, err := json.Marshal(map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "staff_version": basis["staff_version"], "employment_epoch": basis["employment_epoch"], "engagement_kind": "employee", "duration": "ongoing", "starts_at": "2026-11-01T00:00:00Z", "contracting_party": "", "sponsor_id": "", "reason": "Synthetic private employment evidence", "evidence_reference": "SYN-TERMS"})
				require.NoError(t, err)
				result := do(setup, "PUT", period, "manager", string(terms))
				require.Equal(t, 200, result.Code, result.Body.String())
				binding := period + "/revisions/1/evidence/" + uuid.Must(uuid.NewV7()).String()
				result = do(setup, "PUT", binding, "manager", fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic private contract"}`, uuid.Must(uuid.NewV7()).String(), fileID))
				require.Equal(t, 200, result.Code, result.Body.String())
				downloadPath, downloadAction = binding+"/content", "workforce.employment_evidence.downloaded"
			}
			authority, err := iam.NewCurrentAuthorization(db, registry, graph)
			require.NoError(t, err)
			signer, verifier := testutil.NewSessionSignerForTest(t)
			secure := gin.New()
			secure.Use(requestid.Middleware())
			api := platform.NewRouter(secure.Group("/v1", authn.Middleware(authn.NewSessionVerifier(verifier), iam.NewRevocationChecker(db)), requestid.PrincipalMiddleware(), authz.Middleware(authority)), authority)
			workforce.New().Routes(api, platform.Deps{DB: db, Bus: bus, Documents: files})
			upstreamServer := httptest.NewServer(secure)
			t.Cleanup(upstreamServer.Close)
			gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 10 * time.Second, Upstreams: map[string]string{"platform": upstreamServer.URL, "workforce": upstreamServer.URL}})
			require.NoError(t, err)

			token, err := signer.Mint("user-manager", testutil.TenantA, time.Now(), time.Now().Add(time.Hour))
			require.NoError(t, err)
			call := func(method, path, body string) *httptest.ResponseRecorder {
				request := httptest.NewRequest(method, path, strings.NewReader(body))
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				gateway.ServeHTTP(response, request)
				return response
			}
			res = call("GET", downloadPath, "")
			require.Equal(t, http.StatusOK, res.Code, res.Body.String())
			require.Equal(t, content.Bytes(), res.Body.Bytes())

			withdraw := func() {
				if mode == "graph_outage" {
					graphUnavailable.Store(true)
					return
				}
				if strings.HasPrefix(mode, "employment_") {
					raw, err := fgaclient.NewSdkClient(&fgaclient.ClientConfiguration{ApiUrl: graphURL})
					require.NoError(t, err)
					stores, err := raw.ListStores(ctx).Execute()
					require.NoError(t, err)
					storeID := ""
					for _, store := range stores.GetStores() {
						if store.GetName() == storeName && (storeID == "" || store.GetId() < storeID) {
							storeID = store.GetId()
						}
					}
					require.NotEmpty(t, storeID)
					require.NoError(t, raw.SetStoreId(storeID))

					permission := workforce.PermDocumentManage
					if mode == "employment_enrollment" {
						permission = workforce.PermEnrollmentManage
					}
					_, err = raw.Write(ctx).Body(fgaclient.ClientWriteRequest{Deletes: []fgaclient.ClientTupleKeyWithoutCondition{{User: authz.RoleObject(testutil.TenantA, authz.RoleTenantAdmin), Relation: "granted_role", Object: authz.PermObject(testutil.TenantA, permission)}}}).Execute()
					require.NoError(t, err)
					return
				}
				require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "synthetic-governance-owner"), testutil.TenantA, func(tx *gorm.DB) error {
					return tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND subject='user-manager' AND role_key='tenant_admin'`, testutil.TenantA).Error
				}))
			}
			if mode == "upload" {
				nextFile := uuid.NewString()
				nextPath := staffPath + "/documents/" + nextFile
				res = call("PUT", nextPath, intent)
				require.Equal(t, 201, res.Code, res.Body.String())
				cloud.AfterQuarantineWrite(withdraw)
				request := httptest.NewRequest("PUT", nextPath+"/content", bytes.NewReader(content.Bytes()))
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("Content-Type", "image/png")
				res = httptest.NewRecorder()
				gateway.ServeHTTP(res, request)
				require.Equal(t, http.StatusForbidden, res.Code, res.Body.String())
				require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
					if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, hospital).Error; err != nil {
						return err
					}
					var state struct {
						Status           string
						SourceGeneration *int64
					}
					if err := tx.Table("workforce_documents").Select("status,source_generation").Where("id=?", nextFile).Take(&state).Error; err != nil {
						return err
					}
					require.Equal(t, "upload_pending", state.Status)
					require.Nil(t, state.SourceGeneration)
					var queued int64
					if err := tx.Table("outbox_events").Where("subject=?", workforce.DocumentScanRequested).Count(&queued).Error; err != nil {
						return err
					}
					require.EqualValues(t, 1, queued, "the denied upload must not queue a second scan")
					return nil
				}))
			} else {
				cloud.AfterReleasedRead(withdraw)
				res = call("GET", downloadPath, "")
				expectedStatus := http.StatusForbidden
				if mode == "graph_outage" {
					expectedStatus = http.StatusServiceUnavailable
				}
				require.Equal(t, expectedStatus, res.Code, "ended or unavailable authority must not release bytes after storage I/O")
				require.NotEqual(t, content.Bytes(), res.Body.Bytes())
				require.Empty(t, res.Header().Get("Content-Disposition"))
				require.NotContains(t, res.Body.String(), "synthetic-private.png")
				require.Contains(t, res.Header().Get("Cache-Control"), "no-store")
			}
			currentGraph, err := graph.Resolve(ctx, "user-manager", testutil.TenantA)
			if mode == "graph_outage" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if mode == "employment_enrollment" {
				require.True(t, currentGraph.Has(workforce.PermDocumentManage))
				require.False(t, currentGraph.Has(workforce.PermEnrollmentManage))
			} else if mode == "employment_documents" {
				require.False(t, currentGraph.Has(workforce.PermDocumentManage))
				require.True(t, currentGraph.Has(workforce.PermEnrollmentManage))
			} else if mode != "graph_outage" {
				require.True(t, currentGraph.Has(workforce.PermDocumentManage), "the graph intentionally retains old authority")
			}
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var count int64
				if err := tx.Table("workforce_audit").Where("action=?", downloadAction).Count(&count).Error; err != nil {
					return err
				}
				require.EqualValues(t, 1, count, "only the earlier authorized read is recorded as successful")
				var level string
				if err := tx.Table("workforce_hospital_access").Where("tenant_id=? AND hospital_id=? AND subject='user-manager'", testutil.TenantA, hospital).Pluck("level", &level).Error; err != nil {
					return err
				}
				require.Equal(t, "manager", level, "a remaining owner grant must not substitute for the removed IAM permission")
				return nil
			}))

		})
	}
}
