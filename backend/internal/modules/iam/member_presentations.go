package iam

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errMemberPresentationChanged = errors.New("member presentation source changed")

type memberPresentationMembership struct {
	ID      uuid.UUID
	Subject string
	RoleKey string
}

type memberPresentationHandlers struct {
	db         *tenantdb.DB
	identities AssignmentIdentityReader
}

func (m *Module) registerMemberPresentations(r *platform.Router, deps platform.Deps) {
	h := &memberPresentationHandlers{db: deps.DB, identities: m.assignmentIdentity}
	r.POST("/member-presentations", PermMemberManage, h.read)
}

func (h *memberPresentationHandlers) snapshot(ctx context.Context, p authn.Principal, subjects []string, action string, expected []memberPresentationMembership) ([]memberPresentationMembership, error) {
	var rows []memberPresentationMembership
	allSubjects := slices.Clone(subjects)
	if !slices.Contains(allSubjects, p.Subject) {
		allSubjects = append(allSubjects, p.Subject)
	}
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, p.Subject); err != nil {
			return err
		}
		if err := tx.Table("iam_members").Select("id,subject,role_key").Where("tenant_id=? AND subject IN ?", p.TenantID, allSubjects).Order("subject,role_key,id").Limit(len(allSubjects)*len(authz.SystemRoles()) + 1).Clauses(clause.Locking{Strength: "SHARE"}).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > len(allSubjects)*len(authz.SystemRoles()) {
			return errMembershipAuthority
		}
		found := map[string]bool{}
		for _, row := range rows {
			found[row.Subject] = true
		}
		if len(found) != len(allSubjects) {
			return gorm.ErrRecordNotFound
		}
		if expected != nil && !slices.Equal(expected, rows) {
			return errMemberPresentationChanged
		}
		for _, subject := range subjects {
			id := rows[slices.IndexFunc(rows, func(row memberPresentationMembership) bool { return row.Subject == subject })].ID
			if err := tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,?,'iam_members',?)`, p.TenantID, p.Subject, action, id).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return rows, err
}

func memberPresentationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Unauthenticated(c, "Sign in again before viewing account details.")
	case errors.Is(err, errRevocationActorNotAdmin):
		respond.Forbidden(c, "Current tenant administration is required to view member details.")
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "organization member")
	case errors.Is(err, errMemberPresentationChanged):
		respond.Conflict(c, "The organization membership changed. Refresh before choosing a person.")
	default:
		respond.Error(c, 503, "identity_unavailable", "Account details are temporarily unavailable.")
	}
}

func (h *memberPresentationHandlers) read(c *gin.Context) {
	p, tenant, ok := membershipCommandPrincipal(c)
	if !ok {
		return
	}
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	if !authn.Refresh(c) || !authz.Refresh(c, PermMemberManage) {
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.ContentType() != "application/json" {
		respond.Error(c, 400, "invalid_request", "Request up to twenty distinct organization accounts.")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	var in MemberPresentationRequest
	if err != nil || governanceRequiredBody(raw, []string{"subjects"}) != nil || json.Unmarshal(raw, &in) != nil || len(in.Subjects) < 1 || len(in.Subjects) > 20 {
		respond.Error(c, 400, "invalid_request", "Request up to twenty distinct organization accounts.")
		return
	}
	unique := map[string]bool{}
	for _, subject := range in.Subjects {
		if !proposalSubject(subject) || unique[subject] {
			respond.Error(c, 400, "invalid_request", "Choose distinct valid organization accounts.")
			return
		}
		unique[subject] = true
	}
	if h.db == nil || h.identities == nil {
		memberPresentationError(c, errMembershipAuthority)
		return
	}
	before, err := h.snapshot(ctx, p, in.Subjects, "iam.member_presentation.requested", nil)
	if err != nil {
		memberPresentationError(c, err)
		return
	}
	out := MemberPresentations{TenantID: tenant, ActorSubject: p.Subject, Data: make([]MemberPresentation, len(in.Subjects))}
	group, lookupCtx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for index, subject := range in.Subjects {
		group.Go(func() error {
			identity, err := h.identities.AssignmentCandidateIdentity(lookupCtx, p.TenantID, subject)
			if err != nil || identity != nil && identity.Validate() != nil {
				return errMembershipAuthority
			}
			roles := []authz.RoleBinding{}
			for _, row := range before {
				if row.Subject == subject {
					roles = append(roles, authz.RoleBinding{TenantID: p.TenantID, Role: authz.Role(row.RoleKey)})
				}
			}
			view := MemberPresentation{Subject: subject, Roles: []MemberPresentationRole{}}
			for _, role := range rolesInTenant(roles, p.TenantID) {
				view.Roles = append(view.Roles, MemberPresentationRole(role))
			}
			if identity != nil {
				name, email := identity.DisplayName, identity.Email
				view.Name = &name
				view.Email = &email
				view.Available = true
			}
			out.Data[index] = view
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		memberPresentationError(c, err)
		return
	}
	if !authn.Refresh(c) || !authz.Refresh(c, PermMemberManage) {
		return
	}
	_, err = h.snapshot(ctx, p, in.Subjects, "iam.member_presentation.read", before)
	if err != nil {
		memberPresentationError(c, err)
		return
	}
	if ctx.Err() != nil {
		memberPresentationError(c, ctx.Err())
		return
	}
	respond.OK(c, out)
}
