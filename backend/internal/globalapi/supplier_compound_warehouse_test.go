package globalapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func approveCompoundWarehouseEndpoint(t *testing.T, f operatingDutyFixture, files *attachments.GCS) (suppliertrade.Endpoint, string) {
	t.Helper()
	organization, premise, licence, evidence := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	root := "/v1/supply/parties/" + organization.String()
	f.call("PUT", root, "owner", map[string]any{"expected_version": 0, "code": "TEST-COMPOUND-WAREHOUSE", "record_kind": "owned_organisation", "legal_name": "TEST Independent Compound Distributor", "trading_name": "TEST Compound Supply", "business_kind": "distributor", "country_code": "IN", "jurisdiction": "IN-MH", "registration_number": "TEST-COMPOUND-REG", "corporate_group": "", "website": "https://supplier.example.invalid", "default_credit_days": 30, "contacts": []any{}, "addresses": []any{}, "tax_registrations": []any{}}, 200)
	f.call("PUT", root+"/premises/"+premise.String(), "owner", map[string]any{"expected_version": 0, "code": "TEST-COMPOUND-DEPOT", "name": "TEST Compound Nagpur Warehouse", "kind": "warehouse", "line1": "TEST Synthetic Warehouse", "line2": "", "city": "Nagpur", "region": "IN-MH", "postal_code": "440001", "country_code": "IN", "time_zone": "Asia/Kolkata", "status": "active"}, 200)
	path := root + "/licenses/" + licence.String()
	f.call("PUT", path, "owner", map[string]any{"expected_version": 0, "kind": "wholesale", "number": "TEST-" + licence.String(), "issuing_authority": "TEST Authority", "jurisdiction": "IN-MH", "premise_id": premise, "issued_on": "2026-01-01", "expires_on": "2030-12-31", "product_categories": []string{"prescription"}}, 200)
	for _, subject := range []string{"compound-source-owner", "compound-source-reviewer"} {
		f.call("PUT", root+"/access/"+subject+"/compliance", "owner", nil, 200)
	}
	var payload bytes.Buffer
	require.NoError(t, png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	digest := sha256.Sum256(payload.Bytes())
	evidencePath := path + "/evidence/" + evidence.String()
	f.call("PUT", evidencePath, "owner", map[string]any{"expected_license_version": 1, "filename": "TEST-compound-warehouse.png", "content_type": "image/png", "size": payload.Len(), "sha256": hex.EncodeToString(digest[:])}, 201)
	request := httptest.NewRequest("PUT", evidencePath+"/content", bytes.NewReader(payload.Bytes()))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Content-Type", "image/png")
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	require.Equal(t, 202, response.Code, response.Body.String())
	var database string
	require.NoError(t, f.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.SupplyEvidenceWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := supply.NewEvidenceProcessor(t.Context(), worker, files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), supplycontract.EvidenceScanInput{TenantID: f.principal.TenantID, PartyID: organization.String(), LicenseID: licence.String(), FileID: evidence.String(), ScanAttempt: 1})
	require.NoError(t, err)
	require.Equal(t, "available", status)
	for version, actor := range []string{"owner", "reviewer"} {
		action, method, reference := "submit", "not_applicable", ""
		if version == 1 {
			action, method, reference = "verify", "authority_contact", "TEST-independent-warehouse-check"
		}
		f.call("PUT", path+"/reviews/"+uuid.Must(uuid.NewV7()).String(), actor, map[string]any{"expected_version": version, "expected_party_version": 1, "expected_premise_version": 1, "expected_license_version": 1, "evidence_id": evidence, "action": action, "reason": "TEST independent synthetic warehouse review", "method": method, "reference": reference}, 201)
	}
	return suppliertrade.Endpoint{TenantID: uuid.MustParse(f.principal.TenantID), Product: "supply", OrganizationID: organization, LocationID: premise, OrganizationVersion: 1, LocationVersion: 1, LicenceID: licence, LicenceVersion: 1, Jurisdiction: "IN-MH"}, evidencePath
}
