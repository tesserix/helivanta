// Package contract is pharmacy's published event interface. See
// medicore/contract for why this package exists and why it is data only.
package contract

// SubjectBatchSafetyRecorded identifies an immutable, unreviewed safety concern.
const SubjectBatchSafetyRecorded = "helivanta.in.pharmacy.batch_safety_recorded.v1"

// BatchSafetyRecordedData carries identifiers only, never incident descriptions.
type BatchSafetyRecordedData struct {
	PharmacyID string `json:"pharmacy_id"`
	OutletID   string `json:"outlet_id"`
	BatchID    string `json:"batch_id"`
	IncidentID string `json:"incident_id"`
}

// SubjectDispenseRecorded is published when a pharmacist records a
// dispense. No module consumes it today; publishing it is how the
// dispense becomes visible to anything added later without changing
// pharmacy.
const SubjectDispenseRecorded = "helivanta.in.pharmacy.dispense_recorded.v1"

// DispenseRecordedData is the v1 payload of SubjectDispenseRecorded.
type DispenseRecordedData struct {
	DispenseID string `json:"dispense_id"`
	VisitID    string `json:"visit_id"`
}

// SubjectCatalogueUpdated identifies a committed private-product revision.
const SubjectCatalogueUpdated = "helivanta.in.pharmacy.catalogue_updated.v1"

// CatalogueUpdatedData contains identifiers only, not clinical or commercial data.
type CatalogueUpdatedData struct {
	PharmacyID string `json:"pharmacy_id"`
	OutletID   string `json:"outlet_id"`
	ProductID  string `json:"product_id"`
	Version    int64  `json:"version"`
	Status     string `json:"status"`
}

// SubjectStockMovementRecorded identifies a committed quarantined stock movement.
const SubjectStockMovementRecorded = "helivanta.in.pharmacy.stock_movement_recorded.v1"

// StockMovementRecordedData exposes only protected stock references.
type StockMovementRecordedData struct {
	PharmacyID string `json:"pharmacy_id"`
	OutletID   string `json:"outlet_id"`
	MovementID string `json:"movement_id"`
	LocationID string `json:"location_id"`
	BatchID    string `json:"batch_id"`
}

// SubjectPayableRecorded identifies an immutable operational purchase or expense document.
const SubjectPayableRecorded = "helivanta.in.pharmacy.payable_recorded.v1"

// PayableRecordedData carries references, never supplier names or financial amounts.
type PayableRecordedData struct {
	PharmacyID string `json:"pharmacy_id"`
	OutletID   string `json:"outlet_id"`
	PayableID  string `json:"payable_id"`
}

// SubjectSettlementRecorded identifies a recorded external settlement, credit or reversal.
const SubjectSettlementRecorded = "helivanta.in.pharmacy.settlement_recorded.v1"

// SettlementRecordedData excludes payer names, amounts and external payment references.
type SettlementRecordedData struct {
	PharmacyID   string `json:"pharmacy_id"`
	OutletID     string `json:"outlet_id"`
	PayableID    string `json:"payable_id"`
	SettlementID string `json:"settlement_id"`
}

// SubjectSalePosted identifies a posted counter sale invoice.
const SubjectSalePosted = "helivanta.in.pharmacy.sale_posted.v1"

// SalePostedData carries ids and totals, never the buyer or a prescription.
type SalePostedData struct {
	PharmacyID    string           `json:"pharmacy_id"`
	OutletID      string           `json:"outlet_id"`
	SaleID        string           `json:"sale_id"`
	InvoiceNumber string           `json:"invoice_number"`
	Currency      string           `json:"currency"`
	TotalMinor    int64            `json:"total_minor"`
	Lines         []SalePostedLine `json:"lines"`
}

// SalePostedLine is one allocated batch of a sale.
type SalePostedLine struct {
	ProductID     string `json:"product_id"`
	BatchID       string `json:"batch_id"`
	QuantityUnits int64  `json:"quantity_units"`
}
