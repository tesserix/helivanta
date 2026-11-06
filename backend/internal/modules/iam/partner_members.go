package iam

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

//go:embed partner_members.sql
var partnerMembersSchema string

type partnerMemberHandlers struct {
	*partnerGroupHandlers
	authority authz.ScopedAuthorizer
	source    authz.CurrentPartnerMemberGuard
}

type partnerMemberRoute struct{ hospital, group, member, command uuid.UUID }

func (m *Module) registerPartnerMembers(r *platform.Router, deps platform.Deps) {
	h := &partnerMemberHandlers{partnerGroupHandlers: &partnerGroupHandlers{db: deps.DB, bus: deps.Bus}, authority: deps.ScopedAuthorizer, source: deps.PartnerMembers}
	path := "/hospitals/:hospital/partner-groups/:group/member-proposals/:member"
	r.GET("/hospitals/:hospital/partner-member-capabilities", authz.Public, h.capabilities)
	platform.ListRoute(r, "/hospitals/:hospital/partner-member-candidates", authz.Public, h.listCandidates)
	platform.ListRoute(r, "/hospitals/:hospital/partner-groups", authz.Public, h.listGroups)
	platform.ListRoute(r, "/hospitals/:hospital/partner-groups/:group/member-proposals", authz.Public, h.listMembers)
	r.PUT(path, authz.Public, h.putMember)
	r.GET(path, authz.Public, h.getMember)
	r.PUT(path+"/commands/:command", authz.Public, h.putMemberCommand)
	r.GET(path+"/commands/:command", authz.Public, h.getMemberCommand)
}

func partnerMemberPrincipal(c *gin.Context) (authn.Principal, partnerMemberRoute, bool) {
	if !authn.Refresh(c) {
		return authn.Principal{}, partnerMemberRoute{}, false
	}
	p, _, ok := membershipCommandPrincipal(c)
	var route partnerMemberRoute
	if !ok {
		return p, route, false
	}
	for _, part := range []struct {
		name   string
		target *uuid.UUID
	}{{"hospital", &route.hospital}, {"group", &route.group}, {"member", &route.member}, {"command", &route.command}} {
		raw := c.Param(part.name)
		if part.name != "hospital" && raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil || id.String() != raw || (part.name != "hospital" && (id.Version() != 7 || id.Variant() != uuid.RFC4122)) {
			respond.NotFound(c, "partner member")
			return p, route, false
		}
		*part.target = id
	}
	return p, route, true
}

