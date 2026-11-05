package pharmacy

import (
	"context"
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const (
	PermStaffBindingRead    authz.Permission = "pharmacy.staff_binding.read"
	PermStaffBindingPrepare authz.Permission = "pharmacy.staff_binding.prepare"
	PermStaffBindingReview  authz.Permission = "pharmacy.staff_binding.review"
)

type staffDepartmentBindingHandlers struct {
	db          *tenantdb.DB
	bus         *events.Bus
	authority   authz.ScopedAuthorizer
	actors      authz.CurrentStaffActorGuard
	departments authz.DepartmentReferenceGuard
}

type staffBindingScope struct {
	principal                           authn.Principal
	pharmacy, outlet, hospital, binding uuid.UUID
}

func staffDepartmentBindingRoutes(r *platform.Router, deps platform.Deps, actors authz.CurrentStaffActorGuard) {
	h := &staffDepartmentBindingHandlers{db: deps.DB, bus: deps.Bus, authority: deps.ScopedAuthorizer, actors: actors, departments: deps.DepartmentReferences}
	r.PUTScopedHospital("/pharmacies/:pharmacy/outlets/:outlet/hospitals/:hospital/staff-department-bindings/:binding", PermStaffBindingPrepare, "hospital", h.put)
	r.POSTScopedHospital("/pharmacies/:pharmacy/outlets/:outlet/hospitals/:hospital/staff-department-bindings/:binding/commands", PermStaffBindingRead, "hospital", h.decide)
	r.GETScopedHospital("/pharmacies/:pharmacy/outlets/:outlet/hospitals/:hospital/staff-department-bindings/:binding", PermStaffBindingRead, "hospital", h.get)
	r.GETScopedHospital("/pharmacies/:pharmacy/outlets/:outlet/hospitals/:hospital/staff-department-bindings/:binding/commands/:command", PermStaffBindingRead, "hospital", h.receipt)
	platform.ScopedHospitalListRoute(r, "/pharmacies/:pharmacy/outlets/:outlet/hospitals/:hospital/staff-department-bindings", PermStaffBindingRead, "hospital", h.list)
	platform.ScopedHospitalListRoute(r, "/pharmacies/:pharmacy/outlets/:outlet/hospitals/:hospital/staff-department-bindings/:binding/history", PermStaffBindingRead, "hospital", h.history)
}

func bindingRequestScope(c *gin.Context) (staffBindingScope, bool) {
	scope, ok := bindingBaseScope(c)
	if !ok {
		return scope, false
	}
	id, valid := registryID(c, "binding")
	if !valid {
		return scope, false
	}
	scope.binding = id
	return scope, true
}

func bindingBaseScope(c *gin.Context) (staffBindingScope, bool) {
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return staffBindingScope{}, false
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respond.Error(c, 409, "session_changed", "Your account or workspace changed. Reopen the department relationship workspace.")
		return staffBindingScope{}, false
	}
	scope := staffBindingScope{principal: p}
	for param, target := range map[string]*uuid.UUID{"pharmacy": &scope.pharmacy, "outlet": &scope.outlet} {
		id, valid := registryID(c, param)
		if !valid {
			return scope, false
		}
		*target = id
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil || hospital.String() != c.Param("hospital") {
		respond.NotFound(c, "hospital")
		return scope, false
	}
	scope.hospital = hospital
	return scope, true
}

func (h *staffDepartmentBindingHandlers) command(c *gin.Context, s staffBindingScope, permission authz.Permission, writing bool, action func(context.Context, *gorm.DB, authz.StaffActorReference) error) error {
	guard, supported := h.authority.(authz.CompoundScopedCommandGuard)
	if !supported || h.db == nil || h.actors == nil || h.departments == nil || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if !authn.Refresh(c) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	p, ok := authn.PrincipalFrom(c)
	if !ok || p.Subject != s.principal.Subject || p.TenantID != s.principal.TenantID {
		return authz.ErrScopedCommandDenied
	}
	if p.IdleDeadline.IsZero() {
		return authz.ErrScopedAuthorizationUnavailable
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	checks := make([]authz.ScopeCheck, 0, 2)
	for _, required := range []authz.Permission{permission, authz.StaffActorUse} {
		request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: s.hospital.String(), Permission: required}
		grant, err := h.authority.AuthorizeScope(ctx, request)
		if err != nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if grant.AssignmentID == "" {
			return authz.ErrScopedCommandDenied
		}
		if admitted, exists := authz.HospitalScopeGrant(c, required, s.hospital.String()); exists && admitted != grant {
			return authz.ErrScopedCommandDenied
		}
		checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
	}
	return guard.WithScopeGrants(ctx, checks, p.AuthTime, func(commandCtx context.Context) error {
		return h.actors.WithCurrentStaffActor(commandCtx, authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: s.hospital}, func(actorCtx context.Context, actor authz.StaffActorReference) error {
			return h.db.WithTenantCommand(actorCtx, p.TenantID, func(tx *gorm.DB) error {
				mode := "read"
				if writing {
					mode = "write"
				}
				grant := checks[0].Grant
				if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true),set_config('app.pharmacy_staff_binding_hospital',?,true),set_config('app.pharmacy_staff_binding_mode',?,true),set_config('app.pharmacy_staff_binding_permission',?,true),set_config('app.pharmacy_staff_binding_assignment',?,true),set_config('app.pharmacy_staff_binding_assignment_version',?,true),set_config('app.pharmacy_staff_binding_actor',?,true)`, p.Subject, s.outlet.String(), s.pharmacy.String(), s.hospital.String(), mode, string(permission), grant.AssignmentID, strconv.FormatInt(grant.Version, 10), actor.StaffID.String()).Error; err != nil {
					return err
				}
				return action(actorCtx, tx, actor)
			})
		})
	})
}

func staffBindingError(c *gin.Context, err error) {
	if c.Writer.Written() {
		return
	}
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Unauthenticated(c, "credential revoked")
	case errors.Is(err, authz.ErrScopedCommandDenied), errors.Is(err, tenantdb.ErrGuardedCommandExpired):
		respond.Forbidden(c, "Current department binding access is required.")
	case errors.Is(err, authz.ErrScopedAuthorizationUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		respond.Error(c, 503, "scoped_authorization_unavailable", "Current department binding access could not be verified.")
	case errors.Is(err, errStaffBindingEvidenceUnavailable):
		respond.Error(c, 503, "binding_evidence_unavailable", "Department binding evidence could not be recorded. Retry with the same command.")
	case errors.Is(err, authz.ErrDepartmentReferenceHidden), errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "department binding")
	case errors.Is(err, authz.ErrDepartmentReferenceChanged), errors.Is(err, errStaffBindingConflict):
		respond.Error(c, 409, "binding_conflict", "The relationship or its source changed. Reload before continuing.")
	case errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23514"):
		respond.Error(c, 409, "binding_conflict", "The relationship or its source changed. Reload before continuing.")
	case errors.As(err, &pg) && pg.Code == "23503":
		respond.NotFound(c, "department binding")
	case errors.As(err, &pg) && pg.Code == "42501":
		respond.Error(c, 503, "scoped_authorization_unavailable", "Current department binding access could not be verified.")
	default:
		respond.InternalErr(c, errors.New("department binding operation failed"), "Could not process department binding.")
	}
}

func staffBindingAuthorizationObservations(tx *gorm.DB, s staffBindingScope) ([]StaffBindingAuthorizationObservation, error) {
	if tx == nil || tx.Statement == nil || tx.Statement.Context == nil {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	observations := make([]StaffBindingAuthorizationObservation, 0, authz.MaxScopedCommandChecks)
	actorObserved := false
	for _, permission := range []authz.Permission{PermStaffBindingRead, PermStaffBindingPrepare, PermStaffBindingReview, authz.StaffActorUse} {
		grant, ok := authz.ScopedCommandGrant(tx.Statement.Context, authz.ScopeRequest{TenantID: s.principal.TenantID, Subject: s.principal.Subject, HospitalID: s.hospital.String(), Permission: permission})
		if !ok {
			continue
		}
		id, err := uuid.Parse(grant.AssignmentID)
		if err != nil || !bindingV7(id) || id.String() != grant.AssignmentID || grant.Version < 1 || grant.Version > 9007199254740990 || grant.ScopeEpoch < 1 || grant.ScopeEpoch > 9007199254740990 || grant.MembershipEpoch < 1 || grant.MembershipEpoch > 9007199254740990 {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		actorObserved = actorObserved || permission == authz.StaffActorUse
		observations = append(observations, StaffBindingAuthorizationObservation{Permission: string(permission), AssignmentID: id, AssignmentVersion: grant.Version, ScopeEpoch: grant.ScopeEpoch, MembershipEpoch: grant.MembershipEpoch})
	}
	if !actorObserved || len(observations) < 2 || len(observations) > authz.MaxScopedCommandChecks {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	return observations, nil
}
