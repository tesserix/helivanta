package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
)

// Release from quarantine (#1386 A3): the only way stock becomes sellable.

// errNotReleasable names why a batch cannot be released; the message is
// shown to the pharmacist as is.
type errNotReleasable struct{ reason string }

func (e errNotReleasable) Error() string { return e.reason }

type releaseInput struct {
	BatchID        uuid.UUID `json:"batch_id" binding:"required"`
	FromLocationID uuid.UUID `json:"from_location_id" binding:"required"`
	ToLocationID   uuid.UUID `json:"to_location_id" binding:"required"`
	QuantityUnits  int64     `json:"quantity_units" binding:"required,min=1,max=1000000000"`
	Reason         string    `json:"reason" binding:"required,max=500"`
}

type stockRelease struct {
	ID              uuid.UUID `gorm:"column:id" json:"id"`
	BatchID         uuid.UUID `gorm:"column:batch_id" json:"batch_id"`
	FromLocationID  uuid.UUID `gorm:"column:from_location_id" json:"from_location_id"`
	ToLocationID    uuid.UUID `gorm:"column:to_location_id" json:"to_location_id"`
	QuantityUnits   int64     `gorm:"column:quantity_units" json:"quantity_units"`
	Reason          string    `gorm:"column:reason" json:"reason"`
	RegulatoryClass string    `gorm:"column:regulatory_class" json:"regulatory_class"`
	ActorSubject    string    `gorm:"column:actor_subject" json:"actor_subject"`
	CreatedAt       time.Time `gorm:"column:created_at" json:"created_at"`
}

const releaseColumns = "id,batch_id,from_location_id,to_location_id,quantity_units,reason,regulatory_class,actor_subject,created_at"

func (in releaseInput) digest() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{in.BatchID.String(), in.FromLocationID.String(), in.ToLocationID.String(),
		fmt.Sprint(in.QuantityUnits), in.Reason}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// releasable checks everything a batch must satisfy before it can be sold,
// on the outlet's date, and returns the class it is released under. Each
// refusal says why (plan review focus 4).
func releasable(tx *gorm.DB, tenant string, outlet uuid.UUID, in releaseInput) (string, uuid.UUID, error) {
	var locations []struct {
		ID          uuid.UUID
		StockStatus string
	}
	if err := tx.Raw(`SELECT id, stock_status FROM pharmacy_stock_locations WHERE tenant_id=? AND outlet_id=? AND id IN ?`,
		tenant, outlet, []uuid.UUID{in.FromLocationID, in.ToLocationID}).Scan(&locations).Error; err != nil {
		return "", uuid.Nil, err
	}
	statuses := map[uuid.UUID]string{}
	for _, l := range locations {
		statuses[l.ID] = l.StockStatus
	}
	if len(statuses) != 2 {
		return "", uuid.Nil, gorm.ErrRecordNotFound
	}
	if statuses[in.FromLocationID] != "quarantine" || statuses[in.ToLocationID] != "sellable" {
		return "", uuid.Nil, errNotReleasable{"a release moves stock from a quarantine location to a sellable one"}
	}
	var batch struct {
		ProductID uuid.UUID
		ExpiresOn string
		MRPMinor  *int64 `gorm:"column:mrp_minor"`
	}
	if err := tx.Raw(`SELECT product_id, expires_on::text AS expires_on, mrp_minor FROM pharmacy_stock_batches WHERE tenant_id=? AND outlet_id=? AND id=?`,
		tenant, outlet, in.BatchID).Scan(&batch).Error; err != nil {
		return "", uuid.Nil, err
	}
	if batch.ProductID == uuid.Nil {
		return "", uuid.Nil, gorm.ErrRecordNotFound
	}
	today, err := outletLocalDate(tx, tenant, outlet)
	if err != nil {
		return "", uuid.Nil, err
	}
	if batch.ExpiresOn <= today {
		return "", uuid.Nil, errNotReleasable{"the batch expires on " + batch.ExpiresOn + ", which is not after today"}
	}
	if batch.MRPMinor == nil {
		return "", uuid.Nil, errNotReleasable{"the batch has no MRP recorded; receive it again with its printed MRP"}
	}
	var incidents int64
	if err := tx.Raw(`SELECT count(*) FROM pharmacy_batch_safety_incidents WHERE tenant_id=? AND outlet_id=? AND batch_id=?`,
		tenant, outlet, in.BatchID).Scan(&incidents).Error; err != nil {
		return "", uuid.Nil, err
	}
	// An incident has no resolution yet, so any incident holds the batch.
	if incidents > 0 {
		return "", uuid.Nil, errNotReleasable{"the batch has a recorded safety incident"}
	}
	product := catalogueProduct{}
	if err := tx.Raw(`SELECT id, status FROM pharmacy_catalogue_products WHERE tenant_id=? AND outlet_id=? AND id=? FOR SHARE`,
		tenant, outlet, batch.ProductID).Scan(&product).Error; err != nil {
		return "", uuid.Nil, err
	}
	if product.Status != "active" {
		return "", uuid.Nil, errNotReleasable{"the product is " + product.Status}
	}
	product.ID = batch.ProductID
	rows := []catalogueProduct{product}
	if err := attachTaxOn(tx, tenant, outlet, today, rows); err != nil {
		return "", uuid.Nil, err
	}
	if rows[0].Tax == nil {
		return "", uuid.Nil, errNotReleasable{"the product has no tax classification in force"}
	}
	var class []struct {
		DeclarationID   uuid.UUID
		RegulatoryClass string
		NDPS            bool
	}
	if err := tx.Raw(`SELECT declaration_id, regulatory_class, ndps FROM pharmacy_product_schedule_in_force(?, ?, ?)`,
		tenant, outlet, batch.ProductID).Scan(&class).Error; err != nil {
		return "", uuid.Nil, err
	}
	if len(class) == 0 {
		return "", uuid.Nil, errNotReleasable{"the product has no approved schedule class"}
	}
	// Schedule X and NDPS need registers and controls that do not exist yet
	// (#165, #171); releasing them would make stock nobody may sell.
	if class[0].RegulatoryClass == "X" || class[0].NDPS {
		return "", uuid.Nil, errNotReleasable{"Schedule X and NDPS medicines cannot be released until their controls exist"}
	}
	return class[0].RegulatoryClass, class[0].DeclarationID, nil
}

