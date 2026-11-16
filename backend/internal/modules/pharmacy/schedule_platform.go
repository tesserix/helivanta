package pharmacy

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Helivanta's decision on a request to lower a schedule class (#1681). It
// holds no authority of its own: cmd/pharmacy-schedule-review connects as
// helivanta_drug_approver, and the database refuses a decision from any
// other role, on a declaration that does not lower the class, without the
// owner's countersign, or by the declarer or countersigner.

// ErrLoweringNotWaiting means the declaration is not a countersigned
// lowering awaiting a decision.
var ErrLoweringNotWaiting = errors.New("no countersigned lowering awaits a decision under this id")

// ScheduleLowering is one countersigned request awaiting Helivanta.
type ScheduleLowering struct {
	TenantID          uuid.UUID `gorm:"column:tenant_id"`
	OutletID          uuid.UUID `gorm:"column:outlet_id"`
	ProductID         uuid.UUID `gorm:"column:product_id"`
	DeclarationID     uuid.UUID `gorm:"column:declaration_id"`
	ProductName       string    `gorm:"column:product_name"`
	CurrentClass      string    `gorm:"column:current_class"`
	CurrentNDPS       bool      `gorm:"column:current_ndps"`
	RequestedClass    string    `gorm:"column:requested_class"`
	RequestedNDPS     bool      `gorm:"column:requested_ndps"`
	Source            string    `gorm:"column:source"`
	DeclaredBy        string    `gorm:"column:declared_by"`
	DeclaredAt        time.Time `gorm:"column:declared_at"`
	CountersignedBy   string    `gorm:"column:countersigned_by"`
	CountersignReason string    `gorm:"column:countersign_reason"`
}

// loweringListLimit bounds one listing; requests are rare and decided
// oldest first.
const loweringListLimit = 200

// ListScheduleLowerings returns countersigned lowerings awaiting a
// decision, oldest first. db must connect as helivanta_drug_approver.
func ListScheduleLowerings(ctx context.Context, db *tenantdb.DB) ([]ScheduleLowering, error) {
	out := []ScheduleLowering{}
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT d.tenant_id, d.outlet_id, d.product_id, d.id AS declaration_id, p.brand_name AS product_name,
			a.regulatory_class AS current_class, a.ndps AS current_ndps, d.regulatory_class AS requested_class, d.ndps AS requested_ndps,
			d.source, d.declared_by, d.created_at AS declared_at, c.countersigned_by, c.reason AS countersign_reason
			FROM pharmacy_product_schedule_declarations d
			JOIN pharmacy_product_schedule_countersigns c ON c.tenant_id=d.tenant_id AND c.outlet_id=d.outlet_id AND c.declaration_id=d.id
			JOIN pharmacy_catalogue_products p ON p.tenant_id=d.tenant_id AND p.outlet_id=d.outlet_id AND p.id=d.product_id
			LEFT JOIN LATERAL pharmacy_schedule_last_approved(d.tenant_id, d.outlet_id, d.product_id) a ON true
			WHERE d.lowers AND NOT EXISTS (SELECT 1 FROM pharmacy_product_schedule_reviews r
				WHERE r.tenant_id=d.tenant_id AND r.outlet_id=d.outlet_id AND r.declaration_id=d.id)
			ORDER BY d.seq LIMIT ?`, loweringListLimit).Scan(&out).Error
	})
	return out, err
}

// DecideScheduleLowering records Helivanta's decision. by names the person
// deciding; the audit trail records them as "helivanta:<by>". db must
// connect as helivanta_drug_approver.
func DecideScheduleLowering(ctx context.Context, db *tenantdb.DB, declaration uuid.UUID, by string, approve bool, reason string) error {
	by, reason = strings.TrimSpace(by), strings.TrimSpace(reason)
	if by == "" || len(by) > 200 || reason == "" || len(reason) > 500 {
		return errors.New("a decision names who decided and a reason of at most 500 characters")
	}
	var target struct {
		TenantID uuid.UUID `gorm:"column:tenant_id"`
		OutletID uuid.UUID `gorm:"column:outlet_id"`
	}
	if err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT d.tenant_id, d.outlet_id FROM pharmacy_product_schedule_declarations d
			JOIN pharmacy_product_schedule_countersigns c ON c.tenant_id=d.tenant_id AND c.outlet_id=d.outlet_id AND c.declaration_id=d.id
			WHERE d.id=? AND d.lowers`, declaration).Scan(&target).Error
	}); err != nil {
		return err
	}
	if target.TenantID == uuid.Nil {
		return ErrLoweringNotWaiting
	}
	decision := "rejected"
	if approve {
		decision = "approved"
	}
	return db.WithTenant(tenantdb.WithActor(ctx, "helivanta:"+by), target.TenantID.String(), func(tx *gorm.DB) error {
		insert := tx.Exec(`INSERT INTO pharmacy_product_schedule_reviews (tenant_id, outlet_id, declaration_id, decision, reason, reviewed_by)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, target.TenantID, target.OutletID, declaration, decision, reason, by)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return ErrLoweringNotWaiting
		}
		return nil
	})
}
