package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type hospitalStocktakeUse struct {
	hospital, department uuid.UUID
	path                 []string
	actor                authz.StaffActorReference
	operation, source    authz.ScopeGrant
	binding, revision    uuid.UUID
	bindingVersion       int64
	permission           authz.Permission
}

type stocktakeWorkUse struct {
	operatingStockUse
	hospital *hospitalStocktakeUse
}

func (u stocktakeWorkUse) authorityKind() string {
	if u.hospital != nil {
		return "hospital"
	}
	return "operating"
}
func (u stocktakeWorkUse) evidenceTable() string {
	if u.hospital != nil {
		return "pharmacy_stocktake_hospital_evidence"
	}
	return "pharmacy_stocktake_operating_evidence"
}
func (u stocktakeWorkUse) staffID() uuid.UUID {
	if u.hospital != nil {
		return u.hospital.actor.StaffID
	}
	return u.grant.Identity.StaffID
}

func (h *catalogueHandlers) stocktakeWork(ctx context.Context, p authn.Principal, pharmacy, outlet uuid.UUID, hospital, department string, permission authz.Permission, action func(*gorm.DB, stocktakeWorkUse) error) error {
	if hospital == "" && department == "" {
		return h.operatingStock(ctx, p, pharmacy, outlet, permission, func(tx *gorm.DB, u operatingStockUse) error {
			return action(tx, stocktakeWorkUse{operatingStockUse: u})
		})
	}
	guard, ok := h.stocktakeAuthority.(authz.CompoundScopedCommandGuard)
	if !ok || h.db == nil || h.stocktakeActors == nil || h.stocktakeDepartments == nil || h.stocktakeReferences == nil || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	hospitalID, err := uuid.Parse(hospital)
	departmentID, deptOK := operatingStockID(department)
	if err != nil || hospitalID == uuid.Nil || hospitalID.String() != hospital || !deptOK {
		return authz.ErrScopedCommandDenied
	}
	if permission != PermStocktakeRead && permission != PermStocktakeCount && permission != PermStocktakeReview {
		return authz.ErrScopedAuthorizationUnavailable
	}
	ctx, cancel := context.WithDeadline(tenantdb.WithActor(ctx, p.Subject), p.IdleDeadline)
	defer cancel()
	path, err := h.stocktakeDepartments.CurrentPath(ctx, p.TenantID, hospitalID, departmentID)
	if err != nil {
		return err
	}
	if len(path) == 0 || len(path) > authz.MaxDepartmentPath || path[0] != departmentID {
		return authz.ErrScopedAuthorizationUnavailable
	}
	pathStrings := make([]string, len(path))
	for i, id := range path {
		pathStrings[i] = id.String()
	}
	checks := make([]authz.ScopeCheck, 0, 2)
	for _, perm := range []authz.Permission{permission, authz.StocktakeActorUse} {
		request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital, DepartmentPath: pathStrings, Permission: perm}
		grant, err := h.stocktakeAuthority.AuthorizeScope(ctx, request)
		if err != nil {
			return err
		}
		assigned, parseErr := uuid.Parse(grant.DepartmentID)
		if grant.AssignmentID == "" || grant.ScopeKind != "department" || parseErr != nil || !slices.Contains(path, assigned) {
			return authz.ErrScopedCommandDenied
		}
		checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
	}
	return guard.WithScopeGrants(ctx, checks, p.AuthTime, func(current context.Context) error {
		// Candidate ancestry is rechecked against each actual assignment scope under the held IAM proofs.
		var within func(context.Context, int) error
		within = func(current context.Context, index int) error {
			if index < len(checks) {
				assigned, err := uuid.Parse(checks[index].Grant.DepartmentID)
				if err != nil {
					return authz.ErrScopedAuthorizationUnavailable
				}
				return h.stocktakeDepartments.WithinCurrentDepartment(current, p.TenantID, hospitalID, departmentID, assigned, func(locked context.Context) error { return within(locked, index+1) })
			}
			return h.stocktakeActors.WithCurrentStocktakeActor(current, authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospitalID, DepartmentPath: pathStrings}, func(actorCtx context.Context, actor authz.StaffActorReference) error {
				if actor.StaffID == uuid.Nil || actor.LinkID == uuid.Nil || actor.GenerationID == uuid.Nil || actor.LinkVersion < 2 || actor.EmploymentEpoch < 1 {
					return authz.ErrScopedAuthorizationUnavailable
				}
				use := stocktakeWorkUse{operatingStockUse: operatingStockUse{tenant: p.TenantID, subject: p.Subject, pharmacy: pharmacy, outlet: outlet}, hospital: &hospitalStocktakeUse{hospital: hospitalID, department: departmentID, path: pathStrings, actor: actor, operation: checks[0].Grant, source: checks[1].Grant, permission: permission}}
				err := h.db.WithTenantCommand(actorCtx, p.TenantID, func(tx *gorm.DB) error {
					var intact bool
					if err := tx.Raw(`SELECT pharmacy_stocktake_hospital_integrity()`).Scan(&intact).Error; err != nil {
						return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
					}
					if !intact {
						return authz.ErrScopedAuthorizationUnavailable
					}
					return pharmacyStockFrame(tx, p.Subject, pharmacy, outlet, permission, func() error {
						return hospitalStocktakeFrame(tx, hospital, department, func() error {
							if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, p.TenantID+":pharmacy-staff-bindings:"+outlet.String()).Error; err != nil {
								return err
							}
							var binding struct {
								ID, DepartmentRevisionID uuid.UUID
								Version                  int64
							}
							if err := tx.Raw(`SELECT b.id,b.version,b.department_revision_id FROM pharmacy_staff_department_bindings b JOIN pharmacy_outlets o ON(o.tenant_id,o.id)=(b.tenant_id,b.outlet_id) JOIN pharmacy_organizations p ON(p.tenant_id,p.id)=(o.tenant_id,o.pharmacy_id) WHERE b.tenant_id=? AND b.outlet_id=? AND b.hospital_id=? AND b.department_id=? AND b.status='approved' AND o.pharmacy_id=? AND p.ownership_type='hospital_owned' FOR SHARE OF b,o,p`, p.TenantID, outlet, hospitalID, departmentID, pharmacy).Take(&binding).Error; err != nil {
								return err
							}
							use.hospital.binding = binding.ID
							use.hospital.bindingVersion = binding.Version
							use.hospital.revision = binding.DepartmentRevisionID
							return h.stocktakeReferences.WithCurrentDepartment(actorCtx, authz.DepartmentReference{TenantID: p.TenantID, HospitalID: hospitalID, DepartmentID: departmentID, DepartmentRevision: binding.DepartmentRevisionID}, func(context.Context) error {
								return action(tx, use)
							})
						})
					})
				})
				if err != nil {
					return operatingStockFailure{err}
				}
				return nil
			})
		}
		return within(current, 0)
	})
}