func (h *catalogueHandlers) putRelease(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in releaseInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || in.FromLocationID == in.ToLocationID {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a release needs a reason and two different locations")
		return
	}
	digest := in.digest()
	var result stockRelease
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":release:"+id.String()); err != nil {
			return err
		}
		var existing []struct{ IntentSHA256, ActorSubject string }
		if err := tx.Raw(`SELECT intent_sha256, actor_subject FROM pharmacy_stock_releases WHERE tenant_id=? AND outlet_id=? AND id=?`,
			p.TenantID, outlet, id).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 {
			if existing[0].IntentSHA256 != digest || existing[0].ActorSubject != p.Subject {
				return errStockIdentity
			}
			return tx.Raw(`SELECT `+releaseColumns+` FROM pharmacy_stock_releases WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, id).Scan(&result).Error
		}
		class, declaration, err := releasable(tx, p.TenantID, outlet, in)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO pharmacy_stock_releases (id, tenant_id, outlet_id, batch_id, from_location_id, to_location_id, quantity_units,
			reason, regulatory_class, schedule_declaration_id, intent_sha256, actor_subject) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, p.TenantID, outlet, in.BatchID, in.FromLocationID, in.ToLocationID, in.QuantityUnits, in.Reason, class, declaration, digest, p.Subject).Error; err != nil {
			return err
		}
		// Out of quarantine first: the bucket guard refuses an overdraft
		// before anything reaches the sellable location.
		for _, m := range []struct {
			location uuid.UUID
			quantity int64
			side     string
		}{{in.FromLocationID, -in.QuantityUnits, "out"}, {in.ToLocationID, in.QuantityUnits, "in"}} {
			movement := stockMovement{ID: uuid.Must(uuid.NewV7()), TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, ActorSubject: p.Subject,
				MovementFields: movementInput{LocationID: m.location, BatchID: in.BatchID, Kind: "release", QuantityUnits: m.quantity,
					Reason: in.Reason, SourceReference: "release:" + id.String() + ":" + m.side}}
			if err := tx.Create(&movement).Error; err != nil {
				return err
			}
			data, err := json.Marshal(pharmacycontract.StockMovementRecordedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(),
				MovementID: movement.ID.String(), LocationID: m.location.String(), BatchID: in.BatchID.String()})
			if err != nil {
				return err
			}
			if err := h.bus.Publish(tx, pharmacycontract.SubjectStockMovementRecorded, events.Event{Type: "StockMovementRecorded", Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
				return err
			}
		}
		return tx.Raw(`SELECT `+releaseColumns+` FROM pharmacy_stock_releases WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	var refusal errNotReleasable
	switch {
	case errors.As(err, &refusal):
		respond.Error(c, http.StatusConflict, "not_releasable", refusal.reason)
	case err != nil:
		stockError(c, err)
	default:
		respond.OK(c, result)
	}
}
