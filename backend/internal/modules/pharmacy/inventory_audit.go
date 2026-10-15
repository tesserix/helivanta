package pharmacy

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"time"
)

type inventoryAudit struct {
	ID                   uuid.UUID `json:"id"`
	ProductID            uuid.UUID `json:"product_id"`
	SKU                  string    `json:"sku"`
	BrandName            string    `json:"brand_name"`
	UnitOfIssue          string    `json:"unit_of_issue"`
	Currency             string    `json:"currency"`
	LotNumber            string    `json:"lot_number"`
	ExpiresOn            string    `json:"expires_on"`
	CreatedAt            time.Time `json:"created_at"`
	OnHandUnits          int64     `json:"on_hand_units"`
	ReceivedUnits        int64     `json:"received_units"`
	UncostedReceiptUnits int64     `json:"uncosted_receipt_units"`
	AcquisitionCostMinor string    `json:"acquisition_cost_minor"`
	ValuationMinor       *string   `json:"valuation_minor"`
	ValuationStatus      string    `json:"valuation_status"`
}

func (r inventoryAudit) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func (h *catalogueHandlers) listInventoryAudit(c *gin.Context, params pagination.Params) ([]inventoryAudit, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var rows []inventoryAudit
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Table("pharmacy_inventory_audit").Select("id,product_id,sku,brand_name,unit_of_issue,currency,lot_number,to_char(expires_on,'YYYY-MM-DD') AS expires_on,created_at,on_hand_units,received_units,uncosted_receipt_units,acquisition_cost_minor,valuation_minor,valuation_status").Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Find(&rows).Error
	})
	if err != nil {
		payableError(c, err)
	}
	return rows, nil
}
