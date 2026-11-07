package workforce

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type independentStaffGuard struct{ db *tenantdb.DB }

// IndependentStaffIdentities publishes minimal reviewed identity facts to guarded owner commands.
func (*Module) IndependentStaffIdentities(db *tenantdb.DB) authz.CurrentIndependentStaffGuard {
	return &independentStaffGuard{db: db}
}
func (g *independentStaffGuard) WithCurrentIndependentStaff(ctx context.Context, scope authz.IndependentStaffScope, action func(context.Context, authz.IndependentStaffReference) error) error {
	return g.withCurrent(ctx, scope, "", action)
}
func (g *independentStaffGuard) WithCurrentIndependentStaffForReview(ctx context.Context, scope authz.IndependentStaffScope, proposer string, action func(context.Context, authz.IndependentStaffReference) error) error {
	actor := tenantdb.ActorFrom(ctx)
	if !validLoginSubject(proposer) || !validLoginSubject(actor) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if actor == proposer || actor == scope.Subject {
		return authz.ErrScopedCommandDenied
	}
	return g.withCurrent(ctx, scope, proposer, action)
}
func (g *independentStaffGuard) withCurrent(ctx context.Context, scope authz.IndependentStaffScope, proposer string, action func(context.Context, authz.IndependentStaffReference) error) error {
	tenant, err := uuid.Parse(scope.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != scope.TenantID || scope.EmployerID.Version() != 7 || scope.EmployerID.Variant() != uuid.RFC4122 || !validLoginSubject(scope.Subject) || !validBenefitText(tenantdb.ActorFrom(ctx), 1, 200) || action == nil || g.db == nil || !g.db.GuardedCommandActive(ctx) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if scope.EmployerKind != supplierEstablishmentKind && scope.EmployerKind != pharmacyEstablishmentKind {
		return fmt.Errorf("independent employer kind is unavailable: %w", authz.ErrScopedCommandDenied)
	}
	return g.db.WithTenantCommand(ctx, scope.TenantID, func(tx *gorm.DB) (err error) {
		if err = tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, "workforce.independent_identity:"+scope.TenantID+":"+scope.EmployerID.String()).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		var previous string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.workforce_independent_identity','app.workforce_independent_employer','app.workforce_independent_kind','app.workforce_independent_subject','app.workforce_independent_staff','app.workforce_independent_actor','app.workforce_independent_proposer'])AS key`).Scan(&previous).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		restored := false
		restore := func() error {
			return tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error
		}
		defer func() {
			if !restored {
				err = errors.Join(err, restore())
			}
		}()
		if err = tx.Exec(`SELECT set_config('app.workforce_independent_identity','source',true),set_config('app.workforce_independent_employer',?,true),set_config('app.workforce_independent_kind',?,true),set_config('app.workforce_independent_subject',?,true),set_config('app.workforce_independent_actor',?,true),set_config('app.workforce_independent_proposer',?,true)`, scope.EmployerID.String(), scope.EmployerKind, scope.Subject, tenantdb.ActorFrom(ctx), proposer).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		var candidate struct{ StaffID uuid.UUID }
		if err = tx.Table("workforce_login_links").Select("staff_id").Where("tenant_id=? AND hospital_id=? AND subject=? AND status='active' AND employment_generation_id IS NULL", scope.TenantID, scope.EmployerID, scope.Subject).Take(&candidate).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("independent account is unavailable: %w", authz.ErrScopedCommandDenied)
		} else if err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}

		staffID := candidate.StaffID
		if err = tx.Exec(`SELECT set_config('app.workforce_independent_staff',?,true)`, staffID.String()).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		var person struct {
			Version          int64
			EmploymentStatus string
		}

		err = tx.Raw(`SELECT version,employment_status FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=? FOR SHARE`, scope.TenantID, scope.EmployerID, staffID).Row().Scan(&person.Version, &person.EmploymentStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("independent Staff is unavailable: %w", authz.ErrScopedCommandDenied)
		}
		if err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		if person.EmploymentStatus != "active" || person.Version < 1 {
			return fmt.Errorf("independent Staff is not current: %w", authz.ErrScopedCommandDenied)
		}

		var ref authz.IndependentStaffReference
		if err = tx.Raw(`SELECT staff_id,id AS link_id,independent_staff_version AS staff_version,version AS link_version FROM workforce_login_links WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND subject=? AND status='active' AND employment_generation_id IS NULL FOR SHARE`, scope.TenantID, scope.EmployerID, staffID, scope.Subject).Scan(&ref).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		if ref.StaffID != staffID || ref.StaffVersion != person.Version || ref.LinkVersion < 2 || ref.LinkID.Version() != 7 {
			return fmt.Errorf("independent account basis is not current: %w", authz.ErrScopedCommandDenied)
		}

		if proposer != "" {
			var conflicts bool
			if err = tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_login_links l JOIN workforce_login_link_history h ON(h.tenant_id,h.hospital_id,h.id)=(l.tenant_id,l.hospital_id,l.id) WHERE l.tenant_id=? AND l.hospital_id=? AND l.staff_id=? AND l.subject=? AND h.status='active')OR EXISTS(SELECT 1 FROM workforce_login_links a JOIN workforce_login_link_history ah ON(ah.tenant_id,ah.hospital_id,ah.id)=(a.tenant_id,a.hospital_id,a.id) JOIN workforce_login_links m ON(m.tenant_id,m.hospital_id,m.staff_id)=(a.tenant_id,a.hospital_id,a.staff_id) JOIN workforce_login_link_history mh ON(mh.tenant_id,mh.hospital_id,mh.id)=(m.tenant_id,m.hospital_id,m.id) WHERE a.tenant_id=? AND a.hospital_id=? AND a.subject=? AND ah.status='active' AND m.subject=? AND mh.status='active')`, scope.TenantID, scope.EmployerID, staffID, tenantdb.ActorFrom(ctx), scope.TenantID, scope.EmployerID, tenantdb.ActorFrom(ctx), proposer).Row().Scan(&conflicts); err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			if conflicts {
				return authz.ErrScopedCommandDenied
			}
		}
		if err = tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)VALUES(?,?,?,'workforce.independent_staff.use',?,jsonb_build_object('employer_kind',?::text,'staff_version',?::bigint,'link_id',?::uuid,'link_version',?::bigint))`, scope.TenantID, scope.EmployerID, tenantdb.ActorFrom(ctx), staffID, scope.EmployerKind, ref.StaffVersion, ref.LinkID, ref.LinkVersion).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		if err = restore(); err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		restored = true
		return action(ctx, ref)
	})
}

