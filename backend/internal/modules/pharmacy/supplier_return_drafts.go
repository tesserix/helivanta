package pharmacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

func normalizeSupplierReturn(in *SupplierReturnDraftInput) (int64, bool) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.PayableID.Version() != 7 || in.PayableID.Variant() != uuid.RFC4122 || !stocktakeText(in.Reason, 500) || len(in.Lines) < 1 || len(in.Lines) > 100 {
		return 0, false
	}
	sort.Slice(in.Lines, func(i, j int) bool {
		a, b := in.Lines[i], in.Lines[j]
		if a.ReceiptID != b.ReceiptID {
			return a.ReceiptID.String() < b.ReceiptID.String()
		}
		return a.QuarantineLocationID.String() < b.QuarantineLocationID.String()
	})
	var total int64
	for i, line := range in.Lines {
		if line.ReceiptID.Version() != 7 || line.ReceiptID.Variant() != uuid.RFC4122 || line.QuarantineLocationID.Version() != 7 || line.QuarantineLocationID.Variant() != uuid.RFC4122 || line.QuantityUnits == nil || *line.QuantityUnits < 1 || *line.QuantityUnits > 1000000000 || line.ExpectedCreditMinor == nil || *line.ExpectedCreditMinor < 0 || *line.ExpectedCreditMinor > 1000000000000 {
			return 0, false
		}
		if i > 0 && line.ReceiptID == in.Lines[i-1].ReceiptID && line.QuarantineLocationID == in.Lines[i-1].QuarantineLocationID {
			return 0, false
		}
		total += *line.ExpectedCreditMinor
		if total > 1000000000000 {
			return 0, false
		}
	}
	return total, true
}
func supplierReturnHash(actor string, input any) (string, error) {
	raw, err := json.Marshal(struct {
		Actor string
		Input any
	}{actor, input})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func supplierReturnLock(tx *gorm.DB, tenant string, outlet, id uuid.UUID) error {
	return stockLock(tx, tenant+":"+outlet.String()+":supplier-return:"+id.String())
}
func (h *catalogueHandlers) putSupplierReturn(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	id, ok := supplierReturnID(c, "return")
	if !ok {
		return
	}
	var in SupplierReturnDraftInput
	if !decodeStockCommand(c, &in) {
		return
	}
	total, ok := normalizeSupplierReturn(&in)
	if !ok {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Provide bounded original payable receipts, quarantine buckets, issue units and explicit expected credit.")
		return
	}
	digest, err := supplierReturnHash(p.Subject, in)
	if err != nil {
		supplierReturnError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierReturnCreationReceipt
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		if err := supplierReturnLock(tx, p.TenantID, outlet, id); err != nil {
			return err
		}
		existing, lines, err := loadSupplierReturn(tx, p.TenantID, outlet, id)
		if err == nil {
			if existing.ActorSubject != p.Subject || existing.IntentSHA256 != digest {
				return errSupplierReturnConflict
			}
			receipt = existing.receipt(lines)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var bill payable
		if err := tx.Select("id,tenant_id,outlet_id,payee_reference,payee_name,document_reference,currency").Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, in.PayableID).Take(&bill).Error; err != nil {
			return err
		}
		var prepared []supplierReturnLineRow
		err = withAcquisitionScope(tx, outlet, func() error {
			var err error
			prepared, err = prepareSupplierReturnLines(tx, p.TenantID, outlet, id, in, bill.Fields.Currency)
			return err
		})
		if err != nil {
			return err
		}
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":supplier-return-number"); err != nil {
			return err
		}
		var number int64
		if err := tx.Model(&supplierReturnRow{}).Select("COALESCE(MAX(number),0)+1").Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Scan(&number).Error; err != nil {
			return err
		}
		row := supplierReturnRow{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, Number: number, PayableID: in.PayableID, PayeeReference: bill.Fields.PayeeReference, PayeeName: bill.Fields.PayeeName, DocumentReference: bill.Fields.DocumentReference, Currency: bill.Fields.Currency, LineCount: len(prepared), ExpectedCreditMinor: total, Reason: in.Reason, IntentSHA256: digest, ActorSubject: p.Subject}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := withAcquisitionScope(tx, outlet, func() error { return tx.Create(&prepared).Error }); err != nil {
			return err
		}
		result, stored, err := loadSupplierReturn(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		receipt = result.receipt(stored)
		return nil
	})
	if err != nil {
		supplierReturnError(c, err)
		return
	}
	respond.OK(c, receipt)
}

type supplierReturnSource struct {
	ReceiptID       uuid.UUID
	BatchID         uuid.UUID
	ProductID       uuid.UUID
	ProductVersion  int64
	ProductName     string
	GenericName     string
	UnitCode        string
	Currency        string
	LotNumber       string
	ExpiresOn       string
	ReceiptUnits    int64
	SourceReference string
	SourceCreatedAt string
	PlatformReceipt bool
	DispatchedUnits int64
}

