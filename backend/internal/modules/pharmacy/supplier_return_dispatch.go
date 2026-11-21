package pharmacy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

type supplierReturnDispatchRow struct {
	ID                  uuid.UUID
	TenantID            uuid.UUID
	OutletID            uuid.UUID
	ReturnID            uuid.UUID
	LicenceID           uuid.UUID
	LicenceReviewID     uuid.UUID
	DocumentReference   string
	RecipientName       string
	HandedOverAt        time.Time
	PhysicalDisposition string
	Reason              string
	IntentSHA256        string
	ActorSubject        string
	CreatedAt           time.Time `gorm:"autoCreateTime:false;default:now()"`
}

func (supplierReturnDispatchRow) TableName() string { return "pharmacy_supplier_return_dispatches" }

const supplierReturnDispatchColumns = "id,tenant_id,outlet_id,return_id,licence_id,licence_review_id,document_reference,recipient_name,handed_over_at,physical_disposition,reason,intent_sha256,actor_subject,created_at"

type supplierReturnDispatchLineRow struct {
	TenantID              uuid.UUID
	OutletID              uuid.UUID
	DispatchID            uuid.UUID
	ReturnLineID          uuid.UUID
	MovementID            uuid.UUID
	ScheduleDeclarationID uuid.UUID
}

func (supplierReturnDispatchLineRow) TableName() string {
	return "pharmacy_supplier_return_dispatch_lines"
}

func loadSupplierReturnDispatch(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (supplierReturnDispatchRow, SupplierReturnDispatchReceipt, error) {
	var row supplierReturnDispatchRow
	var receipt SupplierReturnDispatchReceipt
	if err := tx.Select(supplierReturnDispatchColumns).Where("tenant_id=? AND outlet_id=? AND id=?", tenant, outlet, id).Take(&row).Error; err != nil {
		return row, receipt, err
	}
	parent, lines, err := loadSupplierReturn(tx, tenant, outlet, row.ReturnID)
	if err != nil {
		return row, receipt, err
	}
	var stored []supplierReturnDispatchLineRow
	if err := tx.Select("tenant_id,outlet_id,dispatch_id,return_line_id,movement_id,schedule_declaration_id").Where("tenant_id=? AND outlet_id=? AND dispatch_id=?", tenant, outlet, id).Limit(101).Find(&stored).Error; err != nil {
		return row, receipt, err
	}
	if len(stored) != len(lines) {
		return row, receipt, errSupplierReturnEvidence
	}
	byLine := make(map[uuid.UUID]uuid.UUID, len(stored))
	for _, line := range stored {
		byLine[line.ReturnLineID] = line.MovementID
	}
	proof := make([]SupplierReturnDispatchLine, len(lines))
	for i, line := range lines {
		movement, ok := byLine[line.ID]
		if !ok {
			return row, receipt, errSupplierReturnEvidence
		}
		proof[i] = SupplierReturnDispatchLine{ReturnLineID: line.ID, MovementID: movement}
	}
	version := int64(0)
	receipt = SupplierReturnDispatchReceipt{SupplierReturnDispatchInput: SupplierReturnDispatchInput{ExpectedVersion: &version, LicenceID: row.LicenceID, DocumentReference: row.DocumentReference, RecipientName: row.RecipientName, HandedOverAt: row.HandedOverAt.UTC().Format(time.RFC3339Nano), PhysicalDisposition: row.PhysicalDisposition, Reason: row.Reason}, ID: row.ID, OutletID: outlet, ReturnID: row.ReturnID, Number: parent.Number, Version: 1, State: "dispatched", LicenceReviewID: row.LicenceReviewID, Lines: proof, DispatchedBy: row.ActorSubject, RecordedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano)}
	return row, receipt, nil
}

