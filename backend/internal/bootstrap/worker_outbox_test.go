package bootstrap_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/png"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/modules/supply"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestSupplyWorkerCompletesWithAllProductionMigrationsWithoutClinicalAccess(t *testing.T) {
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"owner": testutil.TenantA},
		Perms: map[string][]authz.Permission{"owner": {
			supply.PermPartyWrite, supply.PermPremiseWrite, supply.PermLicenseWrite, supply.PermEvidenceWrite,
		}},
		Modules: bootstrap.Modules(nil, nil), Documents: files,
	})
	party, premise, licence, file := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	root := "/v1/supply/parties/" + party
	res := testutil.Do(r, "PUT", root, "owner", `{"expected_version":0,"code":"TEST-POLICY","record_kind":"counterparty","legal_name":"TEST Policy Supplier","trading_name":"TEST","business_kind":"distributor","country_code":"IN","jurisdiction":"TEST-MH","registration_number":"TEST-001","corporate_group":"","website":"","default_credit_days":0,"contacts":[],"addresses":[],"tax_registrations":[]}`)
	require.Equal(t, 200, res.Code, res.Body.String())
	res = testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", `{"expected_version":0,"code":"TEST-DEPOT","name":"TEST Depot","kind":"warehouse","line1":"TEST Building","line2":"","city":"TEST City","region":"TEST Region","postal_code":"TEST-001","country_code":"IN","time_zone":"Asia/Kolkata","status":"active"}`)
	require.Equal(t, 200, res.Code, res.Body.String())
	path := root + "/licenses/" + licence
	res = testutil.Do(r, "PUT", path, "owner", fmt.Sprintf(`{"expected_version":0,"kind":"wholesale","number":"TEST-POLICY-001","issuing_authority":"TEST Authority","jurisdiction":"TEST-MH","premise_id":%q,"issued_on":"2026-01-01","expires_on":"2031-12-31","product_categories":["otc"]}`, premise))
	require.Equal(t, 200, res.Code, res.Body.String())
	var content bytes.Buffer
	require.NoError(t, png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	data := content.Bytes()
	path += "/evidence/" + file
	res = testutil.Do(r, "PUT", path, "owner", fmt.Sprintf(`{"expected_license_version":1,"filename":"TEST-policy.png","content_type":"image/png","size":%d,"sha256":"%x"}`, len(data), sha256.Sum256(data)))
	require.Equal(t, 201, res.Code, res.Body.String())
	req := httptest.NewRequest("PUT", path+"/content", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer owner")
	req.Header.Set("Content-Type", "image/png")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 202, w.Code, w.Body.String())
	var database string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.SupplyEvidenceWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := supply.NewEvidenceProcessor(t.Context(), worker, files, scanner)
	require.NoError(t, err)
	in := supplycontract.EvidenceScanInput{TenantID: testutil.TenantA, PartyID: party, LicenseID: licence, FileID: file, ScanAttempt: 1}
	for range 2 {
		status, processErr := processor.Process(t.Context(), in)
		require.NoError(t, processErr)
		require.Equal(t, "available", status)
	}
	require.Equal(t, 1, cloud.CopyCount())
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		err := tx.Table("outbox_events").Where("subject=?", supplycontract.SubjectEvidenceChanged).Count(&count).Error
		require.EqualValues(t, 1, count)
		return err
	}))
	in.TenantID = testutil.TenantB
	_, err = processor.Process(t.Context(), in)
	require.Error(t, err)
	err = worker.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`SELECT id FROM workforce_documents LIMIT 1`).Error
	})
	require.ErrorContains(t, err, "permission denied")
	hr, err := tenantdb.OpenRuntime(t.Context(), testinfra.DocumentWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, hr.Close()) })
	err = hr.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return events.Enqueue(tx, "helivanta.in.workforce.document_changed.v1", events.Event{
			Type: "helivanta.in.workforce.document_changed.v1", Version: 1, TenantID: testutil.TenantA,
			Data: []byte(`{"hospital_id":"11111111-1111-1111-1111-111111111111","staff_id":"11111111-1111-1111-1111-111111111111","file_id":"11111111-1111-1111-1111-111111111111","scan_attempt":1,"status":"available"}`),
		})
	})
	var denied *pgconn.PgError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, "42501", denied.Code)
	require.Contains(t, denied.Message, "row-level security")
}
