package supplierstock

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"gorm.io/gorm"
)

// Scope is derived by the module from its verified operating/hospital publication guard.
type Scope struct {
	TenantID, OrganizationID, LocationID, AllocationID, OrderID, StaffID uuid.UUID
	Subject, Mode                                                        string
}

// Frame restores all nested owner settings even when a stock command fails.
func (s Store) Frame(tx *gorm.DB, scope Scope, action func() error) (err error) {
	if s.validate() != nil || scope.TenantID == uuid.Nil || scope.OrganizationID == uuid.Nil || scope.LocationID == uuid.Nil || scope.Subject == "" || action == nil {
		return f.ErrInvalid
	}
	if scope.Mode != "operator" && scope.Mode != "publish" && scope.Mode != "cancel" && scope.Mode != "safety" {
		return f.ErrInvalid
	}
	if scope.Mode == "operator" && scope.StaffID == uuid.Nil {
		return f.ErrUnavailable
	}
	values := map[string]string{"app.supplier_stock_owner": string(s.Owner), "app.supplier_stock_organization": scope.OrganizationID.String(), "app.supplier_stock_location": scope.LocationID.String(), "app.supplier_stock_allocation": scope.AllocationID.String(), "app.supplier_stock_order": scope.OrderID.String(), "app.supplier_stock_staff": scope.StaffID.String(), "app.supplier_stock_mode": scope.Mode, "app.actor_subject": scope.Subject}
	old := map[string]string{}
	for key := range values {
		var v string
		if err := tx.Raw(`SELECT COALESCE(current_setting(?,true),'')`, key).Scan(&v).Error; err != nil {
			return err
		}
		old[key] = v
	}
	set := func(m map[string]string) error {
		raw, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return tx.Exec(`SELECT set_config(key,value,true) FROM jsonb_each_text(?::jsonb)`, string(raw)).Error
	}
	if err := set(values); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, set(old)) }()
	return action()
}