func (h *catalogueHandlers) dispatchSupplierReturn(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	parent, ok := supplierReturnID(c, "return")
	if !ok {
		return
	}
	id, ok := supplierReturnID(c, "dispatch")
	if !ok {
		return
	}
	var in SupplierReturnDispatchInput
	if !decodeStockCommand(c, &in) {
		return
	}
	if !normalizeSupplierReturnDispatch(&in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Provide draft version zero, a reviewed outlet licence and explicit outward handoff evidence.")
		return
	}
	digest, err := supplierReturnHash(p.Subject, in)
	if err != nil {
		supplierReturnError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierReturnDispatchReceipt
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		if err := supplierReturnLock(tx, p.TenantID, outlet, parent); err != nil {
			return err
		}
		_, lines, err := loadSupplierReturn(tx, p.TenantID, outlet, parent)
		if err != nil {
			return err
		}
		row, recovered, err := loadSupplierReturnDispatch(tx, p.TenantID, outlet, id)
		if err == nil {
			if row.ReturnID != parent || row.ActorSubject != p.Subject || row.IntentSHA256 != digest {
				return errSupplierReturnConflict
			}
			receipt = recovered
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var terminal int64
		if err := tx.Raw(`SELECT(SELECT COUNT(*)FROM pharmacy_supplier_return_cancellations WHERE tenant_id=? AND outlet_id=? AND return_id=?)+(SELECT COUNT(*)FROM pharmacy_supplier_return_dispatches WHERE tenant_id=? AND outlet_id=? AND return_id=?)`, p.TenantID, outlet, parent, p.TenantID, outlet, parent).Scan(&terminal).Error; err != nil {
			return err
		}
		if terminal != 0 {
			return errSupplierReturnConflict
		}
		var licence struct{ ReviewID uuid.UUID }
		if err := tx.Raw("SELECT review_id FROM pharmacy_trading_licence_source(?,?,?)", p.TenantID, outlet, in.LicenceID).Scan(&licence).Error; err != nil {
			return err
		}
		if licence.ReviewID == uuid.Nil {
			return errSupplierReturnConflict
		}
		at, err := time.Parse(time.RFC3339Nano, in.HandedOverAt)
		if err != nil {
			return err
		}
		row = supplierReturnDispatchRow{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, ReturnID: parent, LicenceID: in.LicenceID, LicenceReviewID: licence.ReviewID, DocumentReference: in.DocumentReference, RecipientName: in.RecipientName, HandedOverAt: at, PhysicalDisposition: in.PhysicalDisposition, Reason: in.Reason, IntentSHA256: digest, ActorSubject: p.Subject}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		proof := make([]supplierReturnDispatchLineRow, len(lines))
		for i, line := range lines {
			var schedule struct{ DeclarationID uuid.UUID }
			if err := tx.Raw("SELECT declaration_id FROM pharmacy_product_schedule_in_force(?,?,?)", p.TenantID, outlet, line.ProductID).Scan(&schedule).Error; err != nil {
				return err
			}
			if schedule.DeclarationID == uuid.Nil {
				return errSupplierReturnConflict
			}
			proof[i] = supplierReturnDispatchLineRow{TenantID: row.TenantID, OutletID: outlet, DispatchID: id, ReturnLineID: line.ID, MovementID: uuid.Must(uuid.NewV7()), ScheduleDeclarationID: schedule.DeclarationID}
		}
		if err := tx.Create(&proof).Error; err != nil {
			return err
		}
		for i, line := range lines {
			_, _, err := h.recordMovement(tx, p, currentPharmacy(ctx), outlet, proof[i].MovementID, movementInput{LocationID: line.QuarantineLocationID, BatchID: line.BatchID, Kind: "supplier_return", QuantityUnits: -*line.QuantityUnits, Reason: in.Reason, SourceReference: fmt.Sprintf("supplier-return:%s:%d", id, i+1)})
			if err != nil {
				return err
			}
		}
		_, receipt, err = loadSupplierReturnDispatch(tx, p.TenantID, outlet, id)
		return err
	})
	if err != nil {
		supplierReturnError(c, err)
		return
	}
	respond.OK(c, receipt)
}
func (h *catalogueHandlers) getSupplierReturnDispatch(c *gin.Context) {
	p, outlet, ok := supplierReturnScope(c)
	if !ok {
		return
	}
	parent, ok := supplierReturnID(c, "return")
	if !ok {
		return
	}
	id, ok := supplierReturnID(c, "dispatch")
	if !ok || !supplierReturnReadRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var receipt SupplierReturnDispatchReceipt
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		row, result, err := loadSupplierReturnDispatch(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		if row.ReturnID != parent || row.ActorSubject != p.Subject {
			return gorm.ErrRecordNotFound
		}
		if err := auditRegisterRead(c, tx, "supplier_return_dispatches", p.TenantID, p.Subject, id, "recover"); err != nil {
			return errors.Join(errSupplierReturnEvidence, err)
		}
		receipt = result
		return nil
	})
	if err != nil {
		supplierReturnError(c, err)
		return
	}
	respond.OK(c, receipt)
}
