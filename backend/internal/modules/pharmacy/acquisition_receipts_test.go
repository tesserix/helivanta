package pharmacy

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm/schema"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
)

func acquisitionID() uuid.UUID { return uuid.Must(uuid.NewV7()) }
func acquisitionJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	require.NoError(t, err)
	return b
}
func acquisitionFixture(t *testing.T, owner string) acquisitionRow {
	t.Helper()
	row := acquisitionRow{AcquisitionReceipt: AcquisitionReceipt{ID: acquisitionID(), OwnerTenantID: acquisitionID(), PharmacyID: acquisitionID(), OutletID: acquisitionID(), BatchID: acquisitionID(), ProductID: acquisitionID(), ProductVersion: 1, Currency: "INR", UnitCode: "unit", QuantityUnits: 20, CreatedAt: time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)}}
	row.SourceReference = "supplier-receipt:" + row.ID.String()
	row.SupplierReceiptID = &row.ID
	one := int64(1)
	shipment := f.Shipment{ID: acquisitionID(), AllocationID: acquisitionID(), OrderID: acquisitionID(), LineID: acquisitionID(), OrderInputSha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SourceTenantID: acquisitionID(), SourceOrganizationID: acquisitionID(), SourceLocationID: acquisitionID(), BuyerTenantID: row.OwnerTenantID, HospitalID: acquisitionID(), BuyerOrganizationID: row.PharmacyID, BuyerLocationID: row.OutletID, Product: supplierorder.SupplierOrderProduct{Owner: owner, ProductID: acquisitionID()}, UnitCode: "unit", QuantityUnits: 25, LotNumber: "TEST-LOT", ExpiresOn: "2030-12-31", HandoffReference: "TEST-CHALLAN", ActorSubject: "TEST-source", ProductDescription: "TEST item", PackSize: 1, DispatchedAt: row.CreatedAt.Add(-time.Minute)}
	if owner == "pharmacy" {
		shipment.Product.Version = &one
	} else {
		id := acquisitionID()
		shipment.Product.VersionID = &id
	}
	receipt := f.ReceivingReceipt{ID: row.ID, OwnerTenantID: row.OwnerTenantID, ShipmentID: shipment.ID, OrderID: shipment.OrderID, HospitalID: shipment.HospitalID, PharmacyID: row.PharmacyID, OutletID: row.OutletID, Input: f.ReceivingInput{DestinationProductID: &row.ProductID, DestinationProductVersion: &one, DestinationBatchID: &row.BatchID, AcceptedUnits: 20, CountedUnits: 20, IdentityConfirmed: true}, CreatedAt: row.CreatedAt}
	summary := f.ReceivingSummary{ID: acquisitionID(), ShipmentID: shipment.ID, OrderID: shipment.OrderID, OwnerTenantID: row.OwnerTenantID, Version: 3, CountedUnits: 20, AcceptedUnits: 20, ShortUnits: 5, Closed: true}
	row.Receipt = acquisitionJSON(t, receipt)
	row.Shipment = acquisitionJSON(t, shipment)
	row.Summary = acquisitionJSON(t, summary)
	return row
}
func TestAcquisitionReceiptRetainsBothSourceOwnersAndDiscrepancies(t *testing.T) {
	for _, owner := range []string{"pharmacy", "supply"} {
		t.Run(owner, func(t *testing.T) {
			row := acquisitionFixture(t, owner)
			require.NoError(t, projectAcquisitionSupplier(&row))
			require.Equal(t, owner, row.Supplier.SourceProduct)
			require.EqualValues(t, 20, row.Supplier.OriginalAcceptedUnits)
			require.EqualValues(t, 20, row.Supplier.EffectiveAcceptedUnits)
			require.EqualValues(t, 1, row.Supplier.ReceiptVersion)
			require.EqualValues(t, 5, row.Supplier.DiscrepancyUnits)
			require.True(t, row.Supplier.ReceivingClosed)
		})
	}
}
func TestAcquisitionReceiptReportsCurrentCorrectionWithoutRewritingCostBasis(t *testing.T) {
	row := acquisitionFixture(t, "supply")
	var original f.ReceivingReceipt
	require.NoError(t, json.Unmarshal(row.Receipt, &original))
	var summary f.ReceivingSummary
	require.NoError(t, json.Unmarshal(row.Summary, &summary))
	summary.CountedUnits = 18
	summary.AcceptedUnits = 18
	summary.ShortUnits = 0
	summary.Closed = false
	summary.Version = 4
	row.Summary = acquisitionJSON(t, summary)
	row.Correction = acquisitionJSON(t, f.ReceivingCorrection{ID: acquisitionID(), OwnerTenantID: row.OwnerTenantID, ShipmentID: original.ShipmentID, ReceiptID: row.ID, OutletID: row.OutletID, Version: 2, Input: f.ReceivingCorrectionInput{CountedUnits: 18, AcceptedUnits: 18}, CumulativeCountedUnits: 18, CumulativeAcceptedUnits: 18})
	require.NoError(t, projectAcquisitionSupplier(&row))
	require.EqualValues(t, 20, row.QuantityUnits)
	require.EqualValues(t, 18, row.Supplier.EffectiveAcceptedUnits)
	require.EqualValues(t, 2, row.Supplier.ReceiptVersion)
	require.False(t, row.Supplier.ReceivingClosed)
}
func TestAcquisitionReceiptRefusesMissingAndForeignOwnerEvidence(t *testing.T) {
	for _, change := range []struct {
		name  string
		alter func(*acquisitionRow)
	}{
		{"missing private receipt", func(r *acquisitionRow) { r.Receipt = nil }},
		{"missing private summary", func(r *acquisitionRow) { r.Summary = nil }},
		{"foreign outlet", func(r *acquisitionRow) { r.OutletID = acquisitionID() }},
		{"changed quantity", func(r *acquisitionRow) { r.QuantityUnits++ }},
		{"changed batch", func(r *acquisitionRow) { r.BatchID = acquisitionID() }},
		{"missing trace", func(r *acquisitionRow) { r.SupplierReceiptID = nil }},
	} {
		t.Run(change.name, func(t *testing.T) {
			r := acquisitionFixture(t, "pharmacy")
			change.alter(&r)
			require.Error(t, projectAcquisitionSupplier(&r))
		})
	}
}
func TestAcquisitionDirectReceiptDoesNotInventSupplierEvidence(t *testing.T) {
	r := acquisitionRow{AcquisitionReceipt: AcquisitionReceipt{ID: acquisitionID(), SourceReference: "TEST original direct receipt"}}
	require.NoError(t, projectAcquisitionSupplier(&r))
	require.Nil(t, r.Supplier)
}

func TestPayableSubjectPreconditionPreservesLegacyAndRejectsSwitchedAccounts(t *testing.T) {
	for _, test := range []struct {
		name    string
		values  []string
		allowed bool
	}{
		{"legacy without header", nil, true},
		{"matching account", []string{"TEST-finance"}, true},
		{"switched account", []string{"OTHER-finance"}, false},
		{"ambiguous account", []string{"TEST-finance", "OTHER-finance"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("PUT", "/bill", nil)
			for _, v := range test.values {
				c.Request.Header.Add("X-Helivanta-Expected-Subject", v)
			}
			require.Equal(t, test.allowed, payableSubject(c, "TEST-finance"))
			if !test.allowed {
				require.Equal(t, 409, recorder.Code)
			}
		})
	}
}

func TestAcquisitionReceiptReadProjectionHasValidDatabaseMapping(t *testing.T) {
	mapping, err := schema.Parse(&acquisitionRow{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	require.Contains(t, mapping.DBNames, "supplier_receipt_id")
	require.NotContains(t, mapping.DBNames, "supplier")
	require.Empty(t, mapping.Relationships.Relations)
}
