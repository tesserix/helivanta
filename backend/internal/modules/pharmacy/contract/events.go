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

// SubjectPrescriptionImageScanRequested asks the pharmacy evidence worker to scan
// one uploaded prescription page (#1386 B-2a).
const SubjectPrescriptionImageScanRequested = "helivanta.in.pharmacy.prescription_image_scan_requested.v1"

// SubjectPrescriptionImageChanged records a page's scan outcome.
const SubjectPrescriptionImageChanged = "helivanta.in.pharmacy.prescription_image_changed.v1"

// PrescriptionImageScanRequestedData carries ids only, never prescriber or
// patient details.
type PrescriptionImageScanRequestedData struct {
	OutletID       string `json:"outlet_id"`
	PrescriptionID string `json:"prescription_id"`
	FileID         string `json:"file_id"`
	ScanAttempt    int64  `json:"scan_attempt"`
}

// PrescriptionImageScanInput is the worker's whole input, including the tenant.
type PrescriptionImageScanInput struct {
	TenantID       string `json:"tenant_id"`
	OutletID       string `json:"outlet_id"`
	PrescriptionID string `json:"prescription_id"`
	FileID         string `json:"file_id"`
	ScanAttempt    int64  `json:"scan_attempt"`
}

// PrescriptionImageChangedData adds the bounded lifecycle status.
type PrescriptionImageChangedData struct {
	PrescriptionImageScanRequestedData
	Status string `json:"status"`
}

// SubjectCreditNoteIssued identifies a counter credit note for goods returned (#1386 C-1).
const SubjectCreditNoteIssued = "helivanta.in.pharmacy.credit_note_issued.v1" //nolint:gosec // an event subject name, not a credential value

// CreditNoteIssuedData carries ids and totals, never the recipient.
type CreditNoteIssuedData struct {
	PharmacyID       string                 `json:"pharmacy_id"`
	OutletID         string                 `json:"outlet_id"`
	CreditNoteID     string                 `json:"credit_note_id"`
	SaleID           string                 `json:"sale_id"`
	CreditNoteNumber string                 `json:"credit_note_number"`
	Currency         string                 `json:"currency"`
	TotalMinor       int64                  `json:"total_minor"`
	Lines            []CreditNoteIssuedLine `json:"lines"`
}

// CreditNoteIssuedLine is one batch returned to quarantine.
type CreditNoteIssuedLine struct {
	ProductID     string `json:"product_id"`
	BatchID       string `json:"batch_id"`
	QuantityUnits int64  `json:"quantity_units"`
}

// SubjectPaymentRequested asks payments to collect money for something
// the pharmacy domain sells (#1386 C-4a). Payments answers with its intent
// events. Purpose says what for; the counter sale is the first.
const SubjectPaymentRequested = "helivanta.in.pharmacy.payment_requested.v1"

// SubjectPaymentCancelled asks payments to close a request's QR and settle
// it; the answer is intent_paid or intent_cancelled, never silence.
const SubjectPaymentCancelled = "helivanta.in.pharmacy.payment_cancelled.v1"

// PaymentPurposeCounterSale is a payment taken at an outlet counter.
const PaymentPurposeCounterSale = "counter_sale"

// PaymentRequestedData carries the amount and its purpose, and nothing
// about the patient or the basket.
type PaymentRequestedData struct {
	RequestID        string `json:"request_id"`
	Purpose          string `json:"purpose"`
	AmountMinor      int64  `json:"amount_minor"`
	Currency         string `json:"currency"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

// PaymentCancelledData names the request to close.
type PaymentCancelledData struct {
	RequestID string `json:"request_id"`
}
