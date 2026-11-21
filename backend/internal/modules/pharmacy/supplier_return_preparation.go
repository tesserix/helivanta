package pharmacy

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

func (h *catalogueHandlers) getSupplierReturnPreparation(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	id, ok := supplierReturnID(c, "payable")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var result SupplierReturnPreparation
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		bill, err := loadPayable(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		if bill.LineCount != len(bill.Fields.Lines) || bill.LineCount < 1 || bill.LineCount > 100 {
			return errSupplierReturnEvidence
		}
		ids := make([]uuid.UUID, 0, len(bill.Fields.Lines))
		for _, line := range bill.Fields.Lines {
			if line.Category == "inventory" {
				if line.ReceiptID == nil {
					return errSupplierReturnEvidence
				}
				ids = append(ids, *line.ReceiptID)
			}
		}
		if len(ids) == 0 {
			return gorm.ErrRecordNotFound
		}
		result = SupplierReturnPreparation{OutletID: outlet, PayableID: id, PayeeReference: bill.Fields.PayeeReference, PayeeName: bill.Fields.PayeeName, DocumentReference: bill.Fields.DocumentReference, Currency: bill.Fields.Currency, Lines: []SupplierReturnPreparationSource{}}
		var clock struct{ At time.Time }
		if err := tx.Raw("SELECT clock_timestamp() AS at").Scan(&clock).Error; err != nil {
			return err
		}
		result.StockAsOf = clock.At.UTC().Format(time.RFC3339Nano)
		err = withAcquisitionScope(tx, outlet, func() error {
			sources, err := lookupSupplierReturnSources(tx, p.TenantID, outlet, id, ids)
			if err != nil {
				return err
			}
			if len(sources) != len(ids) {
				return errSupplierReturnEvidence
			}
			for _, source := range sources {
				if source.Currency != result.Currency || source.ProductName == "" || source.GenericName == "" || source.UnitCode == "" || source.ReceiptUnits < 1 || source.DispatchedUnits < 0 || source.DispatchedUnits > source.ReceiptUnits {
					return errSupplierReturnEvidence
				}
				origin := "external"
				if source.PlatformReceipt || strings.HasPrefix(source.SourceReference, "supplier-receipt:") {
					origin = "platform"
				}
				result.Lines = append(result.Lines, SupplierReturnPreparationSource{ReceiptID: source.ReceiptID, BatchID: source.BatchID, ProductID: source.ProductID, ProductVersion: source.ProductVersion, ProductName: source.ProductName, GenericName: source.GenericName, UnitCode: source.UnitCode, LotNumber: source.LotNumber, ExpiresOn: source.ExpiresOn, ReceiptUnits: source.ReceiptUnits, SourceReference: source.SourceReference, SourceCreatedAt: source.SourceCreatedAt, DispatchedUnits: source.DispatchedUnits, Origin: origin})
			}
			return nil
		})
		if err != nil {
			return err
		}
		if err := auditRegisterRead(c, tx, "supplier_return_preparation", p.TenantID, p.Subject, id, "read"); err != nil {
			return errors.Join(errSupplierReturnEvidence, err)
		}
		return nil
	})
	if err != nil {
		supplierReturnError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *catalogueHandlers) getSupplierReturnPreparationReceipt(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	id, ok := supplierReturnID(c, "return")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierReturnCreationReceipt
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		row, lines, err := loadSupplierReturn(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		if row.ActorSubject != p.Subject {
			return gorm.ErrRecordNotFound
		}
		if err := auditRegisterRead(c, tx, "supplier_return_preparation_recovery", p.TenantID, p.Subject, id, "recover"); err != nil {
			return errors.Join(errSupplierReturnEvidence, err)
		}
		receipt = row.receipt(lines)
		return nil
	})
	if err != nil {
		supplierReturnError(c, err)
		return
	}
	respond.OK(c, receipt)
}
