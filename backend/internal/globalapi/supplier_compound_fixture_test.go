package globalapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/session"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type supplierCompoundFixture struct {
	graphUnavailable *atomic.Bool
	db               *tenantdb.DB
	graph            *authz.Client
	registry         *platform.Registry
	signer           *session.Signer
	handler          http.Handler
	files            *attachments.GCS
	binding          *pharmacyBindingPreparation
	hospitalCall     func(*testing.T, string, string, string, any, int) map[string]any
}

func (f supplierCompoundFixture) actors(t *testing.T, tenant string) operatingDutyFixture {
	t.Helper()
	require.NoError(t, platform.ReconcileTenant(t.Context(), f.registry, f.graph, tenant))
	require.NoError(t, f.graph.GrantTenantRole(t.Context(), tenant, authz.RoleTenantAdmin))
	tokens := map[string]string{}
	subjects := map[string]string{"owner": "compound-source-owner", "reviewer": "compound-source-reviewer"}
	authenticated := time.Now()
	for alias, subject := range subjects {
		require.NoError(t, f.graph.GrantRole(t.Context(), tenant, subject, authz.RoleTenantAdmin))
		token, err := f.signer.Mint(subject, tenant, authenticated, authenticated.Add(time.Hour), "")
		require.NoError(t, err)
		tokens[alias] = token
	}
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		request = request.Clone(request.Context())
		alias := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		if token, ok := tokens[alias]; ok {
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("X-Helivanta-Expected-Tenant", tenant)
			request.Header.Set("X-Helivanta-Expected-Subject", subjects[alias])
		}
		f.handler.ServeHTTP(response, request)
	})
	call := func(method, path, actor string, input any, status int) string {
		t.Helper()
		var body []byte
		if input != nil {
			var err error
			body, err = json.Marshal(input)
			require.NoError(t, err)
		}
		request := httptest.NewRequest(method, path, bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+actor)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, status, response.Code, "%s %s: %s", method, path, response.Body.String())
		return response.Body.String()
	}
	current := operatingDutyFixture{ctx: t.Context(), db: f.db, graph: f.graph, handler: handler, call: call, principal: authn.Principal{TenantID: tenant, Subject: subjects["owner"], AuthTime: authenticated, IdleDeadline: authenticated.Add(time.Hour)}}
	call("POST", "/v1/iam/members", "owner", map[string]any{"subject": subjects["reviewer"], "role_key": string(authz.RolePharmacyOwner)}, 202)
	reviewer := current.principal
	reviewer.Subject = subjects["reviewer"]
	require.Eventually(t, func() bool {
		return iam.NewSourceAdministrationGuard(f.db, f.graph).WithSourceAdministration(t.Context(), reviewer, pharmacy.PermTradingScopeReview, func(context.Context) error { return nil }) == nil
	}, 20*time.Second, 100*time.Millisecond)
	return current
}

