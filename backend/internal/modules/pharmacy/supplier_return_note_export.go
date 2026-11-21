package pharmacy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
)

func (h *catalogueHandlers) exportSupplierReturnNote(c *gin.Context) {
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
	var note supplierReturnNote
	var data []byte
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		parent, lines, err := loadSupplierReturn(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		var source struct {
			Dispatch                                         supplierReturnDispatchRow `gorm:"embedded"`
			NotePharmacyName, NoteOutletName, NoteOutletCode string
		}
		if err := tx.Table("pharmacy_supplier_return_dispatches").Select(supplierReturnDispatchColumns+",note_pharmacy_name,note_outlet_name,note_outlet_code").Where("tenant_id=? AND outlet_id=? AND return_id=?", p.TenantID, outlet, id).Take(&source).Error; err != nil {
			return err
		}
		var proven int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_supplier_return_dispatch_lines l
 JOIN pharmacy_stock_movements m ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.id=l.movement_id
 WHERE l.tenant_id=? AND l.outlet_id=? AND l.dispatch_id=? AND pharmacy_supplier_return_movement_matches(m)`, p.TenantID, outlet, source.Dispatch.ID).Scan(&proven).Error; err != nil {
			return err
		}
		if proven != int64(len(lines)) {
			return errSupplierReturnEvidence
		}
		note = supplierReturnNote{PharmacyName: source.NotePharmacyName, OutletName: source.NoteOutletName, OutletCode: source.NoteOutletCode, SupplierName: parent.PayeeName, OriginalBill: parent.DocumentReference, Reference: source.Dispatch.DocumentReference, Recipient: source.Dispatch.RecipientName, HandoffUTC: source.Dispatch.HandedOverAt.UTC().Format("2006-01-02 15:04:05.999999 UTC"), Reason: parent.Reason, Number: parent.Number, Lines: lines}
		data, err = renderSupplierReturnNote(note)
		if err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "supplier_return_note", p.TenantID, p.Subject, id, "exported")
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			supplierReturnError(c, err)
		} else {
			respond.Error(c, http.StatusServiceUnavailable, "supplier_return_note_unavailable", "The complete commercial note could not be verified. No file was released.")
		}
		return
	}
	if ctx.Err() != nil {
		respond.Error(c, http.StatusServiceUnavailable, "supplier_return_note_unavailable", "Note verification timed out. No file was released.")
		return
	}
	c.Header("X-Supplier-Return-Tenant-Id", p.TenantID)
	c.Header("X-Supplier-Return-Outlet-Id", outlet.String())
	c.Header("X-Supplier-Return-Id", id.String())
	c.Header("X-Supplier-Return-Number", strconv.FormatInt(note.Number, 10))
	c.Header("X-Export-Row-Count", strconv.Itoa(len(note.Lines)))
	c.Header("X-Export-Byte-Count", strconv.Itoa(len(data)))
	c.Header("X-Export-Sha256", fmt.Sprintf("%x", sha256.Sum256(data)))
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="supplier-return-%d.html"`, note.Number))
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}
