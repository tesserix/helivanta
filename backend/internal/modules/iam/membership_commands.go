package iam

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

//go:embed membership_commands.sql
var membershipCommandsSchema string

var errMembershipConflict = errors.New("membership command conflict")
var errMembershipAuthority = errors.New("membership authority unavailable")
var errGovernanceMembership = errors.New("governance membership requires owner recovery")

type membershipCommand struct {
	MembershipCommand `gorm:"embedded"`
	TenantID          uuid.UUID `json:"-"`
}

func (membershipCommand) TableName() string                 { return "iam_membership_commands" }
func (c membershipCommand) PageKey() (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }
func (c membershipCommand) input() MembershipCommandInput {
	return MembershipCommandInput{Operation: c.Operation, BasisID: c.BasisID, Subject: c.Subject, RoleKey: c.RoleKey, ExpectedMembershipEpoch: c.ExpectedMembershipEpoch, Reason: c.Reason}
}

type membershipCommandHandlers struct {
	db       *tenantdb.DB
	bus      *events.Bus
	identity MembershipIdentityReader
}

func (m *Module) registerMembershipCommands(g *platform.Router, deps platform.Deps) {
	h := &membershipCommandHandlers{db: deps.DB, bus: deps.Bus, identity: m.membershipIdentity}
	g.PUT("/membership-commands/:id", PermMemberManage, h.put)
	platform.ListRoute(g, "/membership-commands", PermMemberManage, h.list)
	platform.ListRoute(g, "/membership-candidates", PermMemberManage, h.candidates)
}

func membershipCommandError(c *gin.Context, err error) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		if p, ok := authn.PrincipalFrom(c); ok && !time.Now().Before(p.IdleDeadline) {
			err = authz.ErrScopedCredentialRevoked
		} else {
			err = errors.Join(errMembershipAuthority, err)
		}
	}
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "session_revoked", "Sign in again before managing memberships.")
	case errors.Is(err, errRevocationActorNotAdmin), errors.Is(err, errGovernanceMembership):
		respond.Forbidden(c, "Current administration authority and the separate owner recovery process are required for governance membership.")
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "membership or removal receipt")
	case errors.Is(err, errMembershipConflict), errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "40P01"):
		respond.Conflict(c, "The membership changed or this command was reused. Refresh before continuing.")
	case errors.Is(err, errMembershipAuthority):
		respond.Error(c, 503, "authorization_unavailable", "Membership authority is unavailable. Retry the same command.")
	default:
		respond.InternalErr(c, err, "Could not process membership command. Retry the same command to reconcile its result.")
	}
}

func membershipCommandContext(c *gin.Context, p authn.Principal) (context.Context, context.CancelFunc) {
	until := time.Now().Add(5 * time.Second)
	if p.IdleDeadline.Before(until) {
		until = p.IdleDeadline
	}
	return context.WithDeadline(c.Request.Context(), until)
}

func membershipCommandPrincipal(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	p, tenant, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return p, tenant, false
	}
	c.Header("Cache-Control", "no-store")
	expected := c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(c.Request.Header.Values("X-Helivanta-Expected-Tenant")) != 1 || len(expected) != 1 || expected[0] != p.Subject {
		respond.Error(c, 409, "session_changed", "Your account or workspace changed. Refresh before managing memberships.")
		return authn.Principal{}, uuid.Nil, false
	}
	return p, tenant, true
}

func auditMembershipReceipt(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'membership_receipt_read','iam_membership_commands',?)`, p.TenantID, p.Subject, id).Error
}

func membershipActor(tx *gorm.DB, p authn.Principal, target string) error {
	if p.AuthTime.IsZero() || p.IdleDeadline.IsZero() || !time.Now().Before(p.IdleDeadline) {
		return authz.ErrScopedCredentialRevoked
	}
	if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, "iam.credential:"+p.Subject).Error; err != nil {
		return errors.Join(errMembershipAuthority, err)
	}
	var revoked bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_credential_revocations WHERE subject=? AND revoked_at>=?)`, p.Subject, p.AuthTime).Scan(&revoked).Error; err != nil {
		return errors.Join(errMembershipAuthority, err)
	}
	if revoked || !time.Now().Before(p.IdleDeadline) {
		return authz.ErrScopedCredentialRevoked
	}
	var rows []struct {
		Subject string
		RoleKey string
	}
	if err := tx.Table("iam_members").Select("subject,role_key").Where("tenant_id=? AND ((subject=? AND role_key=?) OR subject=?)", p.TenantID, p.Subject, authz.RoleTenantAdmin, target).Order("subject,role_key").Limit(len(authz.SystemRoles()) + 2).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
		return errors.Join(errMembershipAuthority, err)
	}
	if len(rows) > len(authz.SystemRoles())+1 {
		return errMembershipAuthority
	}
	for _, row := range rows {
		if row.Subject == p.Subject && row.RoleKey == string(authz.RoleTenantAdmin) {
			if !time.Now().Before(p.IdleDeadline) {
				return authz.ErrScopedCredentialRevoked
			}
			return nil
		}
	}
	return errRevocationActorNotAdmin
}

