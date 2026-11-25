package workforce

import (
	"context"
	"errors"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Each guard discloses names only under its own purpose, so one actor purpose never reads for another.
func (g *stocktakeActorGuard) HospitalWorkHospitalName(ctx context.Context, purpose authz.HospitalWorkPurpose, scope authz.StaffActorScope) (string, error) {
	if g == nil || purpose != authz.StocktakeWork {
		return "", authz.ErrScopedAuthorizationUnavailable
	}
	return hospitalWorkName(ctx, g.actors, purpose, scope)
}

func (g *wardIndentActorGuard) HospitalWorkHospitalName(ctx context.Context, purpose authz.HospitalWorkPurpose, scope authz.StaffActorScope) (string, error) {
	if g == nil || purpose != authz.WardIndentWork {
		return "", authz.ErrScopedAuthorizationUnavailable
	}
	return hospitalWorkName(ctx, g.actors, purpose, scope)
}

func hospitalWorkName(ctx context.Context, actors *staffActorGuard, purpose authz.HospitalWorkPurpose, scope authz.StaffActorScope) (string, error) {
	checks, err := purpose.ReadScopeChecks(ctx, scope)
	if err != nil || actors == nil || actors.db == nil || tenantdb.ActorFrom(ctx) != scope.Subject {
		return "", authz.ErrScopedAuthorizationUnavailable
	}
	action := "workforce." + string(purpose) + "_hospital_name.read"
	var name string
	err = actors.withCurrentEmployeeActor(ctx, scope, purpose.ActorPermission(), func(actorCtx context.Context, actor authz.StaffActorReference) error {
		return actors.db.WithTenantCommand(actorCtx, scope.TenantID, func(tx *gorm.DB) error {
			var row struct{ Name string }
			if err := tx.Table("workforce_hospitals").Select("name").Where("tenant_id=? AND id=?", scope.TenantID, scope.HospitalID).Clauses(clause.Locking{Strength: "SHARE"}).Take(&row).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)VALUES(?,?,?,?,?,jsonb_build_object('read_assignment_id',?::uuid,'read_assignment_version',?::bigint,'source_assignment_id',?::uuid,'source_assignment_version',?::bigint,'actor_link_id',?::uuid,'actor_link_version',?::bigint))`, scope.TenantID, scope.HospitalID, scope.Subject, action, scope.HospitalID, checks[0].Grant.AssignmentID, checks[0].Grant.Version, checks[1].Grant.AssignmentID, checks[1].Grant.Version, actor.LinkID, actor.LinkVersion).Error; err != nil {
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