func prepareSupplierReturnLines(tx *gorm.DB, tenant string, outlet, id uuid.UUID, in SupplierReturnDraftInput, currency string) ([]supplierReturnLineRow, error) {
	ids := make([]uuid.UUID, len(in.Lines))
	for i, line := range in.Lines {
		ids[i] = line.ReceiptID
	}
	rows, err := lookupSupplierReturnSources(tx, tenant, outlet, in.PayableID, ids)
	if err != nil {
		return nil, err
	}
	sources := make(map[uuid.UUID]supplierReturnSource, len(rows))
	for _, row := range rows {
		sources[row.ReceiptID] = row
	}
	requested := map[uuid.UUID]int64{}
	type bucket struct{ batch, location uuid.UUID }
	buckets := map[bucket]int64{}
	result := make([]supplierReturnLineRow, len(in.Lines))
	for i, line := range in.Lines {
		source, ok := sources[line.ReceiptID]
		if !ok {
			return nil, gorm.ErrRecordNotFound
		}
		if source.PlatformReceipt || strings.HasPrefix(source.SourceReference, "supplier-receipt:") {
			return nil, errSupplierReturnUnsupported
		}
		if source.Currency != currency || source.ProductName == "" || source.GenericName == "" || source.UnitCode == "" || source.ReceiptUnits < 1 || source.DispatchedUnits < 0 || source.DispatchedUnits > source.ReceiptUnits {
			return nil, errSupplierReturnEvidence
		}
		requested[line.ReceiptID] += *line.QuantityUnits
		if requested[line.ReceiptID]+source.DispatchedUnits > source.ReceiptUnits {
			return nil, errSupplierReturnConflict
		}
		key := bucket{source.BatchID, line.QuarantineLocationID}
		buckets[key] += *line.QuantityUnits
		result[i] = supplierReturnLineRow{TenantID: uuid.MustParse(tenant), OutletID: outlet, ReturnID: id, Position: i + 1, Fields: SupplierReturnLine{SupplierReturnDraftLineInput: line, ID: uuid.Must(uuid.NewV7()), BatchID: source.BatchID, ProductID: source.ProductID, ProductVersion: source.ProductVersion, ProductName: source.ProductName, GenericName: source.GenericName, UnitCode: source.UnitCode, LotNumber: source.LotNumber, ExpiresOn: source.ExpiresOn, ReceiptUnits: source.ReceiptUnits, SourceReference: source.SourceReference, SourceCreatedAt: source.SourceCreatedAt}}
	}
	for key, units := range buckets {
		var location struct{ StockStatus string }
		if err := tx.Table("pharmacy_stock_locations").Select("stock_status").Where("tenant_id=? AND outlet_id=? AND id=?", tenant, outlet, key.location).Take(&location).Error; err != nil {
			return nil, err
		}
		if location.StockStatus != "quarantine" {
			return nil, errSupplierReturnConflict
		}
		var balance int64
		if err := tx.Table("pharmacy_stock_movements").Select("COALESCE(SUM(quantity_units),0)").Where("tenant_id=? AND outlet_id=? AND location_id=? AND batch_id=?", tenant, outlet, key.location, key.batch).Scan(&balance).Error; err != nil {
			return nil, err
		}
		if units > balance {
			return nil, errSupplierReturnConflict
		}
	}
	return result, nil
}

func lookupSupplierReturnSources(tx *gorm.DB, tenant string, outlet, payableID uuid.UUID, ids []uuid.UUID) ([]supplierReturnSource, error) {
	if len(ids) == 0 {
		return []supplierReturnSource{}, nil
	}
	if len(ids) > 100 {
		return nil, errSupplierReturnEvidence
	}
	var rows []supplierReturnSource
	err := tx.Table("pharmacy_payable_lines l").Select(`m.id AS receipt_id,m.batch_id,b.product_id,b.product_version,COALESCE(NULLIF(r.snapshot->>'brand_name',''),r.snapshot->>'generic_name') AS product_name,r.snapshot->>'generic_name' AS generic_name,r.snapshot->>'unit_of_issue' AS unit_code,r.snapshot->>'currency' AS currency,b.lot_number,to_char(b.expires_on,'YYYY-MM-DD') AS expires_on,m.quantity_units AS receipt_units,m.source_reference,to_char(m.created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS source_created_at,EXISTS(SELECT 1 FROM pharmacy_supplier_receipts sr WHERE sr.tenant_id=m.tenant_id AND sr.outlet_id=m.outlet_id AND sr.id=m.id) AS platform_receipt,COALESCE((SELECT SUM(rl.quantity_units) FROM pharmacy_supplier_return_lines rl JOIN pharmacy_supplier_return_dispatches d ON d.tenant_id=rl.tenant_id AND d.outlet_id=rl.outlet_id AND d.return_id=rl.return_id WHERE rl.tenant_id=m.tenant_id AND rl.outlet_id=m.outlet_id AND rl.receipt_id=m.id),0) AS dispatched_units`).
		Joins("JOIN pharmacy_stock_movements m ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.id=l.receipt_id").
		Joins("JOIN pharmacy_stock_batches b ON b.tenant_id=m.tenant_id AND b.outlet_id=m.outlet_id AND b.id=m.batch_id").
		Joins("JOIN pharmacy_catalogue_revisions r ON r.tenant_id=b.tenant_id AND r.outlet_id=b.outlet_id AND r.product_id=b.product_id AND r.version=b.product_version").
		Where("l.tenant_id=? AND l.outlet_id=? AND l.payable_id=? AND l.category='inventory' AND m.kind='receipt' AND m.id IN ?", tenant, outlet, payableID, ids).Order("l.position").Limit(100).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