func (g *independentStaffGuard) WithIndependentEmployerAccess(ctx context.Context, scope authz.IndependentEmployerScope, minimum string, action func(context.Context) error) error {
	tenant, err := uuid.Parse(scope.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != scope.TenantID || scope.EmployerID.Version() != 7 || scope.EmployerID.Variant() != uuid.RFC4122 || action == nil || g.db == nil || !g.db.GuardedCommandActive(ctx) || !validBenefitText(tenantdb.ActorFrom(ctx), 1, 200) || (minimum != "reader" && minimum != "manager") {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if scope.EmployerKind != supplierEstablishmentKind && scope.EmployerKind != pharmacyEstablishmentKind {
		return authz.ErrScopedCommandDenied
	}
	return g.db.WithTenantCommand(ctx, scope.TenantID, func(tx *gorm.DB) error {
		root := establishment{kind: scope.EmployerKind, id: scope.EmployerID}
		if err := root.requireKind(tx, scope.TenantID); errors.Is(err, gorm.ErrRecordNotFound) {
			return authz.ErrScopedCommandDenied
		} else if err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		p := authn.Principal{TenantID: scope.TenantID, Subject: tenantdb.ActorFrom(ctx)}
		if minimum == "manager" {
			if err := lockDirectoryManager(tx, p, scope.EmployerID); errors.Is(err, gorm.ErrRecordNotFound) {
				return authz.ErrScopedCommandDenied
			} else if err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
		} else {
			var grant struct{ Level string }
			if err := tx.Raw(`SELECT level FROM workforce_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject=? FOR SHARE`, scope.TenantID, scope.EmployerID, p.Subject).Scan(&grant).Error; err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			if grant.Level != "reader" && grant.Level != "manager" {
				return authz.ErrScopedCommandDenied
			}
		}
		return action(ctx)
	})
}