func (h *membershipCommandHandlers) put(c *gin.Context) {
	p, tenant, ok := membershipCommandPrincipal(c)
	if !ok {
		return
	}
	c.Header("Cache-Control", "no-store")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.Version() != 7 || id.String() != c.Param("id") {
		respond.NotFound(c, "membership command")
		return
	}
	var in MembershipCommandInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		respond.Error(c, 400, "invalid_request", "Provide a valid membership command.")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		respond.Error(c, 400, "invalid_request", "Provide one membership command.")
		return
	}
	if (in.Operation != "remove" && in.Operation != "recover") || in.BasisID == uuid.Nil || !proposalSubject(in.Subject) || !knownRole(in.RoleKey) || in.ExpectedMembershipEpoch < 1 || !utf8.ValidString(in.Reason) || strings.TrimSpace(in.Reason) == "" || utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsRune(in.Reason, 0) || (in.Operation == "recover" && in.RoleKey != string(authz.RoleStaffMember)) {
		respond.Error(c, 400, "invalid_request", "Provide the exact membership or removal receipt, current epoch and required reason.")
		return
	}
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	var saved membershipCommand
	if in.Operation == "recover" {
		var prior membershipCommand
		replay := false
		err = h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
			if err := membershipActor(tx, p, in.Subject); err != nil {
				return err
			}
			err := tx.Where("tenant_id=? AND id=?", tenant, id).Take(&saved).Error
			if err == nil {
				if saved.ActorSubject != p.Subject || saved.input() != in {
					return errMembershipConflict
				}
				replay = true
				return auditMembershipReceipt(tx, p, id)
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.Join(errMembershipAuthority, err)
			}
			return tx.Select("id").Where("tenant_id=? AND id=? AND operation='remove' AND subject=?", p.TenantID, in.BasisID, in.Subject).Take(&prior).Error
		})
		if err != nil {
			membershipCommandError(c, err)
			return
		}
		if replay {
			saved.CreatedAt = saved.CreatedAt.UTC()
			respond.Accepted(c, saved.MembershipCommand)
			return
		}
		if h.identity == nil {
			membershipCommandError(c, errMembershipAuthority)
			return
		}
		eligible, lookupErr := h.identity.MembershipEligible(ctx, in.Subject)
		if lookupErr != nil {
			membershipCommandError(c, errMembershipAuthority)
			return
		}
		if !eligible {
			respond.Conflict(c, "The account is not currently eligible. Verify it with the identity owner before recovery.")
			return
		}
	}
	err = h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "iam.membership-command:"+p.TenantID+":"+id.String()).Error; err != nil {
			return errors.Join(errMembershipAuthority, err)
		}
		if err := membershipActor(tx, p, in.Subject); err != nil {
			return err
		}
		err := tx.Where("tenant_id=? AND id=?", tenant, id).Take(&saved).Error
		if err == nil {
			if saved.ActorSubject != p.Subject || saved.input() != in {
				return errMembershipConflict
			}
			return auditMembershipReceipt(tx, p, id)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Join(errMembershipAuthority, err)
		}
		var epoch struct {
			Epoch       int64
			MemberCount int64
		}
		if err := tx.Table("iam_subject_membership_epochs").Select("epoch,member_count").Where("tenant_id=? AND subject=?", p.TenantID, in.Subject).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&epoch).Error; err != nil {
			return err
		}
		if epoch.Epoch != in.ExpectedMembershipEpoch {
			return errMembershipConflict
		}
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true),set_config('app.membership_command',?,true),set_config('app.membership_reason',?,true),set_config('app.membership_expected_epoch',?,true)`, p.Subject, id.String(), in.Reason, strconv.FormatInt(in.ExpectedMembershipEpoch, 10)).Error; err != nil {
			return err
		}
		subject := iamcontract.SubjectMemberRevoked
		eventType := "MemberRevoked"
		if in.Operation == "remove" {
			if in.RoleKey == string(authz.RoleTenantAdmin) {
				return errGovernanceMembership
			}
			var row member
			if err := tx.Select("id,subject,role_key").Where("tenant_id=? AND id=? AND subject=? AND role_key=?", tenant, in.BasisID, in.Subject, in.RoleKey).Take(&row).Error; err != nil {
				return err
			}
			removed := tx.Where("tenant_id=? AND id=? AND subject=? AND role_key=?", tenant, in.BasisID, in.Subject, in.RoleKey).Delete(&member{})
			if removed.Error != nil {
				return removed.Error
			}
			if removed.RowsAffected != 1 {
				return errMembershipConflict
			}
		} else {
			var prior membershipCommand
			if err := tx.Select("id").Where("tenant_id=? AND id=? AND operation='remove' AND subject=?", tenant, in.BasisID, in.Subject).Take(&prior).Error; err != nil {
				return err
			}
			var ordinary int64
			if err := tx.Table("iam_members").Where("tenant_id=? AND subject=? AND role_key='staff_member'", tenant, in.Subject).Count(&ordinary).Error; err != nil {
				return err
			}
			if ordinary != 0 {
				return errMembershipConflict
			}
			if err := tx.Exec(`SELECT set_config('app.membership_recovery_basis',?,true)`, in.BasisID.String()).Error; err != nil {
				return err
			}
			resultID, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if err := tx.Create(&member{ID: resultID, TenantID: tenant, Subject: in.Subject, RoleKey: string(authz.RoleStaffMember)}).Error; err != nil {
				return err
			}
			subject = iamcontract.SubjectMemberGranted
			eventType = "MemberGranted"
		}
		if err := tx.Where("tenant_id=? AND id=?", tenant, id).Take(&saved).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields) VALUES(?,?,?,'iam_membership_commands',?,ARRAY['role_key','membership_epoch'])`, tenant, p.Subject, "membership_"+in.Operation, id).Error; err != nil {
			return err
		}
		data, err := json.Marshal(iamcontract.MemberChangedData{Subject: in.Subject, RoleKey: in.RoleKey})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, subject, events.Event{Type: eventType, Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		membershipCommandError(c, err)
		return
	}
	saved.CreatedAt = saved.CreatedAt.UTC()
	respond.Accepted(c, saved.MembershipCommand)
}

