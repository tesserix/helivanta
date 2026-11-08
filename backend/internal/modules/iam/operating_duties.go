package iam

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	PermOperatingRead    authz.Permission = "iam.operating.read"
	PermOperatingPrepare authz.Permission = "iam.operating.prepare"
	PermOperatingReview  authz.Permission = "iam.operating.review"
	PermOperatingRevoke  authz.Permission = "iam.operating.revoke"
)

var errOperatingValidity = errors.New("operating duty validity expired")

//go:embed operating_duties.sql
var operatingDutiesSchema string

//go:embed operating_templates_v1.sql
var operatingTemplatesSchema string

//go:embed operating_templates_v2.sql
var operatingTemplatesCurrentSchema string

//go:embed operating_templates_generated.sql
var operatingTemplatesOrderSchema string

type operatingHandlers struct {
	db        *tenantdb.DB
	bus       *events.Bus
	graph     currentGraph
	staff     authz.CurrentIndependentStaffGuard
	locations authz.CurrentOperatingLocationGuard
}
type operatingDutyRow struct {
	TenantID                                 uuid.UUID
	ID                                       uuid.UUID
	Input                                    json.RawMessage
	PreparedBy, Status                       string
	MembershipEpoch, Version, AuthorityEpoch int64
	CreatedAt, UpdatedAt                     time.Time
}

