package supply

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

type tradingEndpointGuard struct{ db *tenantdb.DB }

func (*Module) TradingEndpoints(db *tenantdb.DB, _ authz.DepartmentReferenceGuard) suppliertrade.CurrentEndpointGuard {
	return tradingEndpointGuard{db: db}
}
func (g tradingEndpointGuard) WithCurrentTradingEndpoint(ctx context.Context, endpoint suppliertrade.Endpoint, category, operation string, action func(context.Context, suppliertrade.EndpointReference) error) error {
	if g.db == nil || !g.db.GuardedCommandActive(ctx) || action == nil {
		return suppliertrade.ErrUnavailable
	}
	if !suppliertrade.ValidEndpointUse(endpoint, category, operation) || endpoint.Product != "supply" || endpoint.HospitalID != nil || endpoint.BindingID != nil || endpoint.BindingVersion != nil {
		return suppliertrade.ErrIneligible
	}
	ref := suppliertrade.EndpointReference{Endpoint: endpoint, Category: category, Operation: operation}
	err := g.db.WithTenantCommand(ctx, endpoint.TenantID.String(), func(tx *gorm.DB) (err error) {
		keys := []string{"app.supply_employer_check", "app.supply_operating_reference", "app.supply_reference_party", "app.supply_reference_premise", "app.supply_trade_source", "app.supply_trade_party", "app.supply_trade_licence"}
		rawKeys, err := json.Marshal(keys)
		if err != nil {
			return err
		}
		var previous string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM jsonb_array_elements_text(?::jsonb)AS key`, string(rawKeys)).Scan(&previous).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
		}()
		if err = tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0)),set_config('app.supply_employer_check','true',true),set_config('app.supply_operating_reference','true',true),set_config('app.supply_reference_party',?,true),set_config('app.supply_reference_premise',?,true),set_config('app.supply_trade_source','current',true),set_config('app.supply_trade_party',?,true),set_config('app.supply_trade_licence',?,true)`, endpoint.TenantID.String()+":supply-license:"+endpoint.OrganizationID.String()+":"+endpoint.LicenceID.String(), endpoint.OrganizationID.String(), endpoint.LocationID.String(), endpoint.OrganizationID.String(), endpoint.LicenceID.String()).Error; err != nil {
			return err
		}
		err = tx.Raw(`SELECT r.id,r.version FROM supply_parties p JOIN supply_premises m ON(m.tenant_id,m.party_id)=(p.tenant_id,p.id)JOIN supply_licenses l ON(l.tenant_id,l.party_id,l.premise_id)=(m.tenant_id,m.party_id,m.id)JOIN supply_license_reviews r ON(r.tenant_id,r.party_id,r.license_id)=(l.tenant_id,l.party_id,l.id)JOIN supply_license_evidence e ON(e.tenant_id,e.party_id,e.license_id,e.id)=(r.tenant_id,r.party_id,r.license_id,r.evidence_id)WHERE p.tenant_id=? AND p.id=? AND p.version=? AND p.record_kind='owned_organisation'AND p.country_code='IN'AND p.jurisdiction=? AND m.id=? AND m.version=? AND m.region=? AND m.country_code='IN'AND m.status='active'AND m.kind IN('warehouse','depot')AND l.id=? AND l.version=? AND l.jurisdiction=? AND l.country_code='IN'AND jsonb_exists(l.product_categories,?)AND(timezone(m.time_zone,CURRENT_TIMESTAMP))::date BETWEEN l.issued_on AND l.expires_on AND r.version=(SELECT max(v.version)FROM supply_license_reviews v WHERE v.tenant_id=l.tenant_id AND v.party_id=l.party_id AND v.license_id=l.id)AND r.status='verified'AND r.party_version=p.version AND r.premise_version=m.version AND r.license_version=l.version AND e.status='available'AND e.license_version=l.version FOR SHARE OF p,m,l,r,e`, endpoint.TenantID, endpoint.OrganizationID, endpoint.OrganizationVersion, endpoint.Jurisdiction, endpoint.LocationID, endpoint.LocationVersion, endpoint.Jurisdiction, endpoint.LicenceID, endpoint.LicenceVersion, endpoint.Jurisdiction, category).Row().Scan(&ref.LicenceReviewID, &ref.LicenceReviewVersion)
		if errors.Is(err, sql.ErrNoRows) {
			return suppliertrade.ErrIneligible
		}
		if err != nil {
			return err
		}
		decision, err := tradingpolicy.Check(tx, tradingpolicy.Scope{Tenant: endpoint.TenantID.String(), Country: "IN", Jurisdiction: endpoint.Jurisdiction, Category: category, Operation: operation, Currency: "INR"}, time.Now())
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return suppliertrade.ErrIneligible
		}
		ref.PolicyVersion, err = uuid.Parse(decision.PolicyVersion)
		return err
	})
	if errors.Is(err, suppliertrade.ErrIneligible) {
		return err
	}
	if err != nil {
		return errors.Join(suppliertrade.ErrUnavailable, err)
	}
	return action(ctx, ref)
}

func (g tradingEndpointGuard) WithTradingAdministration(ctx context.Context, p authn.Principal, e suppliertrade.Endpoint, write bool, action func(context.Context) error) error {
	if g.db == nil || !g.db.GuardedCommandActive(ctx) || action == nil {
		return suppliertrade.ErrUnavailable
	}
	if p.TenantID != e.TenantID.String() || p.Subject == "" || tenantdb.ActorFrom(ctx) != p.Subject || e.Product != "supply" {
		return suppliertrade.ErrIneligible
	}
	return g.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) (err error) {
		keys, _ := json.Marshal([]string{"app.supply_subject", "app.supply_party", "app.supply_directory", "app.supply_manager"})
		var prior string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM jsonb_array_elements_text(?::jsonb)AS key`, string(keys)).Scan(&prior).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, prior).Error)
		}()
		if err = tx.Exec(`SELECT set_config('app.supply_subject',?,true),set_config('app.supply_party',?,true),set_config('app.supply_directory','off',true),set_config('app.supply_manager','off',true)`, p.Subject, e.OrganizationID.String()).Error; err != nil {
			return err
		}
		if err = lockStockAccess(tx, p.TenantID, e.OrganizationID); err != nil {
			return err
		}
		roles := registryReaders
		if write {
			roles = []string{"administrator"}
		}
		if err = requireRoles(tx, p, e.OrganizationID, roles); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return suppliertrade.ErrIneligible
			}
			return err
		}
		var location uuid.UUID
		err = tx.Raw(`SELECT m.id FROM supply_parties p JOIN supply_premises m ON(m.tenant_id,m.party_id)=(p.tenant_id,p.id)WHERE p.tenant_id=? AND p.id=? AND p.record_kind='owned_organisation'AND m.id=? FOR SHARE OF p,m`, p.TenantID, e.OrganizationID, e.LocationID).Row().Scan(&location)
		if errors.Is(err, sql.ErrNoRows) {
			return suppliertrade.ErrIneligible
		}
		if err != nil {
			return err
		}
		// Restore only the private product lens; the credential/resource locks live to the outer commit.
		if err = tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, prior).Error; err != nil {
			return err
		}
		return action(ctx)
	})
}
