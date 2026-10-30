package billing

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/pkg/events"
)

// Billing's record of MediCore's discharge orders (#197 slice 4): the final
// bill waits for one. Ordered and withdrawn may arrive in either order; each
// sets its own half of the row once, and a replay that differs is refused.

func dischargeConsumers() []events.Consumer {
	return []events.Consumer{
		{Name: "billing-discharge-ordered", Subject: medicorecontract.SubjectDischargeOrdered, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			var d medicorecontract.DischargeOrderedData
			if evt.Type != "DischargeOrdered" || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
				return errors.New("invalid discharge order event")
			}
			v, ok := ids(d.OrderID, d.HospitalID, d.AdmissionID, d.PatientID)
			at, err := time.Parse(time.RFC3339Nano, d.OrderedAt)
			if !ok || err != nil || (d.Disposition != "home" && d.Disposition != "transfer_out") {
				return errors.New("invalid discharge order event")
			}
			if err := lockAdmission(tx, evt.TenantID, v[2]); err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO billing_discharge_orders (tenant_id,order_id,hospital_id,admission_id,disposition,ordered_at) VALUES (?,?,?,?,?,?)
 ON CONFLICT (tenant_id,order_id) DO UPDATE SET disposition=EXCLUDED.disposition,ordered_at=EXCLUDED.ordered_at WHERE billing_discharge_orders.ordered_at IS NULL`,
				evt.TenantID, v[0], v[1], v[2], d.Disposition, at).Error; err != nil {
				return err
			}
			if err := sameDischargeOrder(tx, evt.TenantID, v[0], v[1], v[2], func(o heldOrder) bool {
				return o.Disposition != nil && *o.Disposition == d.Disposition && o.OrderedAt != nil && o.OrderedAt.Equal(at.Truncate(time.Microsecond))
			}); err != nil {
				return err
			}
			// Only an order that is accepted opens the account.
			return openAccount(tx, evt.TenantID, v[1], v[2], v[3])
		}},
		{Name: "billing-discharge-withdrawn", Subject: medicorecontract.SubjectDischargeWithdrawn, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			var d medicorecontract.DischargeWithdrawnData
			if evt.Type != "DischargeWithdrawn" || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
				return errors.New("invalid discharge withdrawal event")
			}
			v, ok := ids(d.OrderID, d.HospitalID, d.AdmissionID)
			at, err := time.Parse(time.RFC3339Nano, d.WithdrawnAt)
			if !ok || err != nil {
				return errors.New("invalid discharge withdrawal event")
			}
			if err := lockAdmission(tx, evt.TenantID, v[2]); err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO billing_discharge_orders (tenant_id,order_id,hospital_id,admission_id,withdrawn_at) VALUES (?,?,?,?,?)
 ON CONFLICT (tenant_id,order_id) DO UPDATE SET withdrawn_at=EXCLUDED.withdrawn_at WHERE billing_discharge_orders.withdrawn_at IS NULL`,
				evt.TenantID, v[0], v[1], v[2], at).Error; err != nil {
				return err
			}
			return sameDischargeOrder(tx, evt.TenantID, v[0], v[1], v[2], func(o heldOrder) bool {
				return o.WithdrawnAt != nil && o.WithdrawnAt.Equal(at.Truncate(time.Microsecond))
			})
		}},
	}
}

type heldOrder struct {
	HospitalID, AdmissionID uuid.UUID
	Disposition             *string
	OrderedAt, WithdrawnAt  *time.Time
}

// sameDischargeOrder refuses an event that names the order with another
// admission, or a replay that says something else than what was recorded.
func sameDischargeOrder(tx *gorm.DB, tenant string, order, hospital, admission uuid.UUID, same func(heldOrder) bool) error {
	var held heldOrder
	if err := tx.Raw(`SELECT hospital_id,admission_id,disposition,ordered_at,withdrawn_at FROM billing_discharge_orders WHERE tenant_id=? AND order_id=?`, tenant, order).Scan(&held).Error; err != nil {
		return err
	}
	if held.HospitalID != hospital || held.AdmissionID != admission || !same(held) {
		return errors.New("a discharge order event differs from the one recorded")
	}
	return nil
}
