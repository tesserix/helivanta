package pharmacy

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type pharmacyEmployerGuard struct{ db *tenantdb.DB }

// PharmacyEmployers exposes employer-eligible organizations without sharing a repository.
func (*Module) PharmacyEmployers(db *tenantdb.DB) authz.PharmacyEmployerGuard {
	return &pharmacyEmployerGuard{db: db}
}

func (g *pharmacyEmployerGuard) CurrentPharmacyEmployer(ctx context.Context, tenant string, pharmacyID uuid.UUID) (authz.PharmacyEmployer, error) {
	parsed, err := uuid.Parse(tenant)
	// Outside the caller's guarded command the key-share lock would end before the caller writes.
	if err != nil || parsed == uuid.Nil || parsed.String() != tenant || g.db == nil || !g.db.GuardedCommandActive(ctx) {
		return authz.PharmacyEmployer{}, authz.ErrScopedAuthorizationUnavailable
	}
	if pharmacyID.Version() != 7 {
		return authz.PharmacyEmployer{}, authz.ErrPharmacyEmployerHidden
	}
	var row struct {
		ID            uuid.UUID
		Name          string
		OwnershipType string
	}
	err = g.db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) error {
		return tx.Table("pharmacy_organizations").Select("id,name,ownership_type").
			Where("tenant_id=? AND id=?", tenant, pharmacyID).
			Clauses(clause.Locking{Strength: "KEY SHARE"}).Take(&row).Error
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return authz.PharmacyEmployer{}, authz.ErrPharmacyEmployerHidden
	case err != nil:
		return authz.PharmacyEmployer{}, errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	case row.OwnershipType != "independent" && row.OwnershipType != "chain":
		// A hospital-owned outlet's staff belong to its hospital, so it is never a separate employer.
		return authz.PharmacyEmployer{}, authz.ErrPharmacyEmployerHidden
	}
	return authz.PharmacyEmployer{PharmacyID: row.ID, Name: row.Name, OwnershipType: row.OwnershipType}, nil
}
