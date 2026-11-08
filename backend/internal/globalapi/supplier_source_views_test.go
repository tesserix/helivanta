package globalapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// Retained business projections are fixture data; Staff identity, reviewed duties,
// OpenFGA acknowledgement, authorization and the owner HTTP handlers are real.
func TestSourceViewsUseCurrentReviewedStaffAndRetainReconciliation(t *testing.T) {
	fixture := newOperatingDutyFixture(t)
	fixture.projection.enabled.Store(true)
	activate := func(template string) string {
		t.Helper()
		input := fixture.input
		input.TemplateKey = template
		path := "/v1/iam/operating-duties/" + uuid.Must(uuid.NewV7()).String()
		fixture.call("PUT", path, "owner", input, 201)
		fixture.call("PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST reviewed supplier source work"}, 201)
		require.Eventually(t, func() bool {
			return strings.Contains(fixture.call("GET", path, "owner", nil, 200), `"status":"active"`)
		}, 20*time.Second, 50*time.Millisecond)
		return path
	}
	root := fmt.Sprintf("/v1/supply/stock-owners/supply/organizations/%s/locations/%s/supplier-orders", fixture.input.Scope.EmployerID, fixture.input.Scope.LocationID)
	fixture.call("GET", root, "receiver", nil, 403)
	fulfiller := activate("supply.stock_fulfiller.v1")
	data := supplierStockFixture{organization: fixture.input.Scope.EmployerID, location: fixture.input.Scope.LocationID, product: supplierorder.SupplierOrderProduct{Owner: "supply", ProductID: uuid.New(), VersionID: ptr(uuid.New())}}
	order, _, _ := sourceOrder(t, "supply", data)
	order.SignedBy = ptr("user-receiver")
	order.SignedStaffID = &fixture.input.Identity.StaffID
	order.CreatedAt = time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	order.UpdatedAt = order.CreatedAt.Add(time.Second)
	secondInput := order.Input
	secondInput.PurchaseOrderReference = "TEST retained PO two"
	relationship := suppliertrade.Relationship{ID: order.Input.RelationshipID, OwnerTenantID: order.Terms.Buyer.TenantID, Side: "buyer", Terms: order.Terms, TermsSha256: order.Input.TermsSha256, Status: "active", Version: 3, AuthorityEpoch: 2, RemoteEpoch: 2, SignedBy: ptr("TEST hospital reviewer"), RemoteSignedBy: ptr("TEST source reviewer"), CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt}
	second, err := supplierorder.Prepare(uuid.Must(uuid.NewV7()), relationship, secondInput, supplierorder.Actor{Subject: "TEST hospital maker", StaffID: uuid.New()}, order.CreatedAt.Add(time.Second))
	require.NoError(t, err)
	_, offered, err := supplierorder.Decide(second, supplierorder.SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST retained hospital approval"}, supplierorder.Actor{Subject: "TEST hospital reviewer", StaffID: uuid.New()}, second.CreatedAt.Add(time.Second))
	require.NoError(t, err)
	second, err = supplierorder.Apply(nil, *offered, order.OwnerTenantID, second.CreatedAt.Add(time.Second))
	require.NoError(t, err)
	second, _, err = supplierorder.Decide(second, supplierorder.SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST retained source approval"}, supplierorder.Actor{Subject: "user-receiver", StaffID: fixture.input.Identity.StaffID}, second.CreatedAt.Add(time.Second))
	require.NoError(t, err)
	command := supplierorder.SupplierOrderCommandReceipt{CommandID: uuid.Must(uuid.NewV7()), Input: supplierorder.SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST retained source acceptance"}, Order: order, ActorSubject: "user-receiver", ActorStaffID: fixture.input.Identity.StaffID, RecordedAt: order.UpdatedAt}
	shipment := f.Shipment{ID: uuid.Must(uuid.NewV7()), AllocationID: uuid.Must(uuid.NewV7()), OrderID: order.ID, LineID: order.Input.Lines[0].ID, OrderInputSha256: order.InputSha256, SourceTenantID: order.OwnerTenantID, SourceOrganizationID: data.organization, SourceLocationID: data.location, BuyerTenantID: order.Terms.Buyer.TenantID, HospitalID: *order.Terms.Buyer.HospitalID, BuyerOrganizationID: order.Terms.Buyer.OrganizationID, BuyerLocationID: order.Terms.Buyer.LocationID, Product: data.product, UnitCode: "capsule", QuantityUnits: 10, LotNumber: "TEST-RETAINED-LOT", ExpiresOn: "2030-12-31", HandoffReference: "TEST retained delivery", ActorSubject: "user-receiver", ProductDescription: "TEST retained source product", PackSize: 1, DispatchedAt: order.UpdatedAt.Add(time.Second)}
	summary := f.ReceivingSummary{ID: uuid.Must(uuid.NewV7()), Reason: "TEST hospital receiving closure", OwnerTenantID: order.Terms.Buyer.TenantID, OrderID: order.ID, ShipmentID: shipment.ID, Version: 1, CountedUnits: 9, AcceptedUnits: 8, RejectedUnits: 1, ShortUnits: 1, Closed: true, ActorSubject: "TEST hospital reviewer", RecordedAt: shipment.DispatchedAt.Add(time.Second)}
	require.NoError(t, fixture.db.WithTenantCommand(tenantdb.WithActor(t.Context(), "user-receiver"), testutil.TenantA, func(tx *gorm.DB) error {
		relation := suppliertrade.Relationship{ID: order.Input.RelationshipID, OwnerTenantID: order.OwnerTenantID, Side: "seller", Terms: order.Terms, TermsSha256: order.Input.TermsSha256, Status: "active", Version: 3, AuthorityEpoch: 2, RemoteEpoch: 2, SignedBy: ptr("TEST source reviewer"), RemoteSignedBy: ptr("TEST hospital reviewer"), CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt}
		raw, err := json.Marshal(relation)
		if err != nil {
			return err
		}
		terms, err := json.Marshal(order.Terms)
		if err != nil {
			return err
		}
		if err = tx.Exec(`INSERT INTO supply_trade_relationships(tenant_id,origin_tenant_id,origin_record_id,id,body,proposal_input,proposal_receipt,version,created_at,updated_at)VALUES(?,?,?,?,?::jsonb,?::jsonb,?::jsonb,?,?,?)`, order.OwnerTenantID, order.OwnerTenantID, relation.ID, relation.ID, string(raw), string(terms), string(raw), relation.Version, relation.CreatedAt, relation.UpdatedAt).Error; err != nil {
			return err
		}
		if err = tx.Exec(`SELECT set_config('app.supply_order_mode','seller_write',true),set_config('app.supply_order_product','supply',true),set_config('app.supply_order_organization',?,true),set_config('app.supply_order_location',?,true),set_config('app.supply_order_actor_staff',?,true)`, data.organization.String(), data.location.String(), fixture.input.Identity.StaffID.String()).Error; err != nil {
			return err
		}
		for _, record := range []supplierorder.SupplierOrder{order, second} {
			raw, err = json.Marshal(record)
			if err != nil {
				return err
			}
			if err = tx.Exec(`INSERT INTO supply_purchase_orders(tenant_id,id,origin_tenant_id,origin_record_id,side,hospital_id,buyer_outlet_id,source_product,source_organization_id,source_location_id,relationship_id,version,body,actor_subject,actor_staff_id,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?,?,?,?)`, record.OwnerTenantID, record.ID, record.Terms.Buyer.TenantID, record.ID, "seller", *record.Terms.Buyer.HospitalID, record.Terms.Buyer.LocationID, "supply", data.organization, data.location, record.Input.RelationshipID, record.Version, string(raw), "user-receiver", fixture.input.Identity.StaffID, record.CreatedAt, record.UpdatedAt).Error; err != nil {
				return err
			}
		}
		raw, err = json.Marshal(command)
		if err != nil {
			return err
		}
		input, err := json.Marshal(command.Input)
		if err != nil {
			return err
		}
		if err = tx.Exec(`INSERT INTO supply_purchase_order_commands(tenant_id,id,order_id,input,receipt,actor_subject,actor_staff_id,created_at)VALUES(?,?,?,?::jsonb,?::jsonb,?,?,?)`, order.OwnerTenantID, command.CommandID, order.ID, string(input), string(raw), command.ActorSubject, command.ActorStaffID, command.RecordedAt).Error; err != nil {
			return err
		}
		if err = tx.Exec(`SELECT set_config('app.supply_order_mode','exchange',true),set_config('app.supply_order_id',?,true),set_config('app.supply_order_origin',?,true),set_config('app.actor_subject','system:supplier-shipments',true)`, order.ID.String(), order.Terms.Buyer.TenantID.String()).Error; err != nil {
			return err
		}
		raw, err = json.Marshal(shipment)
		if err != nil {
			return err
		}
		if err = tx.Exec(`INSERT INTO supply_supplier_dispatched_shipments(tenant_id,id,order_id,buyer_tenant_id,body,created_at)VALUES(?,?,?,?,?::jsonb,?)`, order.OwnerTenantID, shipment.ID, order.ID, shipment.BuyerTenantID, string(raw), shipment.DispatchedAt).Error; err != nil {
			return err
		}
		if err = tx.Exec(`SELECT set_config('app.actor_subject','system:supplier-receiving',true)`).Error; err != nil {
			return err
		}
		raw, err = json.Marshal(summary)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO supply_supplier_receiving_updates(tenant_id,shipment_id,version,order_id,source_product,source_organization_id,source_location_id,buyer_tenant_id,body,source_event_id,actor_subject,created_at)VALUES(?,?,?,?,?,?,?,?,?::jsonb,?,'system:supplier-receiving',?)`, order.OwnerTenantID, shipment.ID, summary.Version, order.ID, "supply", data.organization, data.location, summary.OwnerTenantID, string(raw), uuid.Must(uuid.NewV7()), summary.RecordedAt).Error
	}))
	var first struct {
		Data []supplierorder.SupplierOrder
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		}
	}
	require.NoError(t, json.Unmarshal([]byte(fixture.call("GET", root+"?limit=1", "receiver", nil, 200)), &first))
	require.Len(t, first.Data, 1)
	require.True(t, first.Page.HasMore)
	require.NotNil(t, first.Page.NextCursor)
	var next struct{ Data []supplierorder.SupplierOrder }
	require.NoError(t, json.Unmarshal([]byte(fixture.call("GET", root+"?limit=1&cursor="+url.QueryEscape(*first.Page.NextCursor), "receiver", nil, 200)), &next))
	require.Len(t, next.Data, 1)
	require.NotEqual(t, first.Data[0].ID, next.Data[0].ID)
	fixture.call("GET", root+"?cursor="+url.QueryEscape(*first.Page.NextCursor), "owner", nil, 400)
	path := root + "/" + order.ID.String()
	require.Contains(t, fixture.call("GET", path, "receiver", nil, 200), order.Input.PurchaseOrderReference)
	require.Contains(t, fixture.call("GET", "/v1/supply/supplier-orders/"+order.ID.String(), "receiver", nil, 200), order.ID.String())
	require.Contains(t, fixture.call("GET", path+"/commands/"+command.CommandID.String(), "receiver", nil, 200), command.Input.Reason)
	require.Contains(t, fixture.call("GET", path+"/shipments?limit=1", "receiver", nil, 200), shipment.HandoffReference)
	receiving := path + "/shipments/" + shipment.ID.String() + "/receiving"
	require.Contains(t, fixture.call("GET", receiving, "receiver", nil, 200), `"short_units":1`)
	responsePath := receiving + "/responses/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"expected_summary_version": 1, "response": "disputed", "reason": "TEST check the hospital shortage evidence"}
	firstResponse := fixture.call("PUT", responsePath, "receiver", input, 200)
	require.JSONEq(t, firstResponse, fixture.call("PUT", responsePath, "receiver", input, 200))
	require.Contains(t, fixture.call("GET", receiving+"/responses?limit=1", "receiver", nil, 200), "TEST check the hospital shortage evidence")
	require.NoError(t, fixture.db.WithTenantCommand(tenantdb.WithActor(t.Context(), "TEST source outbox inspector"), testutil.TenantA, func(tx *gorm.DB) error {
		var rows []struct{ Payload json.RawMessage }
		if err := tx.Table("outbox_events").Select("payload").Where("tenant_id=? AND subject=?", testutil.TenantA, "helivanta.in.supply.supplier_receiving_response_exchange.v1").Scan(&rows).Error; err != nil {
			return err
		}
		require.Len(t, rows, 1, "response and durable publication commit once, including replay")
		var envelope events.Event
		require.NoError(t, json.Unmarshal(rows[0].Payload, &envelope))
		require.Equal(t, summary.OwnerTenantID.String(), envelope.DestinationTenantID)
		var published f.ReceivingResponseRecord
		require.NoError(t, json.Unmarshal(envelope.Data, &published))
		require.Equal(t, summary.Version, published.SummaryVersion)
		require.Equal(t, fixture.input.Identity.StaffID, published.ActorStaffID)
		return nil
	}))

	fixture.call("PUT", strings.Replace(responsePath, order.ID.String(), second.ID.String(), 1), "receiver", input, 404)
	input["reason"] = "TEST changed reconciliation intent"
	fixture.call("PUT", responsePath, "receiver", input, 409)
	fixture.call("GET", path+"/review-access", "receiver", nil, 403)
	acceptor := activate("supply.order_acceptor.v1")
	var access supplierorder.SupplierOrderReviewAccess
	require.NoError(t, json.Unmarshal([]byte(fixture.call("GET", path+"/review-access", "receiver", nil, 200)), &access))
	require.Equal(t, fixture.input.Identity.StaffID, access.ActorStaffID)
	require.Equal(t, "user-receiver", access.ActorSubject)
	wrong := strings.Replace(root, data.location.String(), uuid.Must(uuid.NewV7()).String(), 1)
	fixture.call("GET", wrong, "receiver", nil, 403)
	fixture.checks.down.Store(true)
	fixture.call("GET", root, "receiver", nil, 503)
	fixture.checks.down.Store(false)
	for _, duty := range []string{fulfiller, acceptor} {
		fixture.call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner", iam.OperatingDutyDecisionInput{ExpectedVersion: 3, Decision: "revoke", Reason: "TEST immediate source cutoff"}, 201)
	}
	fixture.call("GET", root, "receiver", nil, 403)
	fixture.call("GET", path+"/commands/"+command.CommandID.String(), "receiver", nil, 403)
}
