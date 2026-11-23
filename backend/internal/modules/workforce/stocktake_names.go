package workforce

import (
	"context"
	"errors"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (g *stocktakeActorGuard) StocktakeHospitalName(ctx context.Context, scope authz.StaffActorScope) (string, error) {
	checks, err := authz.StocktakeReadScopeChecks(ctx, scope)
	if err != nil || g == nil || g.actors == nil || g.actors.db == nil || tenantdb.ActorFrom(ctx) != scope.Subject {
		return "", authz.ErrScopedAuthorizationUnavailable
	}
	var name string
	err = g.WithCurrentStocktakeActor(ctx, scope, func(actorCtx context.Context, actor authz.StaffActorReference) error {
		return g.actors.db.WithTenantCommand(actorCtx, scope.TenantID, func(tx *gorm.DB) error {
			var row struct{ Name string }
			if err := tx.Table("workforce_hospitals").Select("name").Where("tenant_id=? AND id=?", scope.TenantID, scope.HospitalID).Clauses(clause.Locking{Strength: "SHARE"}).Take(&row).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)VALUES(?,?,?,'workforce.stocktake_hospital_name.read',?,jsonb_build_object('read_assignment_id',?::uuid,'read_assignment_version',?::bigint,'source_assignment_id',?::uuid,'source_assignment_version',?::bigint,'actor_link_id',?::uuid,'actor_link_version',?::bigint))`, scope.TenantID, scope.HospitalID, scope.Subject, scope.HospitalID, checks[0].Grant.AssignmentID, checks[0].Grant.Version, checks[1].Grant.AssignmentID, checks[1].Grant.Version, actor.LinkID, actor.LinkVersion).Error; err != nil {
				return err
			}
			name = row.Name
			return nil
		})
	})
	if err != nil {
		return "", errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	return name, nil
}