func hospitalStocktakeFrame(tx *gorm.DB, hospital, department string, action func() error) (err error) {
	var previous string
	if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.pharmacy_stocktake_hospital','app.pharmacy_stocktake_department','app.pharmacy_stocktake_basis'])AS key`).Scan(&previous).Error; err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
	}()
	if err = tx.Exec(`SELECT set_config('app.pharmacy_stocktake_hospital',?,true),set_config('app.pharmacy_stocktake_department',?,true),set_config('app.pharmacy_stocktake_basis','',true)`, hospital, department).Error; err != nil {
		return err
	}
	return action()
}

func (u stocktakeWorkUse) stocktakeEvidence(tx *gorm.DB, take uuid.UUID, kind string, command *uuid.UUID) error {
	if u.hospital == nil {
		return u.operatingStockUse.stocktakeEvidence(tx, take, kind, command)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return operatingAuditFailure{err}
	}
	if kind == "start" {
		id = take
	} else if (kind == "count" || kind == "submit" || kind == "cancel" || kind == "approve" || kind == "reject") && command != nil {
		id = *command
	}
	var resource *uuid.UUID
	if take != uuid.Nil {
		resource = &take
	}
	basis, err := json.Marshal(u.hospitalBasis())
	if err != nil {
		return operatingAuditFailure{err}
	}
	if err = tx.Exec(`SELECT set_config('app.pharmacy_stocktake_basis',?,true)`, string(basis)).Error; err != nil {
		return operatingAuditFailure{err}
	}
	if err = tx.Exec(`INSERT INTO pharmacy_stocktake_hospital_evidence(tenant_id,outlet_id,id,pharmacy_id,stocktake_id,kind,command_id,permission,hospital_id,department_id,binding_id,binding_version,department_revision_id,staff_id,link_id,link_version,generation_id,employment_epoch,actor_subject,authority_basis)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?::jsonb)`, u.tenant, u.outlet, id, u.pharmacy, resource, kind, command, string(u.hospital.permission), u.hospital.hospital, u.hospital.department, u.hospital.binding, u.hospital.bindingVersion, u.hospital.revision, u.hospital.actor.StaffID, u.hospital.actor.LinkID, u.hospital.actor.LinkVersion, u.hospital.actor.GenerationID, u.hospital.actor.EmploymentEpoch, u.subject, string(basis)).Error; err != nil {
		return operatingAuditFailure{err}
	}
	return nil
}

func (u stocktakeWorkUse) hospitalBasis() map[string]any {
	return map[string]any{"operation": u.hospital.operation, "source": u.hospital.source, "actor": u.hospital.actor, "binding_id": u.hospital.binding, "binding_version": u.hospital.bindingVersion, "department_revision_id": u.hospital.revision}
}
