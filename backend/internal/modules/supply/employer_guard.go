package supply

import (
	"context"
	_ "embed"
	"errors"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed employer_guard.sql
var employerGuardSchema string

type supplierEmployerGuard struct{ db *tenantdb.DB }

func (*Module) SupplierEmployers(db *tenantdb.DB) authz.SupplierEmployerGuard {
	return &supplierEmployerGuard{db: db}
}
func (g *supplierEmployerGuard) CurrentSupplierEmployer(ctx context.Context, tenant string, id uuid.UUID) (authz.SupplierEmployer, error) {
	parsed, err := uuid.Parse(tenant)
	if err != nil || parsed == uuid.Nil || parsed.String() != tenant || g.db == nil || !g.db.GuardedCommandActive(ctx) {
		return authz.SupplierEmployer{}, authz.ErrScopedAuthorizationUnavailable
	}
	if id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		return authz.SupplierEmployer{}, authz.ErrSupplierEmployerHidden
	}
	var source struct {
		ID      uuid.UUID
		Name    string
		Version int64
	}
	err = g.db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) (err error) {
		var previous string
		if err = tx.Raw(`SELECT COALESCE(current_setting('app.supply_employer_check',true),'')`).Scan(&previous).Error; err != nil {
			return err
		}
		if err = tx.Exec(`SELECT set_config('app.supply_employer_check','true',true)`).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config('app.supply_employer_check',?,true)`, previous).Error)
		}()
		return tx.Table("supply_parties").Select("id,legal_name AS name,version").Where("tenant_id=? AND id=? AND record_kind='owned_organisation' AND country_code='IN'", tenant, id).Clauses(clause.Locking{Strength: "SHARE"}).Take(&source).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authz.SupplierEmployer{}, authz.ErrSupplierEmployerHidden
	}
	if err != nil {
		return authz.SupplierEmployer{}, errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	return authz.SupplierEmployer{SupplierID: source.ID, Name: source.Name, SourceVersion: source.Version}, nil
}
