package pharmacy

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"reflect"
	"strings"
	"time"
)

type settlement struct {
	ID           uuid.UUID       `json:"id"`
	TenantID     uuid.UUID       `json:"-"`
	OutletID     uuid.UUID       `json:"outlet_id"`
	PayableID    uuid.UUID       `json:"payable_id"`
	Fields       settlementInput `gorm:"embedded" json:"-"`
	ActorSubject string          `json:"actor_subject"`
	CreatedAt    time.Time       `json:"created_at"`
}

func (settlement) TableName() string                 { return "pharmacy_settlements" }
func (r settlement) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func (r settlement) MarshalJSON() ([]byte, error) {
	type fields settlement
	return json.Marshal(struct {
		fields
		settlementInput
	}{fields: fields(r), settlementInput: r.Fields})
}

const settlementColumns = "id,tenant_id,outlet_id,payable_id,kind,amount_minor,currency,paid_by,method,source_reference,to_char(occurred_on,'YYYY-MM-DD') AS occurred_on,reason,reversal_of,actor_subject,created_at"

func (h *catalogueHandlers) putPayment(c *gin.Context)    { h.putSettlement(c, false) }
func (h *catalogueHandlers) putAdjustment(c *gin.Context) { h.putSettlement(c, true) }
func (h *catalogueHandlers) putSettlement(c *gin.Context, adjustment bool) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	doc, ok := catalogueID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("entry"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "settlement")
		return
	}
	var in settlementInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.SourceReference = strings.ToUpper(strings.TrimSpace(in.SourceReference))
	in.PaidBy = strings.TrimSpace(in.PaidBy)
	in.Reason = strings.TrimSpace(in.Reason)
	_, err = time.Parse(time.DateOnly, in.OccurredOn)
	if err != nil || !currencyPattern.MatchString(in.Currency) || in.PaidBy == "" || in.Reason == "" || in.SourceReference == "" || adjustment == (in.Kind == "payment") || (in.Kind == "payment_reversal") != (in.ReversalOf != nil) {
		respond.Error(c, 400, "invalid_request", "check settlement kind, date, reference and attribution")
		return
	}
	if in.ReversalOf != nil && (in.ReversalOf.Version() != 7 || in.ReversalOf.Variant() != uuid.RFC4122) {
		respond.Error(c, 400, "invalid_request", "invalid original payment")
		return
	}
	var result settlement
	err = h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":payable:"+doc.String()); err != nil {
			return err
		}
		// Authorize the parent before looking up a globally unique entry identifier.
		var count int64
		if err := tx.Model(&payable{}).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, doc).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return gorm.ErrRecordNotFound
		}
		err := tx.Select(settlementColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&result).Error
		if err == nil {
			if result.PayableID != doc || !reflect.DeepEqual(result.Fields, in) {
				return errPayableConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		result = settlement{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, PayableID: doc, Fields: in, ActorSubject: p.Subject}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		if err := tx.Select(settlementColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&result).Error; err != nil {
			return err
		}
		data, err := json.Marshal(pharmacycontract.SettlementRecordedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), PayableID: doc.String(), SettlementID: id.String()})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, pharmacycontract.SubjectSettlementRecorded, events.Event{Type: "SettlementRecorded", Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		payableError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *catalogueHandlers) listSettlements(c *gin.Context, params pagination.Params) ([]settlement, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := catalogueID(c)
	if !ok {
		return nil, nil
	}
	var rows []settlement
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&payable{}).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return gorm.ErrRecordNotFound
		}
		return tenantdb.ApplyKeyset(tx, params).Select(settlementColumns).Where("tenant_id=? AND outlet_id=? AND payable_id=?", p.TenantID, outlet, id).Find(&rows).Error
	})
	if err != nil {
		payableError(c, err)
	}
	return rows, nil
}
