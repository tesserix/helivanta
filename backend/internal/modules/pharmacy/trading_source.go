package pharmacy

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/tradingpolicy"
	"gorm.io/gorm"
	"time"
)

//go:embed trading_source.sql
var tradingSourceSchema string

type tradingEndpointGuard struct {
	db          *tenantdb.DB
	departments authz.DepartmentReferenceGuard
}

func (*Module) TradingEndpoints(db *tenantdb.DB, departments authz.DepartmentReferenceGuard) suppliertrade.CurrentEndpointGuard {
	return tradingEndpointGuard{db: db, departments: departments}
}
func (g tradingEndpointGuard) WithCurrentTradingEndpoint(ctx context.Context, e suppliertrade.Endpoint, category, operation string, action func(context.Context, suppliertrade.EndpointReference) error) error {
	if g.db == nil || !g.db.GuardedCommandActive(ctx) || action == nil {
		return suppliertrade.ErrUnavailable
	}
	if !suppliertrade.ValidEndpointUse(e, category, operation) || e.Product != "pharmacy" || e.OrganizationVersion != 1 || e.LocationVersion != 1 || e.LicenceVersion != 1 {
		return suppliertrade.ErrIneligible
	}
	if e.HospitalID == nil && (e.BindingID != nil || e.BindingVersion != nil) {
		return suppliertrade.ErrIneligible
	}
	if e.HospitalID != nil && (*e.HospitalID == uuid.Nil || e.BindingID == nil || e.BindingVersion == nil || *e.BindingVersion < 1) {
		return suppliertrade.ErrIneligible
	}
	ref := suppliertrade.EndpointReference{Endpoint: e, Category: category, Operation: operation, LicenceReviewVersion: 1}
	var department, revision uuid.UUID
	err := g.db.WithTenantCommand(ctx, e.TenantID.String(), func(tx *gorm.DB) (err error) {
		keys, _ := json.Marshal([]string{"app.pharmacy_trade_source", "app.pharmacy_trade_outlet", "app.pharmacy_trade_registration", "app.pharmacy_trade_licence", "app.pharmacy_trade_binding", "app.pharmacy_trade_hospital"})
		var prior string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM jsonb_array_elements_text(?::jsonb)AS key`, string(keys)).Scan(&prior).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, prior).Error)
		}()
		hospital, binding := "", ""
		if e.HospitalID != nil {
			hospital = e.HospitalID.String()
			binding = e.BindingID.String()
		}
		if err = tx.Exec(`SELECT set_config('app.pharmacy_trade_source','current',true),set_config('app.pharmacy_trade_outlet',?,true),set_config('app.pharmacy_trade_registration',?,true),set_config('app.pharmacy_trade_licence',?,true),set_config('app.pharmacy_trade_binding',?,true),set_config('app.pharmacy_trade_hospital',?,true)`, e.LocationID.String(), e.RegistrationID.String(), e.LicenceID.String(), binding, hospital).Error; err != nil {
			return err
		}
		var ownership string
		err = tx.Raw(`SELECT p.ownership_type FROM pharmacy_organizations p JOIN pharmacy_outlets o ON(o.tenant_id,o.pharmacy_id)=(p.tenant_id,p.id)JOIN pharmacy_trading_scopes s ON(s.tenant_id,s.pharmacy_id,s.outlet_id)=(o.tenant_id,o.pharmacy_id,o.id)WHERE p.tenant_id=? AND p.id=? AND o.id=? AND o.country_code='IN'AND s.id=? AND s.status='approved'AND s.version=? AND s.input->>'licence_id'=? AND s.input->>'jurisdiction'=? AND s.input->>'category'=? FOR SHARE OF p,o,s`, e.TenantID, e.OrganizationID, e.LocationID, *e.RegistrationID, *e.RegistrationVersion, e.LicenceID.String(), e.Jurisdiction, category).Row().Scan(&ownership)
		if errors.Is(err, sql.ErrNoRows) {
			return suppliertrade.ErrIneligible
		}
		if err != nil {
			return err
		}
		if (e.HospitalID == nil && ownership != "independent" && ownership != "chain") || (e.HospitalID != nil && ownership != "hospital_owned") {
			return suppliertrade.ErrIneligible
		}
		if e.HospitalID != nil {
			if g.departments == nil {
				return suppliertrade.ErrUnavailable
			}
			err = tx.Raw(`SELECT department_id,department_revision_id FROM pharmacy_staff_department_bindings WHERE tenant_id=? AND pharmacy_id=? AND outlet_id=? AND id=? AND hospital_id=? AND version=? AND status='approved'FOR SHARE`, e.TenantID, e.OrganizationID, e.LocationID, *e.BindingID, *e.HospitalID, *e.BindingVersion).Row().Scan(&department, &revision)
			if errors.Is(err, sql.ErrNoRows) {
				return suppliertrade.ErrIneligible
			}
			if err != nil {
				return err
			}
		}
		if err = tx.Raw(`SELECT review_id FROM pharmacy_trading_licence_source(?,?,?)`, e.TenantID, e.LocationID, e.LicenceID).Row().Scan(&ref.LicenceReviewID); errors.Is(err, sql.ErrNoRows) {
			return suppliertrade.ErrIneligible
		} else if err != nil {
			return err
		}
		policy, err := tradingpolicy.Check(tx, tradingpolicy.Scope{Tenant: e.TenantID.String(), Country: "IN", Jurisdiction: e.Jurisdiction, Category: category, Operation: operation, Currency: "INR"}, time.Now())
		if err != nil {
			return err
		}
		if !policy.Allowed {
			return suppliertrade.ErrIneligible
		}
		ref.PolicyVersion, err = uuid.Parse(policy.PolicyVersion)
		return err
	})
	if errors.Is(err, suppliertrade.ErrIneligible) || errors.Is(err, suppliertrade.ErrUnavailable) {
		return err
	}
	if err != nil {
		return errors.Join(suppliertrade.ErrUnavailable, err)
	}
	if e.HospitalID != nil {
		err = g.departments.WithCurrentDepartment(ctx, authz.DepartmentReference{TenantID: e.TenantID.String(), HospitalID: *e.HospitalID, DepartmentID: department, DepartmentRevision: revision}, func(ctx context.Context) error { return action(ctx, ref) })
		if errors.Is(err, authz.ErrDepartmentReferenceChanged) || errors.Is(err, authz.ErrDepartmentReferenceHidden) {
			return suppliertrade.ErrIneligible
		}
		if err != nil {
			return errors.Join(suppliertrade.ErrUnavailable, err)
		}
		return nil
	}
	return action(ctx, ref)
}

func (g tradingEndpointGuard) WithTradingAdministration(ctx context.Context, p authn.Principal, e suppliertrade.Endpoint, write bool, action func(context.Context) error) error {
	if g.db == nil || !g.db.GuardedCommandActive(ctx) || action == nil {
		return suppliertrade.ErrUnavailable
	}
	if p.TenantID != e.TenantID.String() || p.Subject == "" || tenantdb.ActorFrom(ctx) != p.Subject || e.Product != "pharmacy" {
		return suppliertrade.ErrIneligible
	}
	return g.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) (err error) {
		keys, _ := json.Marshal([]string{"app.pharmacy_subject", "app.pharmacy_outlet", "app.pharmacy_id"})
		var prior string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM jsonb_array_elements_text(?::jsonb)AS key`, string(keys)).Scan(&prior).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, prior).Error)
		}()
		level, err := outletAccess(tx, p, e.LocationID, e.OrganizationID, true)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return suppliertrade.ErrIneligible
		}
		if err != nil {
			return err
		}
		if write && level != "editor" {
			return suppliertrade.ErrIneligible
		}
		if err = tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, prior).Error; err != nil {
			return err
		}
		return action(ctx)
	})
}
