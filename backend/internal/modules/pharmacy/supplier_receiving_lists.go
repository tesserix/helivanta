package pharmacy

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

type listedSupplierReceipt struct{ f.ReceivingReceipt }

func (r listedSupplierReceipt) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type listedReceivingCorrection struct{ f.ReceivingCorrection }

func (r listedReceivingCorrection) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (h *supplierReceiving) registerLists(r *platform.Router, base string) {
	platform.ResourceListRoute(r, base+"/receipts", authz.Public, 50, h.listReceipts)
	platform.ResourceListRoute(r, base+"/receipts/:receipt/corrections", authz.Public, 50, h.listCorrections)
}

type receivingEvidenceRow struct {
	ID        uuid.UUID
	Body      json.RawMessage
	CreatedAt time.Time
}

func receivingEvidencePage(q *gorm.DB, page pagination.Params) ([]receivingEvidenceRow, error) {
	var rows []receivingEvidenceRow
	q = q.Select("id,body,created_at").Order("created_at DESC,id DESC").Limit(page.Limit + 1)
	if page.After != nil {
		q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
	}
	err := q.Scan(&rows).Error
	return rows, err
}
func receiptPage(tx *gorm.DB, s f.BuyerScope, page pagination.Params) ([]listedSupplierReceipt, error) {
	rows, err := receivingEvidencePage(tx.Table("pharmacy_supplier_receipts").Where("tenant_id=? AND shipment_id=? AND outlet_id=?", s.TenantID, s.ShipmentID, s.OutletID), page)
	if err != nil {
		return nil, err
	}
	result := make([]listedSupplierReceipt, 0, len(rows))
	for _, row := range rows {
		var v f.ReceivingReceipt
		if supplierorder.DecodeObject(row.Body, &v) != nil || v.ID != row.ID || v.OwnerTenantID != s.TenantID || v.ShipmentID != s.ShipmentID || v.OutletID != s.OutletID || v.HospitalID != s.HospitalID || !v.CreatedAt.Equal(row.CreatedAt) {
			return nil, f.ErrUnavailable
		}
		result = append(result, listedSupplierReceipt{v})
	}
	return result, nil
}
func correctionPage(tx *gorm.DB, s f.BuyerScope, receipt uuid.UUID, page pagination.Params) ([]listedReceivingCorrection, error) {
	rows, err := receivingEvidencePage(tx.Table("pharmacy_supplier_receiving_corrections").Where("tenant_id=? AND shipment_id=? AND outlet_id=? AND receipt_id=?", s.TenantID, s.ShipmentID, s.OutletID, receipt), page)
	if err != nil {
		return nil, err
	}
	result := make([]listedReceivingCorrection, 0, len(rows))
	for _, row := range rows {
		var v f.ReceivingCorrection
		if supplierorder.DecodeObject(row.Body, &v) != nil || v.ID != row.ID || v.OwnerTenantID != s.TenantID || v.ShipmentID != s.ShipmentID || v.ReceiptID != receipt || v.OutletID != s.OutletID || !v.CreatedAt.Equal(row.CreatedAt) {
			return nil, f.ErrUnavailable
		}
		result = append(result, listedReceivingCorrection{v})
	}
	return result, nil
}
func auditReceivingList(tx *gorm.DB, s f.BuyerScope, actor supplierorder.Actor, kind string) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'supplier_receiving_list_read',?,?)`, s.TenantID, actor.Subject, kind, s.ShipmentID).Error
}
func (h *supplierReceiving) listReceipts(c *gin.Context, page pagination.Params) ([]listedSupplierReceipt, error) {
	p, s, ok := receivingScope(c, true)
	if !ok {
		return nil, nil
	}
	var result []listedSupplierReceipt
	err := h.with(c, p, s, f.ReadReceiving, func(_ context.Context, tx *gorm.DB, shipment f.Shipment, actor supplierorder.Actor) error {
		var err error
		result, err = receiptPage(tx, s, page)
		if err != nil {
			return err
		}
		for _, row := range result {
			if row.PharmacyID != shipment.BuyerOrganizationID || row.OrderID != shipment.OrderID {
				return f.ErrUnavailable
			}
		}
		return auditReceivingList(tx, s, actor, "pharmacy_supplier_receipts")
	})
	if err != nil {
		supplierstock.Error(c, err)
		return nil, nil
	}
	return result, nil
}
func (h *supplierReceiving) listCorrections(c *gin.Context, page pagination.Params) ([]listedReceivingCorrection, error) {
	p, s, ok := receivingScope(c, true)
	if !ok {
		return nil, nil
	}
	receipt, ok := receivingID(c, "receipt", false)
	if !ok {
		return nil, nil
	}
	var result []listedReceivingCorrection
	err := h.with(c, p, s, f.ReadReceiving, func(_ context.Context, tx *gorm.DB, _ f.Shipment, actor supplierorder.Actor) error {
		if _, err := h.read(tx, s.TenantID, s.ShipmentID, receipt, actor, nil); err != nil {
			return err
		}
		var err error
		result, err = correctionPage(tx, s, receipt, page)
		if err != nil {
			return err
		}
		return auditReceivingList(tx, s, actor, "pharmacy_supplier_receiving_corrections")
	})
	if err != nil {
		supplierstock.Error(c, err)
		return nil, nil
	}
	return result, nil
}
