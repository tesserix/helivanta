package globalapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

func (f supplierCompoundFixture) sourceStaffRequest(t *testing.T, actor authn.Principal, method, path string, input any) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		require.NoError(t, err)
	}
	token, err := f.signer.Mint(actor.Subject, actor.TenantID, actor.AuthTime, actor.IdleDeadline, "")
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Helivanta-Expected-Tenant", actor.TenantID)
	request.Header.Set("X-Helivanta-Expected-Subject", actor.Subject)
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

func (f supplierCompoundFixture) assertPublicPharmacyOrder(t *testing.T, admin, operator operatingDutyFixture, relationship suppliertrade.Relationship, department uuid.UUID, preparer, reviewer string) {
	t.Helper()
	seller, buyer := relationship.Terms.Seller, relationship.Terms.Buyer
	product := uuid.Must(uuid.NewV7())
	admin.call("PUT", "/v1/pharmacy/pharmacies/"+seller.OrganizationID.String()+"/outlets/"+seller.LocationID.String()+"/catalogue/products/"+product.String(), "owner", json.RawMessage(receiverProduct), 200)
	orderID, lineID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	base := "/v1/supply/hospitals/" + buyer.HospitalID.String() + "/departments/" + department.String() + "/outlets/" + buyer.LocationID.String() + "/purchase-orders/" + orderID.String()
	input := supplierorder.SupplierOrderInput{RelationshipID: relationship.ID, TermsSha256: relationship.TermsSha256, PurchaseOrderReference: "TEST-COMPOUND-PUBLIC-PHARMACY", QuoteReference: "TEST owner-selected current capsule", ExpiresAt: time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond), Reason: "TEST hospital intent through current Staff authority", Lines: []supplierorder.SupplierOrderLine{{ID: lineID, Product: supplierorder.SupplierOrderProduct{Owner: "pharmacy", ProductID: product, Version: ptr(int64(1))}, QuantityUnits: 10, UnitCode: "capsule"}}}
	proposal := f.hospitalCall(t, "PUT", base, preparer, input, 201)
	require.Equal(t, "pending_review", proposal["status"])
	require.Equal(t, preparer, proposal["prepared_by"])
	require.Equal(t, proposal, f.hospitalCall(t, "PUT", base, preparer, input, 200))
	decision := supplierorder.SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST independent department procurement review"}
	approvalPath := base + "/commands/" + uuid.Must(uuid.NewV7()).String()
	approval := f.hospitalCall(t, "PUT", approvalPath, reviewer, decision, 201)
	require.Equal(t, "offered", approval["order"].(map[string]any)["status"])
	require.Equal(t, reviewer, approval["actor_subject"])
	require.Equal(t, approval, f.hospitalCall(t, "PUT", approvalPath, reviewer, decision, 200))
	sourcePath := "/v1/supply/supplier-orders/" + orderID.String()
	var incoming supplierorder.SupplierOrder
	require.Eventually(t, func() bool {
		response := f.sourceStaffRequest(t, operator.principal, "GET", sourcePath, nil)
		require.Contains(t, []int{200, 404}, response.Code, response.Body.String())
		if response.Code != 200 {
			return false
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &incoming))
		return incoming.Status == "incoming"
	}, 20*time.Second, 100*time.Millisecond)
	require.Equal(t, orderID, incoming.ID)
	require.Equal(t, input.TermsSha256, incoming.Input.TermsSha256)
	require.Equal(t, seller.TenantID, incoming.OwnerTenantID)
	commandPath := sourcePath + "/commands/" + uuid.Must(uuid.NewV7()).String()
	accept := supplierorder.SupplierOrderDecisionInput{ExpectedVersion: incoming.Version, Decision: "approve", Reason: "TEST source Staff confirms actual current catalogue"}
	denied := f.sourceStaffRequest(t, operator.principal, "PUT", commandPath, accept)
	require.Equal(t, 403, denied.Code, "a fulfilment duty cannot approve supplier orders: %s", denied.Body.String())
	acceptor := operator.input
	acceptor.TemplateKey, acceptor.Reason = "pharmacy.order_acceptor.v1", "TEST separate current-source order acceptance duty"
	dutyPath := "/v1/iam/operating-duties/" + uuid.Must(uuid.NewV7()).String()
	admin.call("PUT", dutyPath, "owner", acceptor, 201)
	admin.call("PUT", dutyPath+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST independent source order-acceptor approval"}, 201)
	require.Eventually(t, func() bool {
		var duty iam.OperatingDuty
		require.NoError(t, json.Unmarshal([]byte(admin.call("GET", dutyPath, "owner", nil, 200)), &duty))
		return duty.Status == "active"
	}, 20*time.Second, 100*time.Millisecond)
	accepted := f.sourceStaffRequest(t, operator.principal, "PUT", commandPath, accept)
	require.Equal(t, 201, accepted.Code, accepted.Body.String())
	var receipt supplierorder.SupplierOrderCommandReceipt
	require.NoError(t, json.Unmarshal(accepted.Body.Bytes(), &receipt))
	require.Equal(t, "accepted", receipt.Order.Status)
	require.Equal(t, operator.principal.Subject, receipt.ActorSubject)
	require.Equal(t, operator.input.Identity.StaffID, receipt.ActorStaffID)
	for _, method := range []string{"PUT", "GET"} {
		var replayInput any
		if method == "PUT" {
			replayInput = accept
		}
		replay := f.sourceStaffRequest(t, operator.principal, method, commandPath, replayInput)
		require.Equal(t, 200, replay.Code, replay.Body.String())
		require.JSONEq(t, accepted.Body.String(), replay.Body.String())
	}
	changed := accept
	changed.Reason = "TEST different command content"
	conflict := f.sourceStaffRequest(t, operator.principal, "PUT", commandPath, changed)
	require.Equal(t, 409, conflict.Code, conflict.Body.String())
	require.Eventually(t, func() bool {
		current := f.hospitalCall(t, "GET", base, preparer, nil, 200)
		return current["status"] == "accepted"
	}, 20*time.Second, 100*time.Millisecond)
}