func partnerMemberDecode(c *gin.Context, into any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(into) != nil || !errors.Is(decoder.Decode(&extra), io.EOF) {
		respond.Error(c, 400, "invalid_request", "Provide one bounded membership request.")
		return false
	}
	return true
}
func partnerMemberHash(subject string, input any) (string, error) {
	data, err := json.Marshal(struct {
		Subject string
		Input   any
	}{subject, input})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func (h *partnerMemberHandlers) transaction(c *gin.Context, p authn.Principal, r partnerMemberRoute, permission authz.Permission, action func(context.Context, *gorm.DB, authz.StaffActorReference) error) error {
	guard, ok := h.authority.(authz.CompoundScopedCommandGuard)
	if !ok || h.source == nil || p.IdleDeadline.IsZero() {
		return authz.ErrScopedAuthorizationUnavailable
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	checks := []authz.ScopeCheck{}
	for _, perm := range []authz.Permission{permission, authz.PartnerMemberSourceUse, authz.StaffPostingSourceUse, authz.StaffActorUse} {
		request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: r.hospital.String(), Permission: perm}
		grant, err := h.authority.AuthorizeScope(ctx, request)
		if err != nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if grant.AssignmentID == "" || grant.ScopeKind != "hospital" {
			return authz.ErrScopedCommandDenied
		}
		checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
	}
	return guard.WithScopeGrants(ctx, checks, p.AuthTime, func(current context.Context) error {
		return h.source.WithCurrentPartnerActor(current, authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: r.hospital}, func(actorCtx context.Context, actor authz.StaffActorReference) error {
			return h.db.WithTenantCommand(actorCtx, p.TenantID, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.iam_partner_groups','read',true),set_config('app.iam_partner_members','write',true),set_config('app.iam_partner_hospital',?,true),set_config('app.actor_subject',?,true)`, r.hospital.String(), p.Subject).Error; err != nil {
					return err
				}
				if err := assignmentAccess(tx); err != nil {
					return err
				}
				return action(actorCtx, tx, actor)
			})
		})
	})
}
func memberScope(p authn.Principal, r partnerMemberRoute, in PartnerMemberProposalInput) authz.PartnerMemberScope {
	return authz.PartnerMemberScope{MemberSubject: in.Subject, Posting: authz.StaffPostingSourceScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: r.hospital, StaffID: in.StaffID, PostingID: in.PostingID, RevisionID: in.PostingRevisionID, Version: in.PostingVersion, DepartmentID: in.DepartmentID, DepartmentRevisionID: in.DepartmentRevisionID}}
}
func readPartnerMember(tx *gorm.DB, tenant string, r partnerMemberRoute) (PartnerGroupMemberProposal, error) {
	var row struct{ Document []byte }
	var member PartnerGroupMemberProposal
	err := tx.Table("iam_partner_members").Select("document").Where("tenant_id=? AND hospital_id=? AND group_id=? AND id=?", tenant, r.hospital, r.group, r.member).Take(&row).Error
	if err != nil {
		return member, err
	}
	err = json.Unmarshal(row.Document, &member)
	return member, err
}
func lockPartnerMemberGroup(tx *gorm.DB, tenant string, r partnerMemberRoute) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, tenant+":partner-group:"+r.group.String()).Error
}
func memberGroupCurrent(tx *gorm.DB, tenant string, r partnerMemberRoute, in PartnerMemberProposalInput) error {
	g, err := readPartnerGroup(tx, tenant, r.group)
	if err != nil {
		return err
	}
	if g.HospitalID != r.hospital {
		return gorm.ErrRecordNotFound
	}
	if g.Status != "approved" || g.MembershipPolicyVersion != 1 || g.Version != in.ExpectedGroupVersion || in.ValidFrom.Before(g.ValidFrom) || in.ExpiresAt.After(g.ExpiresAt) || !in.ExpiresAt.After(time.Now()) {
		return errPartnerGroupConflict
	}
	return nil
}
func (h *partnerMemberHandlers) putMember(c *gin.Context) {
	p, r, ok := partnerMemberPrincipal(c)
	if !ok {
		return
	}
	var in PartnerMemberProposalInput
	if !partnerMemberDecode(c, &in) {
		return
	}
	in.ValidFrom = in.ValidFrom.UTC().Truncate(time.Microsecond)
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	if in.Kind != "subject" || !proposalSubject(in.Subject) || !in.SourcesExamined || in.StaffID == uuid.Nil || in.PostingID == uuid.Nil || in.PostingRevisionID == uuid.Nil || in.DepartmentID == uuid.Nil || in.DepartmentRevisionID == uuid.Nil || in.PostingVersion < 1 || in.ExpectedGroupVersion < 1 || in.ValidFrom.IsZero() || !in.ExpiresAt.After(in.ValidFrom) || !partnerGroupText(in.Reason, 500) {
		respond.Error(c, 400, "invalid_request", "Provide the exact named Staff posting, group version, validity and reviewed reason.")
		return
	}
	hash, err := partnerMemberHash(p.Subject, in)
	if err != nil {
		partnerGroupError(c, err)
		return
	}
	var saved PartnerGroupMemberProposal
	err = h.transaction(c, p, r, PermPartnerGroupMemberPrepare, func(ctx context.Context, tx *gorm.DB, actor authz.StaffActorReference) error {
		// Recovery precedes target revalidation: a completed nomination remains recoverable after exit.
		existing, e := readPartnerMember(tx, p.TenantID, r)
		if e == nil {
			var original string
			if e = tx.Table("iam_partner_members").Select("input_hash").Where("tenant_id=? AND id=?", p.TenantID, r.member).Scan(&original).Error; e != nil {
				return e
			}
			if original != hash {
				return errPartnerGroupConflict
			}
			saved = existing
			return partnerGroupReceiptAudit(tx, p, r.member, "partner_member_receipt_read")
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		return h.source.WithCurrentPartnerMember(ctx, memberScope(p, r, in), func(_ context.Context, ref authz.PartnerMemberReference) error {
			if actor.StaffID == ref.Member.StaffID || p.Subject == in.Subject {
				return errAssignmentDenied
			}
			if in.ExpiresAt.After(ref.SourceUntil) {
				return errPartnerGroupConflict
			}
			if e := lockPartnerMemberGroup(tx, p.TenantID, r); e != nil {
				return e
			}
			// A concurrent exact retry may have completed while source eligibility was checked.
			existing, e = readPartnerMember(tx, p.TenantID, r)
			if e == nil {
				var original string
				if e = tx.Table("iam_partner_members").Select("input_hash").Where("tenant_id=? AND id=?", p.TenantID, r.member).Scan(&original).Error; e != nil {
					return e
				}
				if original != hash {
					return errPartnerGroupConflict
				}
				saved = existing
				return partnerGroupReceiptAudit(tx, p, r.member, "partner_member_receipt_read")
			}
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if e := memberGroupCurrent(tx, p.TenantID, r, in); e != nil {
				return e
			}
			epoch, e := proposalMembershipEpoch(tx, p.TenantID, in.Subject)
			if e != nil {
				return e
			}
			if epoch < 1 {
				return authz.ErrScopedCommandDenied
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			saved = PartnerGroupMemberProposal{ID: r.member, GroupID: r.group, HospitalID: r.hospital, Input: in, MemberStaffID: ref.Member.StaffID, MemberDisplayName: ref.DisplayName, MemberLinkID: ref.Member.LinkID, MemberLinkVersion: ref.Member.LinkVersion, MemberGenerationID: ref.Member.GenerationID, MemberEmploymentEpoch: ref.Member.EmploymentEpoch, MemberMembershipEpoch: epoch, PreparedSourceUseID: ref.UseID, SourceCheckedAt: ref.CheckedAt, SourceUntil: ref.SourceUntil, PreparedBy: p.Subject, PreparedByStaffID: actor.StaffID, PreparedByLinkID: actor.LinkID, PreparedByGenerationID: actor.GenerationID, Status: "pending_review", Version: 1, CreatedAt: now, UpdatedAt: now}
			doc, e := json.Marshal(saved)
			if e != nil {
				return e
			}
			if e = tx.Exec(`INSERT INTO iam_partner_members(tenant_id,hospital_id,group_id,id,subject,staff_id,input_hash,document,status,version,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?::jsonb,'pending_review',1,?,?)`, p.TenantID, r.hospital, r.group, r.member, in.Subject, ref.Member.StaffID, hash, string(doc), now, now).Error; e != nil {
				return e
			}
			return h.memberChanged(tx, p, saved, 0, "partner_member_prepared")
		})
	})
	if err != nil {
		partnerMemberError(c, err)
		return
	}
	respond.OK(c, saved)
}
func partnerMemberError(c *gin.Context, err error) {
	if errors.Is(err, authz.ErrScopedCommandDenied) {
		respond.Forbidden(c, "Current hospital membership authority and independent Staff are required.")
		return
	}
	partnerGroupError(c, err)
}
func (h *partnerMemberHandlers) getMember(c *gin.Context) {
	p, r, ok := partnerMemberPrincipal(c)
	if !ok {
		return
	}
	var saved PartnerGroupMemberProposal
	err := h.transaction(c, p, r, PermPartnerGroupMemberRead, func(_ context.Context, tx *gorm.DB, _ authz.StaffActorReference) error {
		var err error
		saved, err = readPartnerMember(tx, p.TenantID, r)
		if err != nil {
			return err
		}
		return partnerGroupReceiptAudit(tx, p, r.member, "partner_member_read")
	})
	if err != nil {
		partnerMemberError(c, err)
		return
	}
	respond.OK(c, saved)
}
func (h *partnerMemberHandlers) memberChanged(tx *gorm.DB, p authn.Principal, member PartnerGroupMemberProposal, epoch int64, action string) error {
	if err := partnerGroupReceiptAudit(tx, p, member.ID, action); err != nil {
		return err
	}
	if h.bus == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	data, err := json.Marshal(iamcontract.PartnerGroupMemberChangedData{GroupID: member.GroupID, HospitalID: member.HospitalID, MemberID: member.ID, MemberVersion: member.Version, Status: member.Status, CohortEpoch: epoch})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, iamcontract.SubjectPartnerGroupMemberChanged, events.Event{Type: iamcontract.SubjectPartnerGroupMemberChanged, Version: 1, TenantID: p.TenantID, Data: data})
}
