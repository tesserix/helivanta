package globalapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type pharmacyReceiverFixture struct {
	operatingDutyFixture
	pharmacy, outlet, sibling, product, quarantine, shelf, staff string
	hospitalPharmacy, hospitalOutlet                             string
}

const receiverProduct = `{"expected_version":0,"sku":"TEST-AMOX-500","brand_name":"TEST Amoxicillin","generic_name":"TEST amoxicillin","dosage_form":"capsule","route":"oral","manufacturer":"TEST Manufacturer","marketing_company":"TEST Marketing","ingredients":[{"name":"TEST amoxicillin","strength":"500","unit":"mg"}],"pack_size":10,"unit_of_issue":"capsule","currency":"INR","mrp_minor":12500,"status":"active"}`

func v7() string { return uuid.Must(uuid.NewV7()).String() }

// newPharmacyReceiverFixture prepares one independent outlet, a reviewed pharmacist account and an unapproved receiver duty.
func newPharmacyReceiverFixture(t *testing.T) pharmacyReceiverFixture {
	t.Helper()
	f := pharmacyReceiverFixture{operatingDutyFixture: newOperatingDutyFixtureWithOwners(t, nil, pharmacy.New())}
	call := f.call
	f.pharmacy, f.outlet, f.sibling, f.product, f.quarantine, f.shelf = v7(), v7(), v7(), v7(), v7(), v7()
	root := "/v1/pharmacy/pharmacies/" + f.pharmacy
	call("PUT", root, "owner", map[string]any{"code": "TEST-RX-PHARMACY", "name": "TEST Independent Pharmacy", "ownership_type": "independent"}, 200)
	for code, outlet := range map[string]string{"TEST-RX-MAIN": f.outlet, "TEST-RX-SIBLING": f.sibling} {
		call("PUT", root+"/outlets/"+outlet, "owner", map[string]any{"code": code, "name": "TEST " + code, "country_code": "IN", "timezone": "Asia/Kolkata"}, 200)
		call("PUT", root+"/outlets/"+outlet+"/access/user-owner", "owner", map[string]any{"level": "editor"}, 200)
	}
	call("PUT", root+"/outlets/"+f.outlet+"/catalogue/products/"+f.product, "owner", json.RawMessage(receiverProduct), 200)
	call("PUT", root+"/outlets/"+f.outlet+"/stock/locations/"+f.quarantine, "owner", map[string]any{"code": "TEST-Q", "name": "TEST Quarantine"}, 200)
	call("PUT", root+"/outlets/"+f.outlet+"/stock/locations/"+f.shelf, "owner", map[string]any{"code": "TEST-S", "name": "TEST Shelf", "stock_status": "sellable"}, 200)
	f.hospitalPharmacy, f.hospitalOutlet = v7(), v7()
	hospital := "/v1/pharmacy/pharmacies/" + f.hospitalPharmacy
	call("PUT", hospital, "owner", map[string]any{"code": "TEST-RX-HOSPITAL", "name": "TEST Hospital Pharmacy", "ownership_type": "hospital_owned"}, 200)
	call("PUT", hospital+"/outlets/"+f.hospitalOutlet, "owner", map[string]any{"code": "TEST-RX-WARD", "name": "TEST Ward Pharmacy", "country_code": "IN", "timezone": "Asia/Kolkata"}, 200)

	employer := "/v1/workforce/pharmacies/" + f.pharmacy
	call("PUT", employer, "owner", map[string]any{}, 200)
	call("PUT", employer+"/access/user-owner", "owner", map[string]any{"level": "manager"}, 200)
	call("PUT", employer+"/access/user-reviewer", "owner", map[string]any{"level": "manager"}, 200)
	f.staff = v7()
	link := v7()
	staffPath := employer + "/staff/" + f.staff
	call("PUT", staffPath+"?directory_context=v1", "owner", map[string]any{"employee_code": "TEST-PH-RX", "display_name": "TEST Outlet Receiver", "job_category": "pharmacist", "employment_status": "active", "expected_version": 0, "directory_context_version": "v1"}, 200)
	linkPath := staffPath + "/account-links/" + link
	call("PUT", linkPath, "owner", map[string]any{"expected_staff_version": 1, "subject": "user-receiver", "reason": "TEST verified account ownership"}, 201)
	call("PUT", linkPath+"/commands/"+v7(), "reviewer", map[string]any{"expected_version": 1, "decision": "approve", "reason": "TEST independently reviewed identity"}, 201)

	now := time.Now().UTC().Truncate(time.Second)
	f.input = iam.OperatingDutyInput{Scope: iam.OperatingDutyScope{EmployerKind: "pharmacy", EmployerID: uuid.MustParse(f.pharmacy), LocationID: uuid.MustParse(f.outlet), ExpectedEmployerVersion: 1, ExpectedLocationVersion: 1}, Identity: iam.OperatingDutyIdentity{StaffID: uuid.MustParse(f.staff), StaffVersion: 1, LinkID: uuid.MustParse(link), LinkVersion: 2}, Subject: "user-receiver", TemplateKey: "pharmacy.inventory_receiver.v1", Reason: "TEST scoped outlet receiving duty", ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	return f
}

func (f pharmacyReceiverFixture) activate(t *testing.T) {
	t.Helper()
	f.projection.enabled.Store(true)
	f.call("PUT", f.duty, "owner", f.input, 201)
	f.call("PUT", f.duty+"/commands/"+v7(), "reviewer", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST accountable outlet intake"}, 201)
	require.Eventually(t, func() bool { return strings.Contains(f.call("GET", f.duty, "owner", nil, 200), `"status":"active"`) }, 20*time.Second, 100*time.Millisecond)
}

func (f pharmacyReceiverFixture) stock(pharmacy, outlet, suffix string) string {
	return "/v1/pharmacy/pharmacies/" + pharmacy + "/operating-locations/" + outlet + "/stock" + suffix
}

func (f pharmacyReceiverFixture) batch(lot string) map[string]any {
	return map[string]any{"product_id": f.product, "lot_number": lot, "expires_on": "2030-10-31", "mrp_minor": 12500}
}

func (f pharmacyReceiverFixture) receipt(batch, location, source string) map[string]any {
	return map[string]any{"location_id": location, "batch_id": batch, "kind": "receipt", "quantity_units": 30, "reason": "TEST counted supplier delivery", "source_reference": source}
}

// ownerOutletTx reads as the outlet editor, so the duty settings play no part in what the assertion sees.
func (f pharmacyReceiverFixture) ownerOutletTx(t *testing.T, fn func(*gorm.DB) error) {
	t.Helper()
	require.NoError(t, f.db.WithTenant(tenantdb.WithActor(f.ctx, "user-owner"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_id',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_subject','user-owner',true)`, f.pharmacy, f.outlet).Error; err != nil {
			return err
		}
		return fn(tx)
	}))
}

func (f pharmacyReceiverFixture) audits(t *testing.T) []string {
	t.Helper()
	var rows []string
	f.ownerOutletTx(t, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT action||' '||resource_kind||' '||COALESCE(resource_id::text,'-')||' '||permission||' '||actor_subject||' '||(duty_id::text=?)::text||' '||(staff_id::text=?)::text FROM pharmacy_operating_stock_audit WHERE tenant_id=? AND outlet_id=? ORDER BY created_at,id`, strings.TrimPrefix(f.duty, "/v1/iam/operating-duties/"), f.staff, testutil.TenantA, f.outlet).Scan(&rows).Error
	})
	return rows
}

func (f pharmacyReceiverFixture) movementEvents(t *testing.T, movement string) int64 {
	t.Helper()
	var count int64
	f.ownerOutletTx(t, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject=? AND payload->'data'->>'movement_id'=?`, pharmacycontract.SubjectStockMovementRecorded, movement).Scan(&count).Error
	})
	return count
}

func TestIndependentPharmacyReceiverRecordsQuarantinedReceiptUnderReviewedDuty(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	call := f.call
	batch, movement := v7(), v7()
	batchPath, receiptPath := f.stock(f.pharmacy, f.outlet, "/batches/"+batch), f.stock(f.pharmacy, f.outlet, "/receipts/"+movement)
	call("PUT", batchPath, "receiver", f.batch("TEST-RX-LOT"), 404)
	f.activate(t)

	recorded := call("PUT", batchPath, "receiver", f.batch("TEST-RX-LOT"), 200)
	require.Contains(t, recorded, `"lot_number":"TEST-RX-LOT"`)
	require.Contains(t, recorded, `"product_version":1`)
	require.JSONEq(t, recorded, call("PUT", batchPath, "receiver", f.batch("test-rx-lot "), 200), "an identical replay answers the stored batch")
	require.Contains(t, call("PUT", batchPath, "receiver", f.batch("TEST-RX-OTHER"), 409), "already exists with different details")
	require.JSONEq(t, recorded, call("GET", batchPath, "receiver", nil, 200))

	received := call("PUT", receiptPath, "receiver", f.receipt(batch, f.quarantine, "TEST-GRN-1"), 200)
	require.Contains(t, received, `"actor_subject":"user-receiver"`)
	require.Contains(t, received, `"kind":"receipt"`)
	require.Contains(t, received, `"quantity_units":30`)
	require.JSONEq(t, received, call("PUT", receiptPath, "receiver", f.receipt(batch, f.quarantine, "TEST-GRN-1"), 200))
	require.Contains(t, call("PUT", receiptPath, "receiver", f.receipt(batch, f.quarantine, "TEST-GRN-2"), 409), "already exists with different details")
	require.EqualValues(t, 1, f.movementEvents(t, movement), "a replay publishes nothing new")
	require.JSONEq(t, received, call("GET", receiptPath, "receiver", nil, 200))
	require.Contains(t, call("GET", f.stock(f.pharmacy, f.outlet, "/receipts"), "receiver", nil, 200), `"id":"`+movement+`"`)
	require.Contains(t, call("GET", f.stock(f.pharmacy, f.outlet, "/batches"), "receiver", nil, 200), `"id":"`+batch+`"`)
	locations := call("GET", f.stock(f.pharmacy, f.outlet, "/locations"), "receiver", nil, 200)
	require.Contains(t, locations, `"id":"`+f.quarantine+`"`)
	require.Contains(t, locations, `"id":"`+f.shelf+`"`)
	products := call("GET", f.stock(f.pharmacy, f.outlet, "/products"), "receiver", nil, 200)
	require.Contains(t, products, `"id":"`+f.product+`"`)
	require.NotContains(t, products, "mrp_minor", "a receiver names the product, not its commercial terms")
	require.NotContains(t, call("GET", f.stock(f.pharmacy, f.outlet, "/batches?product_id="+v7()), "receiver", nil, 200), batch)

	tail := " user-receiver true true"
	require.Equal(t, []string{
		"pharmacy.operating_stock.batch_recorded batch " + batch + " pharmacy.stock.receive" + tail,
		"pharmacy.operating_stock.read batch " + batch + " pharmacy.stock.read" + tail,
		"pharmacy.operating_stock.receipt_recorded receipt " + movement + " pharmacy.stock.receive" + tail,
		"pharmacy.operating_stock.read receipt " + movement + " pharmacy.stock.read" + tail,
		"pharmacy.operating_stock.read receipt - pharmacy.stock.read" + tail,
		"pharmacy.operating_stock.read batch - pharmacy.stock.read" + tail,
		"pharmacy.operating_stock.read location - pharmacy.stock.read" + tail,
		"pharmacy.operating_stock.read product - pharmacy.stock.read" + tail,
		"pharmacy.operating_stock.read batch - pharmacy.stock.read" + tail,
	}, f.audits(t), "creations and reads are attributed to the exact duty and Staff basis; replays and refusals add nothing")

	call("PUT", f.duty+"/commands/"+v7(), "owner", iam.OperatingDutyDecisionInput{ExpectedVersion: 3, Decision: "revoke", Reason: "TEST immediate outlet cutoff"}, 201)
	call("GET", receiptPath, "receiver", nil, 404)
	call("GET", f.stock(f.pharmacy, f.outlet, "/receipts"), "receiver", nil, 404)
	call("PUT", f.stock(f.pharmacy, f.outlet, "/batches/"+v7()), "receiver", f.batch("TEST-RX-LATE"), 404)
}

func TestIndependentPharmacyReceiverCannotLeaveTheReceivingDuty(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.activate(t)
	call := f.call
	batch := v7()
	call("PUT", f.stock(f.pharmacy, f.outlet, "/batches/"+batch), "receiver", f.batch("TEST-RX-LOT"), 200)
	for name, path := range map[string]string{
		"a sibling outlet":         f.stock(f.pharmacy, f.sibling, "/batches/"+v7()),
		"an unknown pharmacy":      f.stock(v7(), f.outlet, "/batches/"+v7()),
		"a hospital-owned outlet":  f.stock(f.hospitalPharmacy, f.hospitalOutlet, "/batches/"+v7()),
		"a non-version-7 resource": f.stock(f.pharmacy, f.outlet, "/batches/"+uuid.NewString()),
	} {
		t.Run(name, func(t *testing.T) {
			require.JSONEq(t, `{"error":"not_found","message":"pharmacy resource not found"}`, call("PUT", path, "receiver", f.batch("TEST-RX-LOT"), 404))
			call("GET", strings.Replace(path, "/batches/", "/receipts/", 1), "receiver", nil, 404)
		})
	}
	call("GET", f.stock(f.pharmacy, f.sibling, "/receipts"), "receiver", nil, 404)
	call("GET", f.stock(f.pharmacy, f.outlet, "/receipts"), "foreign", nil, 404)

	receipt := f.stock(f.pharmacy, f.outlet, "/receipts/"+v7())
	for _, kind := range []string{"correction", "release", "dispense"} {
		body := f.receipt(batch, f.quarantine, "TEST-GRN-"+kind)
		body["kind"] = kind
		call("PUT", receipt, "receiver", body, 400)
	}
	require.Contains(t, call("PUT", receipt, "receiver", f.receipt(batch, f.shelf, "TEST-GRN-SHELF"), 409), "quarantine only")
	require.Contains(t, call("PUT", receipt, "receiver", f.receipt(v7(), f.quarantine, "TEST-GRN-NOBATCH"), 404), "not found")
	call("PUT", f.stock(f.pharmacy, f.outlet, "/locations/"+v7()), "receiver", map[string]any{"code": "TEST-NEW", "name": "TEST New"}, 404)
	call("PUT", f.stock(f.pharmacy, f.outlet, "/corrections/"+v7()), "receiver", f.receipt(batch, f.quarantine, "TEST-COR"), 404)
	editor := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/outlets/" + f.outlet + "/stock"
	call("PUT", editor+"/movements/"+v7(), "receiver", f.receipt(batch, f.quarantine, "TEST-EDITOR"), 403)
	call("GET", editor+"/batches/"+batch, "receiver", nil, 403)
	require.Contains(t, call("GET", editor+"/batches/"+batch, "owner", nil, 200), `"id":"`+batch+`"`, "the editor's own routes are unchanged")
	require.Len(t, f.audits(t), 1, "refused attempts write no duty evidence")

	revocations := iam.NewRevocationChecker(f.db)
	require.NoError(t, f.db.WithSystem(f.ctx, func(tx *gorm.DB) error {
		return revocations.RevokeTx(tx, "user-receiver", time.Now().UTC().Truncate(time.Microsecond), "sign_out", "user-receiver")
	}))
	require.Contains(t, call("GET", f.stock(f.pharmacy, f.outlet, "/receipts"), "receiver", nil, 401), "session_changed")
	require.Contains(t, call("PUT", f.stock(f.pharmacy, f.outlet, "/batches/"+v7()), "receiver", f.batch("TEST-RX-REVOKED"), 401), "session_changed")
	require.Len(t, f.audits(t), 1, "a revoked credential writes no duty evidence")
}

func TestIndependentPharmacyReceiverLosesTheDutyWhenItsBasisChanges(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.activate(t)
	call := f.call
	list := f.stock(f.pharmacy, f.outlet, "/receipts")
	call("GET", list, "receiver", nil, 200)

	f.checks.down.Store(true)
	require.JSONEq(t, `{"error":"authorization_unavailable","message":"Operating authority could not be verified."}`, call("GET", list, "receiver", nil, 503))
	call("PUT", f.stock(f.pharmacy, f.outlet, "/batches/"+v7()), "receiver", f.batch("TEST-RX-DOWN"), 503)
	f.checks.down.Store(false)
	call("GET", list, "receiver", nil, 200)

	staff := "/v1/workforce/pharmacies/" + f.pharmacy + "/staff/" + f.staff + "?directory_context=v1"
	call("PUT", staff, "owner", map[string]any{"employee_code": "TEST-PH-RX", "display_name": "TEST Renamed Receiver", "job_category": "pharmacist", "employment_status": "active", "expected_version": 1, "directory_context_version": "v1"}, 200)
	call("GET", list, "receiver", nil, 404)
	require.Len(t, f.audits(t), 2, "only the two admitted reads are evidenced")
}

func TestIndependentPharmacyReceiverDutyEndsAtItsExpiry(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	expiresAt := time.Now().UTC().Truncate(time.Second).Add(10 * time.Second)
	f.input.ExpiresAt = expiresAt
	f.activate(t)
	list := f.stock(f.pharmacy, f.outlet, "/receipts")
	require.True(t, time.Now().Before(expiresAt), "activation must finish inside the validity window")
	f.call("GET", list, "receiver", nil, 200)
	require.Eventually(t, func() bool { return !time.Now().Before(expiresAt) }, 15*time.Second, 25*time.Millisecond)
	f.call("GET", list, "receiver", nil, 404)
}

func TestIndependentPharmacyReceiverAuditFailureRollsBackTheReceipt(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.activate(t)
	batch, movement := v7(), v7()
	f.call("PUT", f.stock(f.pharmacy, f.outlet, "/batches/"+batch), "receiver", f.batch("TEST-RX-LOT"), 200)
	require.NoError(t, f.db.Migrate(f.ctx, []tenantdb.Migration{{ID: "test_operating_receipt_audit_refused", SQL: fmt.Sprintf(`CREATE FUNCTION test_refuse_receipt_audit()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.action='pharmacy.operating_stock.receipt_recorded' THEN RAISE EXCEPTION 'TEST audit refused' USING ERRCODE='%s'; END IF; RETURN NEW; END $$;
CREATE TRIGGER test_refuse_receipt_audit BEFORE INSERT ON pharmacy_operating_stock_audit FOR EACH ROW EXECUTE FUNCTION test_refuse_receipt_audit();`, "23514")}}))
	receipt := f.stock(f.pharmacy, f.outlet, "/receipts/"+movement)
	require.JSONEq(t, `{"error":"audit_unavailable","message":"Duty evidence could not be recorded; nothing was saved."}`, f.call("PUT", receipt, "receiver", f.receipt(batch, f.quarantine, "TEST-GRN-1"), 503))
	f.call("GET", receipt, "receiver", nil, 404)
	require.Zero(t, f.movementEvents(t, movement), "the outbox entry rolls back with the receipt")
	var movements int64
	f.ownerOutletTx(t, func(tx *gorm.DB) error {
		return tx.Table("pharmacy_stock_movements").Where("tenant_id=? AND outlet_id=? AND id=?", testutil.TenantA, f.outlet, movement).Count(&movements).Error
	})
	require.Zero(t, movements)
}
