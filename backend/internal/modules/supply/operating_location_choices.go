package supply

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

//go:embed operating_location_choices.sql
var operatingLocationChoicesSchema string

func (g *operatingLocationGuard) OperatingLocationChoices(ctx context.Context, request authz.GovernedIndependentEmployerRequest, page pagination.Params) ([]authz.OperatingLocationChoice, error) {
	if g.db == nil || !g.db.GuardedCommandActive(ctx) || tenantdb.ActorFrom(ctx) != request.Principal.Subject || page.Limit < 1 || page.Limit > 20 {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	if request.Scope.EmployerKind != "supplier" {
		return nil, authz.ErrScopedCommandDenied
	}
	proof, err := authz.OperatingLocationChoiceProof(request, time.Now())
	if err != nil {
		return nil, err
	}
	if page.After != nil && page.After.TenantID != request.Scope.TenantID {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	deadline := request.Source.ExpiresAt
	if request.Principal.IdleDeadline.Before(deadline) {
		deadline = request.Principal.IdleDeadline
	}
	if err := g.db.ConstrainGuardedCommand(ctx, request.Scope.TenantID, deadline); err != nil {
		return nil, err
	}
	rows := []struct {
		ID                  uuid.UUID
		Code, Name, Country string
		Version             int64
		CreatedAt           time.Time
	}{}
	var employerVersion int64
	err = g.db.WithTenantCommand(ctx, request.Scope.TenantID, func(tx *gorm.DB) (err error) {
		var installed bool
		if err := tx.Raw(`SELECT to_regprocedure('public.supply_operating_choice_support()')IS NOT NULL`).Scan(&installed).Error; err != nil {
			return err
		}
		if !installed {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if err := tx.Raw(`SELECT supply_operating_choice_support()`).Scan(&installed).Error; err != nil {
			return err
		}
		if !installed {
			return authz.ErrScopedAuthorizationUnavailable
		}
		var previous string
		if err := tx.Raw(`SELECT COALESCE(current_setting('app.supply_operating_choices',true),'')`).Scan(&previous).Error; err != nil {
			return err
		}
		raw, err := json.Marshal(proof)
		if err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.supply_operating_choices',?,true)`, string(raw)).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config('app.supply_operating_choices',?,true)`, previous).Error)
		}()
		if err := tx.Raw(`SELECT version FROM supply_parties WHERE tenant_id=? AND id=? AND record_kind='owned_organisation'AND country_code='IN' FOR SHARE`, request.Scope.TenantID, request.Scope.EmployerID).Row().Scan(&employerVersion); err != nil {
			return err
		}
		if employerVersion < 1 || employerVersion > 9007199254740991 {
			return authz.ErrScopedAuthorizationUnavailable
		}
		query := `SELECT id,code,name,country_code AS country,version,created_at FROM supply_premises WHERE tenant_id=? AND party_id=? AND country_code='IN'AND status='active'AND kind IN('warehouse','depot')`
		args := []any{request.Scope.TenantID, request.Scope.EmployerID}
		if page.After != nil {
			query += ` AND(created_at,id)<(?,?)`
			args = append(args, page.After.CreatedAt, page.After.ID)
		}
		query += ` ORDER BY created_at DESC,id DESC LIMIT ? FOR SHARE`
		args = append(args, page.Limit+1)
		if err := tx.Raw(query, args...).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		raw, err = json.Marshal(ids)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) SELECT ?::uuid,?::text,'supply.operating_location_choice.read','supply_premises',value::uuid FROM jsonb_array_elements_text(?::jsonb)`, request.Scope.TenantID, request.Principal.Subject, string(raw)).Error
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, authz.ErrScopedCommandDenied
	}
	if err != nil {
		return nil, errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	result := make([]authz.OperatingLocationChoice, 0, len(rows))
	for _, row := range rows {
		result = append(result, authz.OperatingLocationChoice{Reference: authz.OperatingLocationReference{Scope: authz.OperatingLocationScope{TenantID: request.Scope.TenantID, EmployerKind: request.Scope.EmployerKind, EmployerID: request.Scope.EmployerID, LocationID: row.ID}, EmployerVersion: employerVersion, LocationVersion: row.Version, Country: row.Country}, Code: row.Code, Name: row.Name, CreatedAt: row.CreatedAt})
	}
	return result, nil
}
