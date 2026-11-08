package pharmacy

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

func (g tradingEndpointGuard) WithHospitalDepartment(ctx context.Context, e suppliertrade.Endpoint, department uuid.UUID, action func(context.Context) error) error {
	if g.db == nil || g.departments == nil || action == nil || !g.db.GuardedCommandActive(ctx) {
		return suppliertrade.ErrUnavailable
	}
	if e.Product != "pharmacy" || e.TenantID == uuid.Nil || e.OrganizationID.Version() != 7 || e.LocationID.Version() != 7 || e.HospitalID == nil || *e.HospitalID == uuid.Nil || e.BindingID == nil || e.BindingID.Version() != 7 || e.BindingVersion == nil || *e.BindingVersion < 1 || department.Version() != 7 || department.Variant() != uuid.RFC4122 {
		return suppliertrade.ErrIneligible
	}
	var revision uuid.UUID
	err := g.db.WithTenantCommand(ctx, e.TenantID.String(), func(tx *gorm.DB) (err error) {
		var previous string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.pharmacy_trade_source','app.pharmacy_trade_outlet','app.pharmacy_trade_binding','app.pharmacy_trade_hospital'])AS key`).Scan(&previous).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
		}()
		if err = tx.Exec(`SELECT set_config('app.pharmacy_trade_source','current',true),set_config('app.pharmacy_trade_outlet',?,true),set_config('app.pharmacy_trade_binding',?,true),set_config('app.pharmacy_trade_hospital',?,true)`, e.LocationID.String(), e.BindingID.String(), e.HospitalID.String()).Error; err != nil {
			return err
		}
		err = tx.Raw(`SELECT department_revision_id FROM pharmacy_staff_department_bindings WHERE tenant_id=? AND pharmacy_id=? AND outlet_id=? AND id=? AND hospital_id=? AND department_id=? AND version=? AND status='approved'FOR SHARE`, e.TenantID, e.OrganizationID, e.LocationID, *e.BindingID, *e.HospitalID, department, *e.BindingVersion).Row().Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			return suppliertrade.ErrIneligible
		}
		return err
	})
	if err != nil {
		if errors.Is(err, suppliertrade.ErrIneligible) {
			return err
		}
		return errors.Join(suppliertrade.ErrUnavailable, err)
	}
	err = g.departments.WithCurrentDepartment(ctx, authz.DepartmentReference{TenantID: e.TenantID.String(), HospitalID: *e.HospitalID, DepartmentID: department, DepartmentRevision: revision}, action)
	if errors.Is(err, authz.ErrDepartmentReferenceHidden) || errors.Is(err, authz.ErrDepartmentReferenceChanged) {
		return suppliertrade.ErrIneligible
	}
	if err != nil {
		return errors.Join(suppliertrade.ErrUnavailable, err)
	}
	return nil
}
