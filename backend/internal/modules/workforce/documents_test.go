package workforce_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports its own module
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"google.golang.org/api/option"
	"gorm.io/gorm"
)

func TestStaffDocumentUploadPinsBytesAndPublishesOneScanIntent(t *testing.T) {
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	content := pngBytes.Bytes()
	digest := sha256.Sum256(content)
	var crcBytes [4]byte
	binary.BigEndian.PutUint32(crcBytes[:], crc32.Checksum(content, crc32.MakeTable(crc32.Castagnoli)))
	crc := base64.StdEncoding.EncodeToString(crcBytes[:])
	var uploads atomic.Int32
	var cloudKey string
	var beforeReceipt func()
	var storageMu sync.Mutex
	objects := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			const prefix = "/storage/v1/b/helivanta-test-quarantine/o/"
			key := strings.TrimPrefix(r.URL.Path, prefix)
			storageMu.Lock()
			exists := objects[key]
			storageMu.Unlock()
			require.True(t, exists)
			if r.URL.Query().Get("alt") == "media" {
				require.Equal(t, "12345", r.URL.Query().Get("generation"))
				w.Header().Set("X-Goog-Generation", "12345")
				w.Header().Set("X-Goog-Metageneration", "1")
				w.Header().Set("X-Goog-Hash", "crc32c="+crc)
				_, _ = w.Write(content)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"bucket": "helivanta-test-quarantine", "name": key, "generation": "12345", "size": strconv.Itoa(len(content)), "crc32c": crc})
			return
		}
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "0", r.URL.Query().Get("ifGenerationMatch"))
		uploads.Add(1)
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		parts := multipart.NewReader(r.Body, params["boundary"])
		part, err := parts.NextPart()
		require.NoError(t, err)
		var metadata map[string]any
		require.NoError(t, json.NewDecoder(part).Decode(&metadata))
		key := metadata["name"].(string)
		require.Equal(t, "private, no-store", metadata["cacheControl"])
		part, err = parts.NextPart()
		require.NoError(t, err)
		stored, err := io.ReadAll(part)
		require.NoError(t, err)
		require.Equal(t, content, stored)
		storageMu.Lock()
		if objects[key] {
			storageMu.Unlock()
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		objects[key] = true
		cloudKey = key
		storageMu.Unlock()
		if beforeReceipt != nil {
			beforeReceipt()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"bucket": "helivanta-test-quarantine", "name": key, "generation": "12345", "size": strconv.Itoa(len(content)), "crc32c": crc})
	}))
	t.Cleanup(server.Close)
	client, err := storage.NewClient(t.Context(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithHTTPClient(server.Client()), option.WithoutAuthentication(), storage.WithJSONReads())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	files, err := attachments.NewGCS(client, "helivanta-test-quarantine", "helivanta-test-released")
	require.NoError(t, err)
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"manager": testutil.TenantA, "foreign": testutil.TenantB, "reader": testutil.TenantA, "directory": testutil.TenantA, "enrollment-only": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"enrollment-only": {workforce.PermEnrollmentManage}, "manager": {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage}, "foreign": {workforce.PermEnrollmentManage, workforce.PermDocumentManage}, "reader": {workforce.PermEnrollmentManage, workforce.PermDocumentManage}},
		Modules: []platform.Module{workforce.New()}, Documents: files,
	})
	hospital, staff, file := uuid.NewString(), uuid.NewString(), uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "manager", `{"name":"Synthetic Upload Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-manager", "manager", `{"level":"manager"}`).Code)
	staffPath := base + "/staff/" + staff
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-enrollment-only", "manager", `{"level":"manager"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-reader", "manager", `{"level":"reader"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", staffPath, "manager", `{"employee_code":"UPLOAD-001","display_name":"Synthetic Upload Staff","job_category":"nurse","employment_status":"active"}`).Code)
	path := staffPath + "/documents/" + file
	body := `{"record_kind":"enrollment","record_id":"` + staff + `","category":"employment_evidence","filename":"synthetic-upload.png","content_type":"image/png","size":` + strconv.Itoa(len(content)) + `,"sha256":"` + hex.EncodeToString(digest[:]) + `"}`
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "manager", body).Code)
	uploadBytes := func(token, requestPath, contentType, encoding string, data []byte) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPut, requestPath+"/content", bytes.NewReader(data))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Content-Encoding", encoding)
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		return response
	}
	upload := func(token string) *httptest.ResponseRecorder {
		return uploadBytes(token, path, "image/png", "", content)
	}
	for _, denied := range []struct {
		token  string
		status int
	}{{"foreign", 404}, {"reader", 404}, {"directory", 403}, {"enrollment-only", 403}} {
		require.Equal(t, denied.status, upload(denied.token).Code)
	}
	require.Zero(t, uploads.Load(), "a tenant denial must not contact cloud storage")
	for _, invalid := range []struct {
		contentType, encoding string
		data                  []byte
	}{
		{"image/jpeg", "", content}, {"image/png", "gzip", content}, {"image/png", "", content[:len(content)-1]},
		{"image/png", "", append(bytes.Clone(content), 0)}, {"image/png", "", bytes.Repeat([]byte{0}, len(content))},
	} {
		response := uploadBytes("manager", path, invalid.contentType, invalid.encoding, invalid.data)
		require.Equal(t, 400, response.Code, response.Body.String())
	}
	require.Zero(t, uploads.Load(), "invalid bytes must not reach quarantine")
	first := upload("manager")
	require.Equal(t, 202, first.Code, first.Body.String())
	require.Contains(t, first.Body.String(), `"status":"scan_pending"`)
	require.NotContains(t, first.Body.String(), "12345")
	require.Equal(t, "no-store", first.Header().Get("Cache-Control"))
	forgedVerdict := db.WithTenant(tenantdb.WithActor(t.Context(), "user-manager"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true),set_config('app.workforce_document_worker','authorized',true),set_config('app.workforce_document_staff',?,true),set_config('app.workforce_document_file',?,true)`, hospital, staff, file).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE workforce_documents SET status='available',released_generation=98765,engine_version='synthetic-forged',definition_version=28000,definitions_updated_at=clock_timestamp(),scanned_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=?`, testutil.TenantA, hospital, staff, file).Error
	})
	require.Error(t, forgedVerdict, "a caller-set flag cannot authorize the API role to fabricate a clean release")
	var database string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT current_database()`).Scan(&database).Error
	}))
	workerDB, err := tenantdb.OpenRuntime(t.Context(), testinfra.DocumentWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, workerDB.Close()) })
	for _, claim := range []struct {
		tenant, hospital, staff, file string
		visible                       bool
	}{
		{testutil.TenantA, "", "", "", false},
		{testutil.TenantB, hospital, staff, file, false},
		{testutil.TenantA, uuid.NewString(), staff, file, false},
		{testutil.TenantA, hospital, uuid.NewString(), file, false},
		{testutil.TenantA, hospital, staff, uuid.NewString(), false},
		{testutil.TenantA, hospital, staff, file, true},
	} {
		require.NoError(t, workerDB.WithTenant(t.Context(), claim.tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_hospital',?,true),set_config('app.workforce_document_staff',?,true),set_config('app.workforce_document_file',?,true)`, claim.hospital, claim.staff, claim.file).Error; err != nil {
				return err
			}
			var ids []string
			require.NoError(t, tx.Table("workforce_documents").Select("id").Scan(&ids).Error)
			if claim.visible {
				require.Equal(t, []string{file}, ids)
			} else {
				require.Empty(t, ids)
			}
			return nil
		}))
	}
	for _, query := range []string{`SELECT id FROM workforce_staff`, `SELECT filename FROM workforce_documents`, `SELECT id FROM audit_events`} {
		require.Error(t, workerDB.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec(query).Error }), "the worker cannot read personnel, original names or audit history")
	}
	for range 3 {
		replayed := upload("manager")
		require.Equal(t, 200, replayed.Code, replayed.Body.String())
		require.JSONEq(t, first.Body.String(), replayed.Body.String())
	}
	require.EqualValues(t, 1, uploads.Load())
	require.Contains(t, cloudKey, "t/"+testutil.TenantA+"/h/"+hospital+"/o/staff/"+staff+"/r/"+staff+"/d/")
	require.Contains(t, cloudKey, "/f/"+file+"/v/1")
	require.NotContains(t, cloudKey, "synthetic-upload")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var events []struct{ Payload string }
		require.NoError(t, tx.Table("outbox_events").Select("payload").Where("subject=?", "helivanta.in.workforce.document_scan_requested.v1").Find(&events).Error)
		require.Len(t, events, 1)
		require.Contains(t, events[0].Payload, file)
		require.NotContains(t, events[0].Payload, "synthetic-upload")
		require.NotContains(t, events[0].Payload, hex.EncodeToString(digest[:]))
		return nil
	}))
	interruptedFile := uuid.NewString()
	interruptedPath := staffPath + "/documents/" + interruptedFile
	require.Equal(t, 201, testutil.Do(r, "PUT", interruptedPath, "manager", body).Code)
	beforeReceipt = func() {
		// A real grant revocation must not wait for the cloud request's locks.
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SET LOCAL lock_timeout='100ms'`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true)`).Error; err != nil {
				return err
			}
			return tx.Exec(`DELETE FROM workforce_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject='user-manager'`, testutil.TenantA, hospital).Error
		}))
	}
	interrupted := uploadBytes("manager", interruptedPath, "image/png", "", content)
	require.Equal(t, 404, interrupted.Code, interrupted.Body.String())
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-manager", "manager", `{"level":"manager"}`).Code)
	listed := testutil.Do(r, "GET", staffPath+"/documents", "manager", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	var listing struct {
		Items []struct{ ID, Status string } `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &listing))
	var found bool
	for _, item := range listing.Items {
		if item.ID == interruptedFile {
			found = true
			require.Equal(t, "upload_pending", item.Status, "revocation must prevent the committed scan handoff")
		}
	}
	require.True(t, found)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentScanRequested).Count(&count).Error)
		require.EqualValues(t, 1, count, "a revoked in-flight upload cannot publish a scan event")
		return nil
	}))
	beforeReceipt = nil
	concurrentFile := uuid.NewString()
	concurrentPath := staffPath + "/documents/" + concurrentFile
	require.Equal(t, 201, testutil.Do(r, "PUT", concurrentPath, "manager", body).Code)
	responses := make(chan *httptest.ResponseRecorder, 6)
	var callers sync.WaitGroup
	for range 6 {
		callers.Go(func() { responses <- uploadBytes("manager", concurrentPath, "image/png", "", content) })
	}
	callers.Wait()
	close(responses)
	var accepted int
	for response := range responses {
		if response.Code == http.StatusAccepted {
			accepted++
		} else {
			require.Equal(t, 200, response.Code, response.Body.String())
		}
		require.Contains(t, response.Body.String(), `"status":"scan_pending"`)
	}
	require.Equal(t, 1, accepted, "only one racing request may commit the scan handoff")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentScanRequested).Count(&count).Error)
		require.EqualValues(t, 2, count)
		return nil
	}))
	deliveryCtx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	delivered := make(chan events.Event, 4)
	require.NoError(t, bus.StartConsumers(deliveryCtx, db, []events.Consumer{{
		Name: "workforce-doc-acceptance", Subject: workforce.DocumentScanRequested,
		Handle: func(context.Context, *gorm.DB, events.Event) error { return nil },
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			select {
			case delivered <- evt:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}}))
	go bus.RunDispatcher(deliveryCtx, db)
	waiting := map[string]bool{file: true, concurrentFile: true}
	for len(waiting) > 0 {
		select {
		case evt := <-delivered:
			require.Equal(t, testutil.TenantA, evt.TenantID)
			require.Empty(t, evt.DestinationTenantID)
			var scan workforcecontract.DocumentScanRequestedData
			require.NoError(t, json.Unmarshal(evt.Data, &scan))
			require.Equal(t, hospital, scan.HospitalID)
			require.Equal(t, staff, scan.StaffID)
			require.True(t, waiting[scan.FileID], "only committed files may reach the durable consumer")
			delete(waiting, scan.FileID)
		case <-deliveryCtx.Done():
			t.Fatalf("scan request did not reach the durable JetStream consumer: %v", deliveryCtx.Err())
		}
	}
}

func TestStaffDocumentIntentsAreScopedPrivateAndIdempotent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("intent metadata must not call GCS while holding authorization locks")
		w.WriteHeader(500)
	}))
	t.Cleanup(server.Close)
	client, err := storage.NewClient(t.Context(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithHTTPClient(server.Client()), option.WithoutAuthentication(), storage.WithJSONReads())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	files, err := attachments.NewGCS(client, "helivanta-test-quarantine", "helivanta-test-released")
	require.NoError(t, err)
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"manager": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB, "directory": testutil.TenantA, "enrollment-only": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"enrollment-only": {workforce.PermEnrollmentManage}, "manager": {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage}, "reader": {workforce.PermEnrollmentManage, workforce.PermDocumentManage}, "foreign": {workforce.PermEnrollmentManage, workforce.PermDocumentManage}},
		Modules: []platform.Module{workforce.New()}, Documents: files,
	})
	hospital, staff, file := uuid.NewString(), uuid.NewString(), uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "manager", `{"name":"Synthetic File Hospital"}`).Code)
	for _, level := range []struct{ subject, level string }{{"manager", "manager"}, {"reader", "reader"}, {"directory", "manager"}, {"enrollment-only", "manager"}} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+level.subject, "manager", `{"level":"`+level.level+`"}`).Code)
	}
	staffPath := base + "/staff/" + staff
	require.Equal(t, 200, testutil.Do(r, "PUT", staffPath, "manager", `{"employee_code":"FILE-001","display_name":"Synthetic File Staff","job_category":"nurse","employment_status":"active"}`).Code)
	body := `{"record_kind":"enrollment","record_id":"` + staff + `","category":"employment_evidence","filename":"synthetic-evidence.png","content_type":"image/png","size":256,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	path := staffPath + "/documents/" + file
	w := testutil.Do(r, "PUT", path, "manager", body)
	require.Equal(t, 201, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var saved map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, "upload_pending", saved["status"])
	require.Equal(t, "synthetic-evidence.png", saved["filename"])
	require.Equal(t, staff, saved["record_id"])
	for range 3 {
		repeated := testutil.Do(r, "PUT", path, "manager", body)
		require.Equal(t, 200, repeated.Code, repeated.Body.String())
		require.JSONEq(t, w.Body.String(), repeated.Body.String())
	}
	for _, actor := range []struct {
		token  string
		status int
	}{{"reader", 404}, {"foreign", 404}, {"directory", 403}, {"enrollment-only", 403}, {"", 401}} {
		for _, method := range []string{"GET", "PUT"} {
			requestPath := path
			if method == "GET" {
				requestPath = staffPath + "/documents"
			}
			denied := testutil.Do(r, method, requestPath, actor.token, body)
			require.Equal(t, actor.status, denied.Code, denied.Body.String())
			require.NotContains(t, denied.Body.String(), "synthetic-evidence.png")
		}
	}
	listing := testutil.Do(r, "GET", staffPath+"/documents", "manager", "")
	require.Equal(t, 200, listing.Code, listing.Body.String())
	require.Contains(t, listing.Body.String(), "synthetic-evidence.png")
	for _, route := range []string{staffPath, base + "/staff"} {
		directory := testutil.Do(r, "GET", route, "manager", "")
		require.NotContains(t, directory.Body.String(), "synthetic-evidence.png")
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var rows int64
		require.NoError(t, tx.Table("workforce_documents").Count(&rows).Error)
		require.Zero(t, rows, "plain tenant scope must not expose confidential file metadata")
		return nil
	}))
	qualificationID := uuid.NewString()
	enrollment := `{"expected_version":1,"staff":{"employee_code":"FILE-001","display_name":"Synthetic File Staff","job_category":"nurse","employment_status":"active"},"profile":{"engagement_type":"employee"},"qualifications":[{"id":"` + qualificationID + `","qualification":"Synthetic certificate","institution":"Synthetic school","completion_year":2020}]}`
	enrolled := testutil.Do(r, "PUT", staffPath+"/enrollment", "manager", enrollment)
	require.Equal(t, 200, enrolled.Code, enrolled.Body.String())
	qualifiedBody := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(body, `"enrollment"`, `"qualification"`), staff, qualificationID), `"employment_evidence"`, `"qualification_evidence"`)
	qualifiedPath := staffPath + "/documents/" + uuid.NewString()
	qualified := testutil.Do(r, "PUT", qualifiedPath, "manager", qualifiedBody)
	require.Equal(t, 201, qualified.Code, qualified.Body.String())
	removed := testutil.Do(r, "PUT", staffPath+"/enrollment", "manager", `{"expected_version":2,"staff":{"employee_code":"FILE-001","display_name":"Synthetic File Staff","job_category":"nurse","employment_status":"active"},"profile":{"engagement_type":"employee"},"qualifications":[]}`)
	require.Equal(t, 200, removed.Code, removed.Body.String())
	replay := testutil.Do(r, "PUT", qualifiedPath, "manager", qualifiedBody)
	require.Equal(t, 200, replay.Code, "historical record removal must not change a completed intent's retry receipt: "+replay.Body.String())
	require.JSONEq(t, qualified.Body.String(), replay.Body.String())
	forged := db.WithTenant(tenantdb.WithActor(t.Context(), "user-manager"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, hospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_documents(tenant_id,hospital_id,id,staff_id,record_id,record_kind,record_version,category,filename,content_type,size,sha256,status,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at)
		 SELECT tenant_id,hospital_id,?::uuid,staff_id,record_id,record_kind,3,category,filename,content_type,size,sha256,'available',clock_timestamp(),1,2,'1.5.1',28000,clock_timestamp(),clock_timestamp()
		 FROM workforce_documents WHERE id=?`, uuid.NewString(), file).Error
	})
	require.Error(t, forged, "even an authorized application transaction cannot insert a fabricated release")
	require.Equal(t, 204, testutil.Do(r, "DELETE", base+"/access/user-manager", "manager", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", staffPath+"/documents", "manager", "").Code)
}