func approveCompoundPharmacyEndpoint(t *testing.T, f operatingDutyFixture, files *attachments.GCS, organization, outlet uuid.UUID) suppliertrade.Endpoint {
	t.Helper()
	root := "/v1/pharmacy/pharmacies/" + organization.String() + "/outlets/" + outlet.String()
	for _, subject := range []string{"compound-source-owner", "compound-source-reviewer"} {
		f.call("PUT", root+"/access/"+subject, "owner", map[string]any{"level": "editor"}, 200)
	}
	licence, scope := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	path := root + "/licences/" + licence.String()
	f.call("PUT", path, "owner", map[string]any{"licence_kind": "wholesale", "form_label": "TEST-FORM", "licence_number": "TEST-" + licence.String(), "issuing_authority": "TEST Issuing Authority", "valid_from": "2025-01-01", "valid_until": "2030-12-31", "pharmacist_name": "TEST Pharmacist", "pharmacist_registration": "TEST-PROFESSIONAL"}, 201)
	var payload bytes.Buffer
	require.NoError(t, png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	digest := sha256.Sum256(payload.Bytes())
	evidence := uuid.Must(uuid.NewV7())
	evidencePath := path + "/evidence/" + evidence.String()
	f.call("PUT", evidencePath, "owner", map[string]any{"filename": "TEST-compound-licence.png", "content_type": "image/png", "size": payload.Len(), "sha256": hex.EncodeToString(digest[:])}, 201)
	request := httptest.NewRequest("PUT", evidencePath+"/content", bytes.NewReader(payload.Bytes()))
	request.Header.Set("Authorization", "Bearer owner")
	request.Header.Set("Content-Type", "image/png")
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	require.Equal(t, 202, response.Code, response.Body.String())
	var database string
	require.NoError(t, f.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.PharmacyEvidenceWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := pharmacy.NewLicenceEvidenceProcessor(t.Context(), worker, files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), pharmacycontract.LicenceEvidenceScanInput{TenantID: f.principal.TenantID, OutletID: outlet.String(), LicenceID: licence.String(), FileID: evidence.String(), ScanAttempt: 1})
	require.NoError(t, err)
	require.Equal(t, "available", status)
	f.call("POST", path+"/review", "reviewer", map[string]any{"decision": "approved", "reason": "TEST independently checked synthetic licence"}, 200)
	scopePath := root + "/trading-scopes/" + scope.String()
	f.call("PUT", scopePath, "owner", suppliertrade.PharmacyTradingScopeInput{LicenceID: licence, Jurisdiction: "IN-MH", Category: "prescription", Reason: "TEST compound source scope", AttestPremiseAndLicenceScope: true}, 201)
	f.call("PUT", scopePath+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", suppliertrade.PharmacyTradingScopeCommand{ExpectedVersion: 1, Decision: "approve", Reason: "TEST independent synthetic scope review", AttestPremiseAndLicenceScope: true}, 201)
	approveSyntheticTradingPolicyForReviewer(t, f, "IN-MH", "prescription", "order", "compound-source-reviewer")
	version := int64(2)
	return suppliertrade.Endpoint{TenantID: uuid.MustParse(f.principal.TenantID), Product: "pharmacy", OrganizationID: organization, LocationID: outlet, OrganizationVersion: 1, LocationVersion: 1, LicenceID: licence, LicenceVersion: 1, Jurisdiction: "IN-MH", RegistrationID: &scope, RegistrationVersion: &version}
}

func confirmedCompoundRelationship(t *testing.T, db *tenantdb.DB, seller, buyer suppliertrade.Endpoint) suppliertrade.Relationship {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	terms := suppliertrade.Terms{Seller: seller, Buyer: buyer, Category: "prescription", Currency: "INR", ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), TermsReference: "TEST bilateral synthetic source sharing"}
	source, err := suppliertrade.PrepareRelationship(uuid.Must(uuid.NewV7()), terms, "TEST-source-preparer", now)
	require.NoError(t, err)
	sourceStates := []suppliertrade.Relationship{source}
	source, offer, err := suppliertrade.DecideRelationship(source, source.Version, "approve", "TEST-source-reviewer", now)
	require.NoError(t, err)
	require.NotNil(t, offer)
	sourceStates = append(sourceStates, source)
	home, _, err := suppliertrade.ApplyRelationshipExchange(nil, *offer, buyer.TenantID, now)
	require.NoError(t, err)
	homeStates := []suppliertrade.Relationship{home}
	home, accepted, err := suppliertrade.DecideRelationship(home, home.Version, "approve", "TEST-hospital-reviewer", now)
	require.NoError(t, err)
	require.NotNil(t, accepted)
	homeStates = append(homeStates, home)
	source, ack, err := suppliertrade.ApplyRelationshipExchange(&source, *accepted, seller.TenantID, now)
	require.NoError(t, err)
	require.NotNil(t, ack)
	sourceStates = append(sourceStates, source)
	home, _, err = suppliertrade.ApplyRelationshipExchange(&home, *ack, buyer.TenantID, now)
	require.NoError(t, err)
	homeStates = append(homeStates, home)
	require.True(t, suppliertrade.RelationshipCurrent(source, now))
	require.True(t, suppliertrade.RelationshipCurrent(home, now))
	for _, states := range [][]suppliertrade.Relationship{sourceStates, homeStates} {
		require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "TEST-fixture"), states[0].OwnerTenantID.String(), func(tx *gorm.DB) error {
			var input, receipt *string
			if states[0].Side == "seller" {
				raw, err := json.Marshal(suppliertrade.SupplierRelationshipInput{Terms: terms})
				if err != nil {
					return err
				}
				initial, err := json.Marshal(states[0])
				if err != nil {
					return err
				}
				a, b := string(raw), string(initial)
				input, receipt = &a, &b
			}
			for index, state := range states {
				raw, err := json.Marshal(state)
				if err != nil {
					return err
				}
				if index == 0 {
					err = tx.Exec(`INSERT INTO supply_trade_relationships(tenant_id,origin_tenant_id,origin_record_id,id,body,proposal_input,proposal_receipt,version,created_at,updated_at)VALUES(?,?,?,?,?::jsonb,?::jsonb,?::jsonb,?,?,?)`, state.OwnerTenantID, seller.TenantID, state.ID, state.ID, string(raw), input, receipt, state.Version, state.CreatedAt, state.UpdatedAt).Error
				} else {
					err = tx.Exec(`UPDATE supply_trade_relationships SET body=?::jsonb,version=?,updated_at=? WHERE tenant_id=? AND id=? AND version=?`, string(raw), state.Version, state.UpdatedAt, state.OwnerTenantID, state.ID, states[index-1].Version).Error
				}
				if err != nil {
					return err
				}
				if err = tx.Exec(`INSERT INTO supply_trade_relationship_history(tenant_id,origin_tenant_id,origin_record_id,relationship_id,version,body,actor_subject,created_at)VALUES(?,?,?,?,?,?::jsonb,?,?)`, state.OwnerTenantID, seller.TenantID, state.ID, state.ID, state.Version, string(raw), "TEST-fixture", state.UpdatedAt).Error; err != nil {
					return err
				}
			}
			return nil
		}))
	}
	return source
}