type membershipCommandHistory struct {
	MembershipCommandHistory `gorm:"embedded"`
}

func (h membershipCommandHistory) PageKey() (time.Time, uuid.UUID) { return h.CreatedAt, h.ID }

func (h *membershipCommandHandlers) list(c *gin.Context, page pagination.Params) ([]membershipCommandHistory, error) {
	p, _, ok := membershipCommandPrincipal(c)
	if !ok {
		return nil, nil
	}
	c.Header("Cache-Control", "no-store")
	var rows []membershipCommandHistory
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, ""); err != nil {
			return err
		}
		query := tx.Table("iam_membership_commands").Select(`id,operation,basis_id,subject,role_key,expected_membership_epoch,reason,result_membership_id,result_membership_epoch,actor_subject,created_at,
 (SELECT e.epoch FROM iam_subject_membership_epochs e WHERE e.tenant_id=iam_membership_commands.tenant_id AND e.subject=iam_membership_commands.subject) AS current_membership_epoch,
 (operation='remove' AND NOT EXISTS(SELECT 1 FROM iam_membership_commands recovery WHERE recovery.tenant_id=iam_membership_commands.tenant_id AND recovery.operation='recover' AND recovery.basis_id=iam_membership_commands.id)
 AND NOT EXISTS(SELECT 1 FROM iam_members m WHERE m.tenant_id=iam_membership_commands.tenant_id AND m.subject=iam_membership_commands.subject AND m.role_key='staff_member')) AS recoverable`).Where("tenant_id=?", p.TenantID)
		if err := tenantdb.ApplyKeyset(query, page).Find(&rows).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'membership_history_read','iam_membership_commands',?)`, p.TenantID, p.Subject, p.TenantID).Error
	})
	if err != nil {
		membershipCommandError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i].CreatedAt = rows[i].CreatedAt.UTC()
	}
	return rows, nil
}

type membershipCandidate struct {
	MembershipCandidate `gorm:"embedded"`
}

func (c membershipCandidate) PageKey() (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }
func (h *membershipCommandHandlers) candidates(c *gin.Context, page pagination.Params) ([]membershipCandidate, error) {
	p, _, ok := membershipCommandPrincipal(c)
	if !ok {
		return nil, nil
	}
	c.Header("Cache-Control", "no-store")
	var rows []membershipCandidate
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, ""); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx.Table("iam_members").Select("id,subject,role_key,created_at,(SELECT e.epoch FROM iam_subject_membership_epochs e WHERE e.tenant_id=iam_members.tenant_id AND e.subject=iam_members.subject AND e.member_count>0) AS membership_epoch").Where("tenant_id=?", p.TenantID), page).Find(&rows).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'membership_candidates_read','iam_members',?)`, p.TenantID, p.Subject, p.TenantID).Error
	})
	if err != nil {
		membershipCommandError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i].CreatedAt = rows[i].CreatedAt.UTC()
	}
	return rows, nil
}
