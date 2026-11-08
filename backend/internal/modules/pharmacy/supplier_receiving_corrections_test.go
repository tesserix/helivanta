package pharmacy_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"                           //nolint:depguard // external test of the owning module
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // owner event contract under test
	"github.com/tesserix/helivanta/internal/platform"

	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestSupplierReceivingCorrectionRepositoryRetainsEvidenceAndRollsBackCustodyFailures(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"receiver": testutil.TenantA}, Perms: map[string][]authz.Permission{"receiver": {pharmacy.PermCatalogueRead, pharmacy.PermCatalogueWrite, pharmacy.PermAccessManage, pharmacy.PermStockRead, pharmacy.PermStockReceive}}, Modules: []platform.Module{pharmacy.New()}})
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	tenant := uuid.MustParse(testutil.TenantA)
	org, outlet, product, q := id(), id(), id(), id()
	root := "/v1/pharmacy/pharmacies/" + org.String()
	call := func(path, body string) {
		t.Helper()
		w := testutil.Do(r, "PUT", path, "receiver", body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	call(root, `{"code":"TEST-CORRECTIONS","name":"TEST hospital pharmacy","ownership_type":"hospital_owned"}`)
	call(root+"/outlets/"+outlet.String(), `{"code":"TEST-MAIN","name":"TEST Main","country_code":"IN","timezone":"Asia/Kolkata"}`)
	root += "/outlets/" + outlet.String()
	call(root+"/access/user-receiver", `{"level":"editor"}`)
	call(root+"/catalogue/products/"+product.String(), `{"expected_version":0,"sku":"TEST-CAPSULE","brand_name":"TEST capsule","generic_name":"TEST molecule","dosage_form":"capsule","route":"oral","manufacturer":"TEST Manufacturer","marketing_company":"TEST Marketing","ingredients":[{"name":"TEST molecule","strength":"500","unit":"mg"}],"pack_size":10,"unit_of_issue":"capsule","currency":"INR","mrp_minor":12500,"status":"active"}`)
	call(root+"/stock/locations/"+q.String(), `{"code":"TEST-Q","name":"TEST quarantine"}`)
	shipment := f.Shipment{ID: id(), AllocationID: id(), OrderID: id(), LineID: id(), OrderInputSha256: strings.Repeat("a", 64), SourceTenantID: uuid.MustParse(testutil.TenantB), SourceOrganizationID: id(), SourceLocationID: id(), BuyerTenantID: tenant, HospitalID: uuid.New(), BuyerOrganizationID: org, BuyerLocationID: outlet, Product: supplierorder.SupplierOrderProduct{Owner: "pharmacy", ProductID: id(), Version: correctionPtr(int64(1))}, UnitCode: "capsule", QuantityUnits: 50, LotNumber: "TEST-LOT", ExpiresOn: "2030-12-31", HandoffReference: "TEST-CHALLAN", ActorSubject: "TEST supplier", ProductDescription: "TEST capsule", PackSize: 10, DispatchedAt: time.Now().UTC().Add(-time.Minute)}
	scope := f.BuyerScope{TenantID: tenant, HospitalID: shipment.HospitalID, DepartmentID: uuid.New(), OutletID: outlet, ShipmentID: shipment.ID}
	receiver := supplierorder.Actor{Subject: "user-receiver", StaffID: uuid.New()}
	reviewer := supplierorder.Actor{Subject: "TEST-independent-reviewer", StaffID: uuid.New()}
	other := supplierorder.Actor{Subject: "TEST-second-reviewer", StaffID: uuid.New()}
	// Repository tests start after current authority admission; actual owner policies and stock triggers remain enabled.
	command := func(actor supplierorder.Actor, purpose string, action func(*gorm.DB) error) error {
		ctx := tenantdb.WithActor(t.Context(), actor.Subject)
		return db.WithTenant(ctx, tenant.String(), func(tx *gorm.DB) error {
			values := map[string]string{"app.pharmacy_subject": actor.Subject, "app.pharmacy_receiving_access": "authorized", "app.pharmacy_receiving_outlet": outlet.String(), "app.pharmacy_receiving_pharmacy": org.String(), "app.pharmacy_receiving_hospital": scope.HospitalID.String(), "app.pharmacy_receiving_department": scope.DepartmentID.String(), "app.pharmacy_receiving_staff": actor.StaffID.String(), "app.pharmacy_receiving_purpose": purpose}
			raw, err := json.Marshal(values)
			if err != nil {
				return err
			}
			if err = tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, string(raw)).Error; err != nil {
				return err
			}
			if err = pharmacy.TestReceivingStockLock(tx, tenant.String()+":supplier-receiving:"+shipment.ID.String()); err != nil {
				return err
			}
			return action(tx)
		})
	}
	var original f.ReceivingReceipt
	require.NoError(t, command(receiver, "record", func(tx *gorm.DB) error {
		at, err := supplierstock.Clock(tx)
		if err != nil {
			return err
		}
		input := f.ReceivingInput{DestinationProductID: &product, DestinationProductVersion: correctionPtr(int64(1)), DestinationBatchID: correctionPtr(id()), QuarantineLocationID: &q, CountedUnits: 30, AcceptedUnits: 20, RejectedUnits: 10, MrpMinor: correctionPtr(int64(12500)), ObservedLotNumber: shipment.LotNumber, ObservedExpiresOn: shipment.ExpiresOn, IdentityConfirmed: true, Reason: "TEST count"}
		original, err = f.Receive(id(), shipment, input, f.ReceivingTotals{}, receiver, at)
		if err != nil {
			return err
		}
		summary := f.ReceivingSummary{ID: original.ID, ShipmentID: shipment.ID, OrderID: shipment.OrderID, OwnerTenantID: tenant, Version: 1, CountedUnits: 30, AcceptedUnits: 20, RejectedUnits: 10, Reason: input.Reason, ActorSubject: receiver.Subject, RecordedAt: at}
		if err = pharmacy.TestSaveReceivingHead(tx, scope, shipment, summary); err != nil {
			return err
		}
		body, err := json.Marshal(original)
		if err != nil {
			return err
		}
		hash, err := f.IntentHash(receiver, input)
		if err != nil {
			return err
		}
		if err = tx.Exec(`INSERT INTO pharmacy_supplier_receipts(tenant_id,id,shipment_id,pharmacy_id,outlet_id,destination_batch_id,quarantine_location_id,counted_units,accepted_units,rejected_units,intent_hash,body,actor_subject,actor_staff_id,created_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?,?,?)`, tenant, original.ID, shipment.ID, org, outlet, input.DestinationBatchID, q, 30, 20, 10, hash, string(body), receiver.Subject, receiver.StaffID, at).Error; err != nil {
			return err
		}
		return pharmacy.PostSupplierQuarantineForTest(tx, authn.Principal{TenantID: tenant.String(), Subject: receiver.Subject}, shipment, original, 1)
	}))
	closeReceiving := func(version int64) error {
		return command(receiver, "record", func(tx *gorm.DB) error {
			head, err := pharmacy.TestReadReceivingHead(tx, tenant, shipment.ID)
			if err != nil {
				return err
			}
			require.Equal(t, version, head.Version)
			at, err := supplierstock.Clock(tx)
			if err != nil {
				return err
			}
			input := f.ReceivingClosureInput{ExpectedCountedUnits: head.CountedUnits, ExpectedSummaryVersion: &version, Reason: "TEST explicit shortage closure"}
			closure, err := f.CloseReceiving(id(), shipment, f.ReceivingTotals{Counted: head.CountedUnits, Accepted: head.AcceptedUnits, Rejected: head.RejectedUnits}, input, receiver, at)
			if err != nil {
				return err
			}
			summary := f.ReceivingSummary{ID: closure.ID, ShipmentID: shipment.ID, OrderID: shipment.OrderID, OwnerTenantID: tenant, Version: version + 1, CountedUnits: head.CountedUnits, AcceptedUnits: head.AcceptedUnits, RejectedUnits: head.RejectedUnits, ShortUnits: closure.ShortUnits, Closed: true, Reason: closure.Reason, ActorSubject: receiver.Subject, RecordedAt: at}
			if err = pharmacy.TestSaveReceivingHead(tx, scope, shipment, summary); err != nil {
				return err
			}
			raw, err := json.Marshal(closure)
			if err != nil {
				return err
			}
			intent, err := json.Marshal(input)
			if err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO pharmacy_supplier_receiving_closures(tenant_id,id,shipment_id,outlet_id,summary_version,input,body,actor_subject,actor_staff_id,created_at)VALUES(?,?,?,?,?,?::jsonb,?::jsonb,?,?,?)`, tenant, closure.ID, shipment.ID, outlet, summary.Version, string(intent), string(raw), receiver.Subject, receiver.StaffID, at).Error
		})
	}
	require.NoError(t, closeReceiving(1))
	correctionID := id()
	input := f.ReceivingCorrectionInput{ExpectedSummaryVersion: 2, ExpectedReceiptVersion: 1, CountedUnits: 28, AcceptedUnits: 18, RejectedUnits: 10, Reason: "TEST reviewed recount"}
	var corrected f.ReceivingCorrection
	apply := func(actor supplierorder.Actor, request uuid.UUID, in f.ReceivingCorrectionInput, after error) error {
		return command(actor, "review", func(tx *gorm.DB) error {
			var err error
			corrected, err = pharmacy.ApplySupplierCorrectionForTest(tx, scope, shipment, original.ID, request, in, actor)
			if err != nil {
				return err
			}
			return after
		})
	}
	require.ErrorIs(t, apply(receiver, correctionID, input, nil), f.ErrConflict)
	rollback := errors.New("TEST rollback")
	require.ErrorIs(t, apply(reviewer, correctionID, input, rollback), rollback)
	require.NoError(t, apply(reviewer, correctionID, input, nil))
	require.Equal(t, int64(-2), corrected.AcceptedDeltaUnits)
	require.NoError(t, apply(reviewer, correctionID, input, nil))
	changed := input
	changed.Reason = "TEST different intent"
	require.ErrorIs(t, apply(reviewer, correctionID, changed, nil), f.ErrConflict)
	require.NoError(t, command(reviewer, "review", func(tx *gorm.DB) error {
		head, err := pharmacy.TestReadReceivingHead(tx, tenant, shipment.ID)
		if err != nil {
			return err
		}
		require.Equal(t, int64(3), head.Version)
		require.Equal(t, int64(18), head.AcceptedUnits)
		require.False(t, head.Closed)
		var body struct{ Body json.RawMessage }
		if err = tx.Table("pharmacy_supplier_receipts").Select("body").Where("tenant_id=? AND id=?", tenant, original.ID).Take(&body).Error; err != nil {
			return err
		}
		expected, err := json.Marshal(original)
		if err != nil {
			return err
		}
		require.JSONEq(t, string(expected), string(body.Body))
		var quantity int64
		if err = tx.Table("pharmacy_stock_movements").Select("COALESCE(SUM(quantity_units),0)").Where("tenant_id=? AND outlet_id=? AND batch_id=? AND location_id=?", tenant, outlet, *original.Input.DestinationBatchID, q).Scan(&quantity).Error; err != nil {
			return err
		}
		require.Equal(t, int64(18), quantity)
		return nil
	}))
	require.NoError(t, closeReceiving(3))
	call(root+"/stock/movements/"+id().String(), fmt.Sprintf(`{"location_id":%q,"batch_id":%q,"kind":"correction","quantity_units":-1,"reason":"TEST physical adjustment","source_reference":"TEST-adjustment"}`, q, *original.Input.DestinationBatchID))
	input.ExpectedSummaryVersion = 4
	input.ExpectedReceiptVersion = 2
	input.CountedUnits = 27
	input.AcceptedUnits = 17
	require.Error(t, apply(other, id(), input, nil), "changed quarantine custody must roll back the summary, correction and outbox")
	require.NoError(t, command(reviewer, "review", func(tx *gorm.DB) error {
		head, err := pharmacy.TestReadReceivingHead(tx, tenant, shipment.ID)
		require.Equal(t, int64(4), head.Version)
		if err != nil {
			return err
		}
		var audits int64
		if err = tx.Table("audit_events").Where("tenant_id=? AND resource_type='pharmacy_supplier_receiving_heads'AND resource_id=?", tenant, shipment.ID).Count(&audits).Error; err != nil {
			return err
		}
		require.Equal(t, int64(4), audits, "only committed receiving state changes create audit evidence")
		var publications int64
		if err = tx.Table("outbox_events").Where("tenant_id=? AND subject=? AND payload#>>'{data,summary,id}'=?", tenant, pharmacycontract.SubjectSupplierReceivingRecorded, correctionID.String()).Count(&publications).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), publications, "rollback and replay must not duplicate correction publication")
		return nil
	}))
	require.NoError(t, command(reviewer, "review", func(tx *gorm.DB) error {
		receipt, err := pharmacy.ReadSupplierReceiptForTest(tx, tenant, shipment.ID, original.ID, reviewer, nil)
		if err != nil {
			return err
		}
		require.Equal(t, original.ID, receipt.ID)
		_, err = pharmacy.ReadSupplierReceiptForTest(tx, tenant, shipment.ID, original.ID, reviewer, &original.Input)
		require.ErrorIs(t, err, f.ErrConflict, "command recovery must remain bound to its original actor")
		return nil
	}))
}

func correctionPtr[T any](v T) *T { return &v }