func (f supplierCompoundFixture) assertAdmission(t *testing.T, subject, reviewer string, memberData map[string]any) func() {
	t.Helper()
	var member iam.PartnerGroupMemberProposal
	raw, err := json.Marshal(memberData)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &member))
	require.NotNil(t, f.binding)
	bindingID := uuid.Must(uuid.NewV7())
	bindingPath := f.binding.base + "/hospitals/" + member.HospitalID.String() + "/staff-department-bindings/" + bindingID.String()
	f.hospitalCall(t, "PUT", bindingPath, f.binding.preparer, map[string]any{"department_id": f.binding.pending["department_id"], "department_revision_id": f.binding.pending["department_revision_id"], "replaces_binding_id": nil, "replaces_binding_version": nil, "reason": "TEST current hospital buyer binding"}, 200)
	f.hospitalCall(t, "POST", bindingPath+"/commands", reviewer, map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "operation": "approve", "expected_version": 1, "reason": "TEST independent hospital buyer approval", "attest_department_responsibility": true}, 200)
	buyerActor := f.actors(t, testutil.TenantA)
	parts := strings.Split(f.binding.base, "/")
	buyer := approveCompoundPharmacyEndpoint(t, buyerActor, f.files, uuid.MustParse(parts[4]), uuid.MustParse(parts[6]))
	bindingVersion := int64(2)
	buyer.HospitalID, buyer.BindingID, buyer.BindingVersion = &member.HospitalID, &bindingID, &bindingVersion
	sellerActor := f.actors(t, testutil.TenantB)
	organization, outlet := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	sourceRoot := "/v1/pharmacy/pharmacies/" + organization.String()
	sellerActor.call("PUT", sourceRoot, "owner", map[string]any{"code": "TEST-COMPOUND-SOURCE", "name": "TEST Independent Compound Pharmacy", "ownership_type": "independent"}, 200)
	sellerActor.call("PUT", sourceRoot+"/outlets/"+outlet.String(), "owner", map[string]any{"code": "TEST-COMPOUND-MH", "name": "TEST Compound Nagpur Premise", "country_code": "IN", "timezone": "Asia/Kolkata"}, 200)
	seller := approveCompoundPharmacyEndpoint(t, sellerActor, f.files, organization, outlet)
	relationship := confirmedCompoundRelationship(t, f.db, seller, buyer)
	paths := map[string]string{}

	for _, kind := range []string{"subject", "group"} {
		require.True(t, t.Run("effective pharmacy shipment grant for "+kind, func(t *testing.T) {
			request := seedSupplierRecipientFixture(t, f.db, testutil.TenantA, subject, member, kind, "pharmacy", "partner.shipment_reader.v1", relationship)
			base := fmt.Sprintf("/v1/supply/hospitals/%s/partner-grants/pharmacy/%s/terms/%s", member.HospitalID, request.GrantID, relationship.TermsSha256)
			paths[kind] = base
			result := f.hospitalCall(t, "GET", base+"/shipment-source", subject, nil, 200)
			require.Equal(t, "TEST Independent Compound Pharmacy", result["source_organization_name"])
			require.Equal(t, "TEST Compound Nagpur Premise", result["source_location_name"])
			require.Equal(t, request.GrantID.String(), result["grant_id"])
			require.Equal(t, relationship.ID.String(), result["relationship_id"])
			require.Equal(t, relationship.TermsSha256, result["terms_sha256"])
			sharedScope := result["scope"].(map[string]any)
			require.Equal(t, testutil.TenantB, sharedScope["source_tenant_id"])
			require.Equal(t, testutil.TenantA, sharedScope["recipient_tenant_id"])
			require.Equal(t, member.HospitalID.String(), sharedScope["hospital_id"])
			require.Equal(t, seller.OrganizationID.String(), sharedScope["source_organization_id"])
			require.Equal(t, seller.LocationID.String(), sharedScope["source_location_id"])
			orders := f.hospitalCall(t, "GET", base+"/shipment-orders", subject, nil, 200)
			require.Contains(t, orders, "data")
			require.IsType(t, []any{}, orders["data"])
			require.Empty(t, orders["data"])
			require.Equal(t, false, orders["page"].(map[string]any)["has_more"])
			f.hospitalCall(t, "GET", strings.Replace(base, relationship.TermsSha256, strings.Repeat("f", 64), 1)+"/shipment-source", subject, nil, 404)
		}))
		sellerActor.call("GET", paths[kind]+"/shipment-source", "owner", nil, 404)
	}
	require.True(t, t.Run("current compound grant fails closed during graph outage", func(t *testing.T) {
		require.NotNil(t, f.graphUnavailable)
		f.graphUnavailable.Store(true)
		t.Cleanup(func() { f.graphUnavailable.Store(false) })
		unavailable := f.hospitalCall(t, "GET", paths["subject"]+"/shipment-source", subject, nil, 503)
		require.NotContains(t, unavailable, "scope")
		require.NotContains(t, unavailable, "source_organization_name")
		f.graphUnavailable.Store(false)
		f.hospitalCall(t, "GET", paths["subject"]+"/shipment-source", subject, nil, 200)
	}))
	require.True(t, t.Run("compound source disclosure requires its committed audit", func(t *testing.T) {
		apply := func(sql string) {
			t.Helper()
			require.NoError(t, f.db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "synthetic_compound_audit_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
		}
		clear := func() { apply(`ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS synthetic_compound_source_audit`) }
		t.Cleanup(clear)
		apply(`ALTER TABLE audit_events ADD CONSTRAINT synthetic_compound_source_audit CHECK(action<>'partner_shipment_source_read') NOT VALID`)
		unavailable := f.hospitalCall(t, "GET", paths["subject"]+"/shipment-source", subject, nil, 503)
		require.NotContains(t, unavailable, "scope")
		require.NotContains(t, unavailable, "source_organization_name")
		clear()
		f.hospitalCall(t, "GET", paths["subject"]+"/shipment-source", subject, nil, 200)
	}))

	return func() {
		f.hospitalCall(t, "GET", paths["group"]+"/shipment-source", subject, nil, 404)
		f.hospitalCall(t, "GET", paths["subject"]+"/shipment-source", subject, nil, 200)
		licencePath := sourceRoot + "/outlets/" + outlet.String() + "/licences/" + seller.LicenceID.String() + "/revocations/" + uuid.Must(uuid.NewV7()).String()
		sellerActor.call("PUT", licencePath, "reviewer", suppliertrade.PharmacyLicenceRevocationInput{Reason: "TEST immediate compound source withdrawal"}, 201)
		f.hospitalCall(t, "GET", paths["subject"]+"/shipment-source", subject, nil, 404)
	}
}
