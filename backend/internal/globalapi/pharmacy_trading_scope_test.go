package globalapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/modules/workspace"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPharmacyTradingScopeNeedsCurrentSourceAdministrationAndIndependentEvidence(t *testing.T) {
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	f := newOperatingDutyFixtureWithOwners(t, files, pharmacy.New(), workspace.New())
	call := f.call
	organization, outlet, licence, scope := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	root := "/v1/pharmacy/pharmacies/" + organization.String()
	path := root + "/outlets/" + outlet.String()
	call("PUT", root, "owner", map[string]any{"code": "TEST-TRADE-PHARMACY", "name": "TEST Independent Supplier Pharmacy", "ownership_type": "independent"}, 200)
	call("PUT", path, "owner", map[string]any{"code": "TEST-MH", "name": "TEST Pune Premise", "country_code": "IN", "timezone": "Asia/Kolkata"}, 200)
	for _, actor := range []string{"owner", "reviewer"} {
		call("PUT", path+"/access/user-"+actor, "owner", map[string]any{"level": "editor"}, 200)
	}
	call("POST", "/v1/iam/members", "owner", map[string]any{"subject": "user-reviewer", "role_key": string(authz.RolePharmacyOwner)}, 202)
	reviewerPrincipal := f.principal
	reviewerPrincipal.Subject = "user-reviewer"
	require.Eventually(t, func() bool {
		return iam.NewSourceAdministrationGuard(f.db, f.graph).WithSourceAdministration(t.Context(), reviewerPrincipal, pharmacy.PermTradingScopeReview, func(context.Context) error { return nil }) == nil
	}, 20*time.Second, 100*time.Millisecond)
	licencePath := path + "/licences/" + licence.String()
	call("PUT", licencePath, "owner", map[string]any{"licence_kind": "wholesale", "form_label": "TEST-FORM", "licence_number": "TEST-LICENCE", "issuing_authority": "TEST Issuing Authority", "valid_from": "2025-01-01", "valid_until": "2030-12-31", "pharmacist_name": "TEST Pharmacist", "pharmacist_registration": "TEST-PROFESSIONAL"}, 201)
	body := suppliertrade.PharmacyTradingScopeInput{LicenceID: licence, Jurisdiction: "IN-MH", Category: "prescription", Reason: "TEST verified premise and category scope", AttestPremiseAndLicenceScope: true}
	scopePath := path + "/trading-scopes/" + scope.String()
	first := call("PUT", scopePath, "owner", body, 201)
	call("PUT", scopePath, "receiver", body, 403)
	require.JSONEq(t, first, call("GET", scopePath+"/proposal-receipt", "owner", nil, 200))
	call("GET", scopePath+"/proposal-receipt", "reviewer", nil, 404)
	changed := body
	changed.Jurisdiction = "IN-OD"
	call("PUT", scopePath, "owner", changed, 409)
	approve := suppliertrade.PharmacyTradingScopeCommand{ExpectedVersion: 1, Decision: "approve", Reason: "TEST independent source scope review", AttestPremiseAndLicenceScope: true}
	call("PUT", scopePath+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner", approve, 409)
	call("PUT", scopePath+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", approve, 409)
	evidence := uuid.Must(uuid.NewV7())
	evidencePath := licencePath + "/evidence/" + evidence.String()
	var payload bytes.Buffer
	require.NoError(t, png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	digest := sha256.Sum256(payload.Bytes())
	call("PUT", evidencePath, "owner", map[string]any{"filename": "TEST-licence.png", "content_type": "image/png", "size": payload.Len(), "sha256": hex.EncodeToString(digest[:])}, 201)
	request := httptest.NewRequest("PUT", evidencePath+"/content", bytes.NewReader(payload.Bytes()))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Content-Type", "image/png")
	request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	request.Header.Set("X-Helivanta-Expected-Subject", "user-owner")
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
	var database string
	require.NoError(t, f.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.PharmacyEvidenceWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := pharmacy.NewLicenceEvidenceProcessor(t.Context(), worker, files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), pharmacycontract.LicenceEvidenceScanInput{TenantID: testutil.TenantA, OutletID: outlet.String(), LicenceID: licence.String(), FileID: evidence.String(), ScanAttempt: 1})
	require.NoError(t, err)
	require.Equal(t, "available", status)
	call("POST", licencePath+"/review", "reviewer", map[string]any{"decision": "approved", "reason": "TEST independently checked synthetic licence"}, 200)
	command := uuid.Must(uuid.NewV7())
	decision := call("PUT", scopePath+"/commands/"+command.String(), "reviewer", approve, 201)
	require.Contains(t, decision, `"status":"approved"`)
	approveSyntheticTradingPolicy(t, f, "IN-MH", "prescription", "order")
	registrationVersion := int64(2)
	endpoint := suppliertrade.Endpoint{TenantID: uuid.MustParse(testutil.TenantA), Product: "pharmacy", OrganizationID: organization, LocationID: outlet, OrganizationVersion: 1, LocationVersion: 1, LicenceID: licence, LicenceVersion: 1, Jurisdiction: "IN-MH", RegistrationID: &scope, RegistrationVersion: &registrationVersion}
	sources := platform.TradingEndpoints([]platform.Module{pharmacy.New()}, f.db, nil)
	seen := 0
	check := func() error {
		return f.db.WithGuardedTenantCommand(tenantdb.WithActor(t.Context(), "user-owner"), testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
			return sources.WithCurrentTradingEndpoint(ctx, endpoint, "prescription", "order", func(ctx context.Context, ref suppliertrade.EndpointReference) error {
				seen++
				require.True(t, suppliertrade.EndpointEqual(endpoint, ref.Endpoint))
				require.Equal(t, "order", ref.Operation)
				return f.db.WithTenantCommand(ctx, testutil.TenantA, func(tx *gorm.DB) error {
					var purpose string
					if err := tx.Raw(`SELECT COALESCE(current_setting('app.pharmacy_trade_source',true),'')`).Scan(&purpose).Error; err != nil {
						return err
					}
					require.Empty(t, purpose)
					return nil
				})
			})
		})
	}
	require.NoError(t, check())
	require.Equal(t, 1, seen)
	revocationID := uuid.Must(uuid.NewV7())
	revoked := call("PUT", licencePath+"/revocations/"+revocationID.String(), "reviewer", suppliertrade.PharmacyLicenceRevocationInput{Reason: "TEST immediate licence withdrawal"}, 201)
	require.JSONEq(t, revoked, call("GET", licencePath+"/revocations/"+revocationID.String(), "reviewer", nil, 200))
	require.JSONEq(t, revoked, call("PUT", licencePath+"/revocations/"+revocationID.String(), "reviewer", suppliertrade.PharmacyLicenceRevocationInput{Reason: "TEST immediate licence withdrawal"}, 200))
	call("GET", licencePath+"/revocations/"+revocationID.String(), "owner", nil, 404)
	call("PUT", licencePath+"/revocations/"+revocationID.String(), "reviewer", suppliertrade.PharmacyLicenceRevocationInput{Reason: "TEST altered intent"}, 409)
	require.ErrorIs(t, check(), suppliertrade.ErrIneligible)
	require.Equal(t, 1, seen)
	// With no positive cache, the immediately following current-source read stays denied.
	require.True(t, errors.Is(check(), suppliertrade.ErrIneligible))

	require.JSONEq(t, decision, call("GET", scopePath+"/commands/"+command.String(), "reviewer", nil, 200))
	call("GET", scopePath+"/commands/"+command.String(), "owner", nil, 404)
	call("GET", scopePath, "foreign", nil, 403)
	call("GET", scopePath+"?hospital_id="+uuid.NewString(), "owner", nil, 400)
	revoke := suppliertrade.PharmacyTradingScopeCommand{ExpectedVersion: 2, Decision: "revoke", Reason: "TEST source premise scope withdrawn"}
	call("PUT", scopePath+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", revoke, 201)
	require.JSONEq(t, decision, call("GET", scopePath+"/commands/"+command.String(), "reviewer", nil, 200))
	require.JSONEq(t, first, call("GET", scopePath+"/proposal-receipt", "owner", nil, 200))
	require.Contains(t, call("GET", scopePath, "owner", nil, 200), `"status":"revoked"`)
	page := call("GET", path+"/trading-scopes?limit=1", "owner", nil, 200)
	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(page), &decoded))
	require.Contains(t, decoded, "page")
	// Source roles do not authorize a different private outlet.
	second := uuid.Must(uuid.NewV7())
	call("PUT", root+"/outlets/"+second.String(), "owner", map[string]any{"code": "TEST-PRIVATE", "name": "TEST Other Premise", "country_code": "IN", "timezone": "Asia/Kolkata"}, 200)
	call("GET", root+"/outlets/"+second.String()+"/trading-scopes", "reviewer", nil, 404)
}
