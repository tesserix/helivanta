package iam

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

//go:embed membership_enrollments.sql
var membershipEnrollmentsSchema string

type membershipEnrollment struct {
	MembershipEnrollment `gorm:"embedded"`
	TenantID             uuid.UUID `json:"-"`
}

func (membershipEnrollment) TableName() string                 { return "iam_membership_enrollments" }
func (e membershipEnrollment) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }
func (e membershipEnrollment) input() MembershipEnrollmentInput {
	return MembershipEnrollmentInput{Subject: e.Subject, Reason: e.Reason}
}

func (m *Module) registerMembershipEnrollments(g *platform.Router, deps platform.Deps) {
	h := &membershipCommandHandlers{db: deps.DB, bus: deps.Bus, identity: m.membershipIdentity}
	g.PUT("/membership-enrollments/:id", PermMemberManage, h.enroll)
	g.GET("/membership-enrollments/:id", PermMemberManage, h.enrollment)
	platform.ListRoute(g, "/membership-enrollments", PermMemberManage, h.enrollments)
}

func enrollmentReplay(tx *gorm.DB, p authn.Principal, id uuid.UUID, in MembershipEnrollmentInput, saved *membershipEnrollment) (bool, error) {
	if err := membershipActor(tx, p, in.Subject); err != nil {
		return false, err
	}
	err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(saved).Error
	if err == nil {
		if saved.ActorSubject != p.Subject || saved.input() != in {
			return false, errMembershipConflict
		}
		return true, auditEnrollmentRead(tx, p, id)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, errors.Join(errMembershipAuthority, err)
	}
	var previous bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_subject_membership_epochs WHERE tenant_id=? AND subject=?)`, p.TenantID, in.Subject).Scan(&previous).Error; err != nil {
		return false, errors.Join(errMembershipAuthority, err)
	}
	if previous {
		return false, errMembershipConflict
	}
	return false, nil
}
func auditEnrollmentRead(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'membership_enrollment_read','iam_membership_enrollments',?)`, p.TenantID, p.Subject, id).Error
}
func (h *membershipCommandHandlers) enroll(c *gin.Context) {
	p, tenant, ok := membershipCommandPrincipal(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.Version() != 7 || id.String() != c.Param("id") {
		respond.NotFound(c, "enrollment command")
		return
	}
	var in MembershipEnrollmentInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		respond.Error(c, 400, "invalid_request", "Provide an account ID and required enrollment reason.")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		respond.Error(c, 400, "invalid_request", "Provide one enrollment command.")
		return
	}
	if !proposalSubject(in.Subject) || !utf8.ValidString(in.Reason) || strings.TrimSpace(in.Reason) == "" || utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsRune(in.Reason, 0) {
		respond.Error(c, 400, "invalid_request", "Provide a valid account ID and required enrollment reason.")
		return
	}
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	var saved membershipEnrollment
	replay := false
	err = h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		var err error
		replay, err = enrollmentReplay(tx, p, id, in, &saved)
		return err
	})
	if err != nil {
		membershipCommandError(c, err)
		return
	}
	if replay {
		saved.CreatedAt = saved.CreatedAt.UTC()
		respond.Accepted(c, saved.MembershipEnrollment)
		return
	}
	if h.identity == nil {
		membershipCommandError(c, errMembershipAuthority)
		return
	}
	eligible, err := h.identity.MembershipEligible(ctx, in.Subject)
	if err != nil {
		membershipCommandError(c, errMembershipAuthority)
		return
	}
	if !eligible {
		respond.Conflict(c, "The account is not currently eligible. Verify it with the identity owner before enrollment.")
		return
	}
	err = h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "iam.membership-enrollment:"+p.TenantID+":"+id.String()).Error; err != nil {
			return errors.Join(errMembershipAuthority, err)
		}
		replay, err := enrollmentReplay(tx, p, id, in, &saved)
		if err != nil || replay {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true),set_config('app.membership_enrollment',?,true),set_config('app.membership_reason',?,true)`, p.Subject, id.String(), in.Reason).Error; err != nil {
			return err
		}
		memberID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := tx.Create(&member{ID: memberID, TenantID: tenant, Subject: in.Subject, RoleKey: string(authz.RoleStaffMember)}).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id=? AND id=?", tenant, id).Take(&saved).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields) VALUES(?,?,'membership_enroll','iam_membership_enrollments',?,ARRAY['role_key','membership_epoch'])`, tenant, p.Subject, id).Error; err != nil {
			return err
		}
		data, err := json.Marshal(iamcontract.MemberChangedData{Subject: in.Subject, RoleKey: string(authz.RoleStaffMember)})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, iamcontract.SubjectMemberGranted, events.Event{Type: "MemberGranted", Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		membershipCommandError(c, err)
		return
	}
	saved.CreatedAt = saved.CreatedAt.UTC()
	respond.Accepted(c, saved.MembershipEnrollment)
}
func (h *membershipCommandHandlers) enrollment(c *gin.Context) {
	p, _, ok := membershipCommandPrincipal(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.Version() != 7 || id.String() != c.Param("id") {
		respond.NotFound(c, "enrollment receipt")
		return
	}
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	var saved membershipEnrollment
	err = h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, ""); err != nil {
			return err
		}
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&saved).Error; err != nil {
			return err
		}
		return auditEnrollmentRead(tx, p, id)
	})
	if err != nil {
		membershipCommandError(c, err)
		return
	}
	saved.CreatedAt = saved.CreatedAt.UTC()
	respond.OK(c, saved.MembershipEnrollment)
}
func (h *membershipCommandHandlers) enrollments(c *gin.Context, page pagination.Params) ([]membershipEnrollment, error) {
	p, _, ok := membershipCommandPrincipal(c)
	if !ok {
		return nil, nil
	}
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	var rows []membershipEnrollment
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, ""); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx.Where("tenant_id=?", p.TenantID), page).Find(&rows).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'membership_enrollments_read','iam_membership_enrollments',?)`, p.TenantID, p.Subject, p.TenantID).Error
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