func registerOperatingDutyRoutes(r *platform.Router, deps platform.Deps) {
	r = r.Group("/iam")
	h := &operatingHandlers{db: deps.DB, bus: deps.Bus, staff: deps.IndependentStaff, locations: deps.OperatingLocations}
	h.graph, _ = deps.Authz.(currentGraph)
	platform.ListRoute(r, "/operating-employers/:kind/:employer/staff/:staff/duties", PermOperatingRead, h.list)
	r.PUT("/operating-duties/:id", PermOperatingPrepare, h.prepare)
	r.GET("/operating-duties/:id", PermOperatingRead, h.get)
	r.GET("/operating-duties/:id/proposal-receipt", PermOperatingRead, h.proposalReceipt)
	r.PUT("/operating-duties/:id/commands/:command", authz.Public, h.decide)
	r.GET("/operating-duties/:id/commands/:command", PermOperatingRead, h.getCommand)
}
func operatingScope(c *gin.Context, permission authz.Permission) (authn.Principal, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	if !authn.Refresh(c) || !authz.Refresh(c, permission) {
		return authn.Principal{}, uuid.Nil, false
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	for key, expected := range map[string]string{"X-Helivanta-Expected-Tenant": p.TenantID, "X-Helivanta-Expected-Subject": p.Subject} {
		values := c.Request.Header.Values(key)
		if len(values) != 1 || values[0] != expected {
			respond.Error(c, 409, "context_changed", "Your account or workspace changed. Reopen this source.")
			return p, uuid.Nil, false
		}
	}
	if c.Param("id") == "" {
		return p, uuid.Nil, true
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != c.Param("id") {
		respond.NotFound(c, "operating duty")
		return p, uuid.Nil, false
	}
	return p, id, true
}
func operatingDecode(c *gin.Context, value any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		respond.Error(c, 400, "invalid_request", "Enter a valid operating duty command.")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		respond.Error(c, 400, "invalid_request", "Send exactly one operating duty command.")
		return false
	}
	return true
}
func operatingError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errOperatingValidity):
		respond.Error(c, 400, "invalid_request", "Choose a future expiry within the supported duty interval.")
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, authz.ErrScopedCommandDenied):
		respond.NotFound(c, "operating duty")
	case errors.Is(err, errAssignmentDenied):
		respond.Forbidden(c, "Current operating duty administration authority is required.")
	case errors.Is(err, errProposalConflict), errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23514"):
		respond.Error(c, 409, "operating_conflict", "The duty or its source basis changed. Refresh before continuing.")
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "credentials_revoked", "Your credentials changed. Sign in again.")
	case errors.Is(err, authz.ErrScopedAuthorizationUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, tenantdb.ErrGuardedCommandExpired):
		respond.Error(c, 503, "authorization_unavailable", "Current operating authority or source evidence is unavailable.")
	default:

		safe := fmt.Errorf("operating duty failure (%T)", err)
		if errors.As(err, &pg) {
			safe = fmt.Errorf("operating duty storage failure (SQLSTATE %s, constraint %s)", pg.Code, pg.ConstraintName)
		}
		respond.InternalErr(c, safe, "Could not process the operating duty.")
	}
}
func operatingAdministrationRole(permission authz.Permission, role authz.Role) bool {
	if role == authz.RoleTenantAdmin {
		return true
	}
	if role == authz.RoleSupplyOwner || role == authz.RoleSupplyAdmin || role == authz.RolePharmacyOwner {
		return true
	}
	if role == authz.RoleSupplyCompliance {
		return permission == PermOperatingRead || permission == PermOperatingReview
	}
	return role == authz.RoleSupplyAuditor && permission == PermOperatingRead
}
func (h *operatingHandlers) employerAccess(ctx context.Context, tenant string, scope OperatingDutyScope, minimum string, action func(context.Context) error) error {
	if h.staff == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return h.staff.WithIndependentEmployerAccess(ctx, authz.IndependentEmployerScope{TenantID: tenant, EmployerKind: scope.EmployerKind, EmployerID: scope.EmployerID}, minimum, action)
}
func (h *operatingHandlers) actorProof(ctx context.Context, p authn.Principal, permission authz.Permission) (authz.Membership, error) {
	return h.actorProofFor(ctx, p, permission, func(role authz.Role) bool { return operatingAdministrationRole(permission, role) })
}
func (h *operatingHandlers) actorProofFor(ctx context.Context, p authn.Principal, permission authz.Permission, roleAllowed func(authz.Role) bool) (authz.Membership, error) {
	if h.graph == nil {
		return authz.Membership{}, authz.ErrScopedAuthorizationUnavailable
	}
	members, err := currentMemberships(ctx, h.db, p.TenantID, p.Subject)
	if err != nil {
		return authz.Membership{}, authz.ErrScopedAuthorizationUnavailable
	}
	for _, member := range members {
		if roleAllowed != nil && !roleAllowed(member.Role) {
			continue
		}
		ready, err := h.graph.IsMembershipReady(ctx, member)
		if err != nil {
			return authz.Membership{}, authz.ErrScopedAuthorizationUnavailable
		}
		if !ready {
			return authz.Membership{}, authz.ErrScopedAuthorizationUnavailable
		}
		permissions, err := h.graph.ResolveMembership(ctx, member)
		if err != nil {
			return authz.Membership{}, authz.ErrScopedAuthorizationUnavailable
		}
		if permissions.Has(permission) {
			return member, nil
		}
	}
	return authz.Membership{}, errAssignmentDenied
}
func (h *operatingHandlers) transaction(ctx context.Context, p authn.Principal, proof authz.Membership, command string, action func(context.Context, *gorm.DB) error) error {
	deadline := time.Now().Add(5 * time.Second)
	if p.IdleDeadline.Before(deadline) {
		deadline = p.IdleDeadline
	}
	if p.AuthTime.IsZero() || !deadline.After(time.Now()) {
		return authz.ErrScopedCredentialRevoked
	}
	ctx = tenantdb.WithActor(ctx, p.Subject)
	return h.db.WithGuardedTenantCommand(ctx, p.TenantID, deadline, func(tx *gorm.DB) error {
		var member struct{ ID uuid.UUID }
		if err := tx.Table("iam_members").Select("id").Where("tenant_id=? AND id=? AND subject=? AND role_key=?", p.TenantID, proof.ID, p.Subject, proof.Role).Clauses(clause.Locking{Strength: "SHARE"}).Take(&member).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return errAssignmentDenied
		} else if err != nil {
			return err
		}
		epoch, err := proposalMembershipEpoch(tx, p.TenantID, p.Subject)
		if err != nil {
			return err
		}
		if epoch != proof.Epoch {
			return errAssignmentDenied
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, "iam.credential:"+p.Subject).Error; err != nil {
			return err
		}
		var revoked bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_credential_revocations WHERE subject=? AND revoked_at>=?)`, p.Subject, p.AuthTime).Scan(&revoked).Error; err != nil {
			return err
		}
		if revoked {
			return authz.ErrScopedCredentialRevoked
		}
		return tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true),set_config('app.iam_operating_command',?,true)`, command).Error
	}, func(commandCtx context.Context) error {
		return h.db.WithTenantCommand(commandCtx, p.TenantID, func(tx *gorm.DB) error { return action(commandCtx, tx) })
	})
}
func loadOperatingDuty(tx *gorm.DB, tenant string, id uuid.UUID) (operatingDutyRow, error) {
	var row operatingDutyRow
	err := tx.Table("iam_operating_duties").Select("tenant_id,id,input,prepared_by,status,membership_epoch,version,authority_epoch,created_at,updated_at").Where("tenant_id=? AND id=?", tenant, id).Take(&row).Error
	return row, err
}
func operatingDutyView(row operatingDutyRow, receipt bool) (OperatingDuty, error) {
	var input OperatingDutyInput
	if err := json.Unmarshal(row.Input, &input); err != nil {
		return OperatingDuty{}, err
	}
	result := OperatingDuty{OwnerTenantID: row.TenantID, ID: row.ID, Input: input, Status: row.Status, Version: row.Version, PreparedBy: row.PreparedBy, CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC()}
	if receipt {
		result.Status = "pending_review"
		result.Version = 1
		result.UpdatedAt = result.CreatedAt
	}
	return result, nil
}
func operatingAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID, action string) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,?,'iam_operating_duties',?)`, p.TenantID, p.Subject, action, id).Error
}
func operatingTemplate(key, kind string) (OperatingDutyTemplate, bool) {
	for _, template := range operatingDutyTemplates {
		if template.Key == key && template.EmployerKind == kind {
			return template, true
		}
	}
	return OperatingDutyTemplate{}, false
}
func validateOperatingInput(in *OperatingDutyInput, now time.Time, tenant string) bool {
	template, ok := operatingTemplate(in.TemplateKey, in.Scope.EmployerKind)
	if !ok {
		return false
	}
	in.ValidFrom = in.ValidFrom.UTC().Truncate(time.Microsecond)
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	in.Reason = strings.TrimSpace(in.Reason)
	for _, v := range []int64{in.Scope.ExpectedEmployerVersion, in.Scope.ExpectedLocationVersion, in.Identity.StaffVersion, in.Identity.LinkVersion} {
		if v < 1 || v > 9007199254740991 {
			return false
		}
	}
	return authz.ValidOperatingLocationScope(authz.OperatingLocationScope{TenantID: tenant, EmployerKind: in.Scope.EmployerKind, EmployerID: in.Scope.EmployerID, LocationID: in.Scope.LocationID}) && in.Identity.StaffID != uuid.Nil && in.Identity.LinkID.Version() == 7 && in.Identity.LinkID.Variant() == uuid.RFC4122 && in.Identity.LinkVersion >= 2 && proposalSubject(in.Subject) && utf8.ValidString(in.Reason) && utf8.RuneCountInString(in.Reason) >= 1 && utf8.RuneCountInString(in.Reason) <= 500 && !strings.ContainsRune(in.Reason, 0) && !in.ValidFrom.IsZero() && in.ExpiresAt.After(in.ValidFrom) && in.ExpiresAt.Sub(in.ValidFrom) <= time.Duration(template.MaxValidityHours)*time.Hour && in.ValidFrom.Before(now.AddDate(1, 0, 0))
}
func (h *operatingHandlers) prepare(c *gin.Context) {
	p, id, ok := operatingScope(c, PermOperatingPrepare)
	if !ok {
		return
	}
	var in OperatingDutyInput
	if !operatingDecode(c, &in) {
		return
	}
	if !validateOperatingInput(&in, time.Now(), p.TenantID) {
		respond.Error(c, 400, "invalid_request", "Choose the exact reviewed Staff identity, stock source and supported duty with bounded validity.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	proof, err := h.actorProof(ctx, p, PermOperatingPrepare)
	if err != nil {
		operatingError(c, err)
		return
	}
	var result OperatingDuty
	created := false
	err = h.transaction(ctx, p, proof, "prepare", func(commandCtx context.Context, tx *gorm.DB) error {
		recover := func(row operatingDutyRow) error {
			expected, err := json.Marshal(in)
			if err != nil {
				return err
			}
			retained, err := operatingDutyView(row, true)
			if err != nil {
				return err
			}
			original, err := json.Marshal(retained.Input)
			if err != nil {
				return err
			}

			return h.employerAccess(commandCtx, p.TenantID, retained.Input.Scope, "manager", func(context.Context) error {
				if row.PreparedBy != p.Subject || string(expected) != string(original) {
					return errProposalConflict
				}
				result = retained
				return operatingAudit(tx, p, id, "iam.operating.proposal_recovered")
			})
		}
		row, err := loadOperatingDuty(tx, p.TenantID, id)
		if err == nil {
			return recover(row)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if !in.ExpiresAt.After(time.Now()) {
			return errOperatingValidity
		}
		return h.employerAccess(commandCtx, p.TenantID, in.Scope, "manager", func(commandCtx context.Context) error {
			epoch, err := proposalMembershipEpoch(tx, p.TenantID, in.Subject)
			if err != nil {
				return err
			}
			if epoch < 1 {
				return gorm.ErrRecordNotFound
			}
			if h.staff == nil || h.locations == nil {
				return authz.ErrScopedAuthorizationUnavailable
			}
			source, err := h.locations.CurrentOperatingLocation(commandCtx, authz.OperatingLocationScope{TenantID: p.TenantID, EmployerKind: in.Scope.EmployerKind, EmployerID: in.Scope.EmployerID, LocationID: in.Scope.LocationID})
			if err != nil {
				return err
			}
			if source.Scope != (authz.OperatingLocationScope{TenantID: p.TenantID, EmployerKind: in.Scope.EmployerKind, EmployerID: in.Scope.EmployerID, LocationID: in.Scope.LocationID}) || source.Country != "IN" {
				return authz.ErrScopedAuthorizationUnavailable
			}
			if source.EmployerVersion != in.Scope.ExpectedEmployerVersion || source.LocationVersion != in.Scope.ExpectedLocationVersion {
				return errProposalConflict
			}
			return h.staff.WithCurrentIndependentStaff(commandCtx, authz.IndependentStaffScope{TenantID: p.TenantID, Subject: in.Subject, EmployerKind: in.Scope.EmployerKind, EmployerID: in.Scope.EmployerID}, func(_ context.Context, identity authz.IndependentStaffReference) error {
				if identity != (authz.IndependentStaffReference{StaffID: in.Identity.StaffID, StaffVersion: in.Identity.StaffVersion, LinkID: in.Identity.LinkID, LinkVersion: in.Identity.LinkVersion}) {
					return errProposalConflict
				}
				input, err := json.Marshal(in)
				if err != nil {
					return err
				}
				inserted := tx.Exec(`INSERT INTO iam_operating_duties(tenant_id,id,employer_kind,employer_id,location_id,subject,template_key,input,prepared_by,membership_epoch,status,version,authority_epoch,actor_subject,reason,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?::jsonb,?,?,'pending_review',1,1,?,?,clock_timestamp(),clock_timestamp())ON CONFLICT(tenant_id,id)DO NOTHING`, p.TenantID, id, in.Scope.EmployerKind, in.Scope.EmployerID, in.Scope.LocationID, in.Subject, in.TemplateKey, string(input), p.Subject, epoch, p.Subject, in.Reason)
				if inserted.Error != nil {
					return inserted.Error
				}
				row, err := loadOperatingDuty(tx, p.TenantID, id)
				if err != nil {
					return err
				}
				if inserted.RowsAffected == 0 {
					return recover(row)
				}
				result, err = operatingDutyView(row, true)
				if err != nil {
					return err
				}
				created = true
				return h.publish(tx, p.TenantID, row.ID, row.Version)
			})
		})
	})
	if err != nil {
		operatingError(c, err)
		return
	}
	if created {
		respond.Created(c, result)
	} else {
		respond.OK(c, result)
	}
}
func (h *operatingHandlers) publish(tx *gorm.DB, tenant string, id uuid.UUID, version int64) error {
	if h.bus == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	data, err := json.Marshal(iamcontract.OperatingDutyChangedData{DutyID: id.String(), Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, iamcontract.SubjectOperatingDutyChanged, events.Event{TenantID: tenant, Type: iamcontract.SubjectOperatingDutyChanged, Version: 1, Data: data})
}
func (h *operatingHandlers) get(c *gin.Context)             { h.read(c, false) }
func (h *operatingHandlers) proposalReceipt(c *gin.Context) { h.read(c, true) }
func (h *operatingHandlers) read(c *gin.Context, receipt bool) {
	p, id, ok := operatingScope(c, PermOperatingRead)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	proof, err := h.actorProof(ctx, p, PermOperatingRead)
	if err != nil {
		operatingError(c, err)
		return
	}
	var result OperatingDuty
	err = h.transaction(ctx, p, proof, "read", func(commandCtx context.Context, tx *gorm.DB) error {
		row, err := loadOperatingDuty(tx, p.TenantID, id)
		if err != nil {
			return err
		}
		if receipt && row.PreparedBy != p.Subject {
			return gorm.ErrRecordNotFound
		}
		result, err = operatingDutyView(row, receipt)
		if err != nil {
			return err
		}
		return h.employerAccess(commandCtx, p.TenantID, result.Input.Scope, "reader", func(context.Context) error { return operatingAudit(tx, p, id, "iam.operating.read") })
	})
	if err != nil {
		operatingError(c, err)
		return
	}
	respond.OK(c, result)
}

func (d OperatingDuty) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }
func (h *operatingHandlers) list(c *gin.Context, page pagination.Params) ([]OperatingDuty, error) {
	p, _, ok := operatingScope(c, PermOperatingRead)
	if !ok {
		return nil, nil
	}
	kind := c.Param("kind")
	employer, err := uuid.Parse(c.Param("employer"))
	staff, staffErr := uuid.Parse(c.Param("staff"))
	if err != nil || staffErr != nil || staff == uuid.Nil || staff.String() != c.Param("staff") || employer.Version() != 7 || employer.Variant() != uuid.RFC4122 || employer.String() != c.Param("employer") || (kind != "supplier" && kind != "pharmacy") {
		respond.NotFound(c, "operating employer")
		return nil, nil
	}
	for key, values := range c.Request.URL.Query() {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.Error(c, 400, "invalid_request", "Use one page limit and optional continuation cursor.")
			return nil, nil
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	proof, err := h.actorProof(ctx, p, PermOperatingRead)
	if err != nil {
		operatingError(c, err)
		return nil, err
	}
	result := []OperatingDuty{}
	err = h.transaction(ctx, p, proof, "read", func(commandCtx context.Context, tx *gorm.DB) error {
		return h.employerAccess(commandCtx, p.TenantID, OperatingDutyScope{EmployerKind: kind, EmployerID: employer}, "reader", func(context.Context) error {
			var rows []operatingDutyRow
			query := tx.Table("iam_operating_duties").Select("tenant_id,id,input,prepared_by,status,membership_epoch,version,authority_epoch,created_at,updated_at").Where("tenant_id=? AND employer_kind=? AND employer_id=? AND input#>>'{identity,staff_id}'=?", p.TenantID, kind, employer, staff.String()).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
			if page.After != nil {
				query = query.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
			}
			if err := query.Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				view, err := operatingDutyView(row, false)
				if err != nil {
					return err
				}
				result = append(result, view)
			}
			return operatingAudit(tx, p, employer, "iam.operating.staff_directory_read")
		})
	})
	if err != nil {
		operatingError(c, err)
	}
	return result, err
}
