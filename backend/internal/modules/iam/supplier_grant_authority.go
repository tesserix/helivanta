package iam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type partnerGraph interface {
	ProjectPartnerAssignment(context.Context, authz.PartnerAssignment) error
	CheckPartnerAssignment(context.Context, authz.PartnerAssignment, string, authz.Permission) (bool, error)
}
type partnerScopeAuthority struct {
	db            *tenantdb.DB
	members       *partnerMemberHandlers
	relationships suppliertrade.CurrentRelationshipGuard
	graph         partnerGraph
}
type partnerRecipient struct {
	Home                      supplieraccess.SupplierGrant `json:"home"`
	Identity                  authz.StaffActorReference    `json:"identity"`
	PostingHash               string                       `json:"posting_hash"`
	MembershipEpoch           int64                        `json:"membership_epoch"`
	GroupVersion, CohortEpoch int64
	MemberID                  uuid.UUID
	MemberVersion             int64
}

func NewPartnerScopeAuthorizer(deps platform.Deps) authz.PartnerScopeAuthorizer {
	graph, _ := deps.Authz.(partnerGraph)
	return &partnerScopeAuthority{db: deps.DB, graph: graph, relationships: deps.TradingRelationships, members: &partnerMemberHandlers{partnerGroupHandlers: &partnerGroupHandlers{db: deps.DB, bus: deps.Bus}, authority: deps.ScopedAuthorizer, source: deps.PartnerMembers}}
}
func recipientDigest(r partnerRecipient) (string, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("helivanta.iam.partner-recipient.v1\n"), raw...))
	return hex.EncodeToString(sum[:]), nil
}
func (a *partnerScopeAuthority) currentRecipient(ctx context.Context, request authz.PartnerScopeRequest, action func(context.Context, partnerRecipient) error) error {
	p := request.Principal
	return a.members.transactionContext(ctx, p, partnerMemberRoute{hospital: request.HospitalID}, request.Permission, func(current context.Context, tx *gorm.DB, _ authz.StaffActorReference) error {
		if err := supplierGrantFrame(tx, "hospital", request.HospitalID); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, p.TenantID+":supplier-grant:"+request.GrantID.String()).Error; err != nil {
			return err
		}
		home, err := homeGrant(tx, p, request.HospitalID, request.GrantID)
		if err != nil {
			return err
		}
		template, ok := supplieraccess.TemplateFor(home.Input.TemplateKey)
		if !ok || template.Permission != string(request.Permission) || home.Scope.SourceProduct != request.Product || home.Status != "approved" || home.SignedBy == nil || home.RemoteSignedBy == nil || home.HomeBasisSha256 == nil || time.Now().Before(home.Input.ValidFrom) || !time.Now().Before(home.Input.ExpiresAt) {
			return authz.ErrScopedCommandDenied
		}
		var stored struct {
			Document  []byte
			BasisHash string
		}
		if err = tx.Table("iam_supplier_grant_bases").Select("document,basis_hash").Where("tenant_id=? AND hospital_id=? AND grant_id=?", p.TenantID, request.HospitalID, home.ID).Take(&stored).Error; err != nil {
			return err
		}
		var basis supplierHomeBasis
		if suppliertrade.DecodeObject(stored.Document, &basis) != nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		digest, _, err := basisDigest(basis)
		if err != nil || digest != stored.BasisHash || digest != *home.HomeBasisSha256 {
			return authz.ErrScopedAuthorizationUnavailable
		}
		snapshot := partnerRecipient{Home: home}
		var selected supplieraccess.SupplierGrantRecipientBasisInput
		var expected authz.StaffActorReference
		var expectedMembership int64
		if home.Input.RecipientKind == "subject" {
			if home.Input.RecipientSubject == nil || *home.Input.RecipientSubject != p.Subject || basis.Named == nil || basis.Identity == nil {
				return authz.ErrScopedCommandDenied
			}
			selected = *basis.Named
			expected = *basis.Identity
			expectedMembership = basis.MembershipEpoch
		} else {
			if home.Input.RecipientGroupID == nil || basis.GroupVersion < 2 {
				return authz.ErrScopedCommandDenied
			}
			groupID := *home.Input.RecipientGroupID
			if err = tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, p.TenantID+":partner-group:"+groupID.String()).Error; err != nil {
				return err
			}
			var locked struct{ ID uuid.UUID }
			if err = tx.Table("iam_partner_groups").Select("id").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, request.HospitalID, groupID).Clauses(clause.Locking{Strength: "SHARE"}).Take(&locked).Error; err != nil {
				return err
			}
			group, e := readPartnerGroup(tx, p.TenantID, groupID)
			if e != nil {
				return e
			}
			if group.HospitalID != request.HospitalID || group.Status != "approved" || group.Version != basis.GroupVersion || group.MembershipPolicyVersion != 1 || !slices.Contains(template.GroupPurposes, group.Purpose) || time.Now().Before(group.ValidFrom) || !time.Now().Before(group.ExpiresAt) {
				return authz.ErrScopedCommandDenied
			}
			var document struct{ Document []byte }
			if err = tx.Table("iam_partner_members").Select("document").Where("tenant_id=? AND hospital_id=? AND group_id=? AND subject=? AND status='approved'", p.TenantID, request.HospitalID, groupID, p.Subject).Clauses(clause.Locking{Strength: "SHARE"}).Take(&document).Error; err != nil {
				return err
			}
			var member PartnerGroupMemberProposal
			if suppliertrade.DecodeObject(document.Document, &member) != nil {
				return authz.ErrScopedAuthorizationUnavailable
			}
			if member.Status != "approved" || member.Input.Subject != p.Subject || member.GroupID != groupID || member.HospitalID != request.HospitalID || member.Input.ExpectedGroupVersion != group.Version || time.Now().Before(member.Input.ValidFrom) || !time.Now().Before(member.Input.ExpiresAt) {
				return authz.ErrScopedCommandDenied
			}
			selected = supplieraccess.SupplierGrantRecipientBasisInput{StaffID: member.Input.StaffID, PostingID: member.Input.PostingID, PostingRevisionID: member.Input.PostingRevisionID, PostingVersion: member.Input.PostingVersion, DepartmentID: member.Input.DepartmentID, DepartmentRevisionID: member.Input.DepartmentRevisionID}
			expected = authz.StaffActorReference{StaffID: member.MemberStaffID, LinkID: member.MemberLinkID, LinkVersion: member.MemberLinkVersion, GenerationID: member.MemberGenerationID, EmploymentEpoch: member.MemberEmploymentEpoch}
			expectedMembership = member.MemberMembershipEpoch
			snapshot.GroupVersion = group.Version
			snapshot.MemberID = member.ID
			snapshot.MemberVersion = member.Version
			if err = tx.Table("iam_partner_group_cohorts").Select("epoch").Where("tenant_id=? AND hospital_id=? AND group_id=?", p.TenantID, request.HospitalID, groupID).Scan(&snapshot.CohortEpoch).Error; err != nil {
				return err
			}
			if snapshot.CohortEpoch < 1 {
				return authz.ErrScopedAuthorizationUnavailable
			}
			if err = a.db.ConstrainGuardedCommand(current, p.TenantID, member.Input.ExpiresAt); err != nil {
				return err
			}
			if err = a.db.ConstrainGuardedCommand(current, p.TenantID, group.ExpiresAt); err != nil {
				return err
			}
		}
		scope := authz.PartnerMemberScope{MemberSubject: p.Subject, Posting: authz.StaffPostingSourceScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: request.HospitalID, StaffID: selected.StaffID, PostingID: selected.PostingID, RevisionID: selected.PostingRevisionID, Version: selected.PostingVersion, DepartmentID: selected.DepartmentID, DepartmentRevisionID: selected.DepartmentRevisionID}}
		return a.members.source.WithCurrentPartnerMember(current, scope, func(held context.Context, reference authz.PartnerMemberReference) error {
			epoch, e := proposalMembershipEpoch(tx, p.TenantID, p.Subject)
			if e != nil {
				return e
			}
			if reference.Subject != p.Subject || reference.Member != expected || epoch != expectedMembership {
				return authz.ErrScopedCommandDenied
			}
			snapshot.Identity = reference.Member
			snapshot.PostingHash = reference.Source.SourceHash
			snapshot.MembershipEpoch = epoch
			if e = a.db.ConstrainGuardedCommand(held, p.TenantID, home.Input.ExpiresAt); e != nil {
				return e
			}
			if e = grantReadAudit(tx, p, home.ID, "partner_grant_recipient_use"); e != nil {
				return e
			}
			return action(held, snapshot)
		})
	})
}
func (a *partnerScopeAuthority) sourceSnapshot(ctx context.Context, recipient partnerRecipient) (supplieraccess.SupplierGrant, error) {
	var source supplieraccess.SupplierGrant
	err := a.db.WithTenant(ctx, recipient.Home.Scope.SourceTenantID.String(), func(tx *gorm.DB) error {
		if err := supplierGrantFrame(tx, "consume", uuid.Nil); err != nil {
			return err
		}
		var e error
		source, e = loadSupplierGrant(tx, recipient.Home.Scope.SourceTenantID.String(), recipient.Home.ID)
		return e
	})
	return source, err
}
func grantsMatch(source, home supplieraccess.SupplierGrant) bool {
	return supplieraccess.ValidateRecord(source) == nil && supplieraccess.ValidateRecord(home) == nil && source.Side == "source" && home.Side == "hospital" && source.Status == "approved" && home.Status == "approved" && source.ID == home.ID && source.Scope == home.Scope && source.InputSha256 == home.InputSha256 && source.AuthorityEpoch == home.RemoteEpoch && source.RemoteEpoch == home.AuthorityEpoch && source.HomeBasisSha256 != nil && home.HomeBasisSha256 != nil && *source.HomeBasisSha256 == *home.HomeBasisSha256 && source.SignedBy != nil && home.SignedBy != nil && source.RemoteSignedBy != nil && home.RemoteSignedBy != nil && *source.SignedBy == *home.RemoteSignedBy && *source.RemoteSignedBy == *home.SignedBy
}
func (a *partnerScopeAuthority) WithPartnerScope(ctx context.Context, request authz.PartnerScopeRequest, action func(context.Context, authz.PartnerScopeGrant) error) (err error) {
	p := request.Principal
	if a.db == nil || a.members == nil || a.members.authority == nil || a.members.source == nil || a.relationships == nil || a.graph == nil || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if a.db.GuardedCommandActive(ctx) || p.TenantID == "" || !proposalSubject(p.Subject) || p.AuthTime.IsZero() || !p.IdleDeadline.After(time.Now()) || request.HospitalID == uuid.Nil || request.GrantID.Version() != 7 || (request.Product != "supply" && request.Product != "pharmacy") || !slices.Contains([]authz.Permission{authz.PartnerCatalogueRead, authz.PartnerShipmentRead, authz.PartnerInvoiceRead}, request.Permission) {
		return authz.ErrInvalidScopedAssignment
	}
	defer func() {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, supplieraccess.ErrIneligible) || errors.Is(err, suppliertrade.ErrIneligible) || errors.Is(err, suppliertrade.ErrConflict) {
			err = authz.ErrScopedCommandDenied
		}
	}()
	var recipient partnerRecipient
	if err = a.currentRecipient(ctx, request, func(_ context.Context, value partnerRecipient) error { recipient = value; return nil }); err != nil {
		return err
	}
	source, err := a.sourceSnapshot(ctx, recipient)
	if err != nil {
		return err
	}
	if !grantsMatch(source, recipient.Home) {
		return authz.ErrScopedCommandDenied
	}
	relationship, err := a.relationships.VerifyRelationship(ctx, suppliertrade.RelationshipScope{TenantID: source.Scope.SourceTenantID.String(), ID: source.Input.RelationshipID, TermsSha256: source.Input.TermsSha256, Purpose: "new_business"})
	if err != nil {
		return err
	}
	recipientHash, err := recipientDigest(recipient)
	if err != nil {
		return err
	}
	assignment := authz.PartnerAssignment{Grant: source, Subject: p.Subject, MembershipEpoch: recipient.MembershipEpoch, RecipientHash: recipientHash}
	if err = a.graph.ProjectPartnerAssignment(ctx, assignment); err != nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	allowed, err := a.graph.CheckPartnerAssignment(ctx, assignment, p.Subject, request.Permission)
	if err != nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if !allowed {
		return authz.ErrScopedCommandDenied
	}
	projectionHash, err := assignment.ProjectionHash()
	if err != nil {
		return err
	}
	// Network/graph verification has finished before either owner's final locks.
	return a.currentRecipient(ctx, request, func(homeCtx context.Context, current partnerRecipient) error {
		currentHash, e := recipientDigest(current)
		if e != nil {
			return e
		}
		if currentHash != recipientHash {
			return authz.ErrScopedCommandDenied
		}
		deadline := time.Now().Add(5 * time.Second)
		if until, ok := homeCtx.Deadline(); ok && until.Before(deadline) {
			deadline = until
		}
		base, cancel := context.WithDeadline(tenantdb.WithActor(ctx, p.Subject), deadline)
		defer cancel()
		stop := context.AfterFunc(homeCtx, cancel)
		defer stop()
		return a.db.WithGuardedTenantCommand(base, source.Scope.SourceTenantID.String(), deadline, func(tx *gorm.DB) error {
			if e := supplierGrantFrame(tx, "consume", uuid.Nil); e != nil {
				return e
			}
			if e := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, source.Scope.SourceTenantID.String()+":supplier-grant:"+source.ID.String()).Error; e != nil {
				return e
			}
			latest, e := loadSupplierGrant(tx, source.Scope.SourceTenantID.String(), source.ID)
			if e != nil {
				return e
			}
			if latest.Version != source.Version || !grantsMatch(latest, current.Home) {
				return authz.ErrScopedCommandDenied
			}
			return nil
		}, func(sourceCtx context.Context) error {
			return a.relationships.WithVerifiedRelationship(sourceCtx, relationship, func(held context.Context, trade suppliertrade.Relationship) error {
				if !supplieraccess.Current(source, trade, time.Now()) {
					return authz.ErrScopedCommandDenied
				}
				return a.db.WithTenantCommand(held, source.Scope.SourceTenantID.String(), func(tx *gorm.DB) error {
					now := time.Now().UTC()
					if e := tx.Exec(`INSERT INTO iam_supplier_grant_projection_acks(tenant_id,grant_id,hospital_id,projection_hash,subject,version,authority_epoch,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?)ON CONFLICT DO NOTHING`, source.Scope.SourceTenantID, source.ID, source.Scope.HospitalID, projectionHash, p.Subject, source.Version, source.AuthorityEpoch, now, now).Error; e != nil {
						return e
					}
					if e := tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'partner_grant_disclosure_use','iam_supplier_grants',?)`, source.Scope.SourceTenantID, p.Subject, source.ID).Error; e != nil {
						return e
					}
					if e := supplierGrantFrame(tx, "", uuid.Nil); e != nil {
						return e
					}
					until, _ := held.Deadline()
					return action(held, authz.PartnerScopeGrant{SourceTenantID: source.Scope.SourceTenantID.String(), RecipientTenantID: p.TenantID, HospitalID: request.HospitalID, GrantID: source.ID, RelationshipID: source.Input.RelationshipID, Product: source.Scope.SourceProduct, SourceOrganizationID: source.Scope.SourceOrganizationID, SourceLocationID: source.Scope.SourceLocationID, BuyerOrganizationID: trade.Terms.Buyer.OrganizationID, BuyerLocationID: trade.Terms.Buyer.LocationID, Category: trade.Terms.Category, Permission: request.Permission, ProjectionHash: projectionHash, ValidUntil: until})
				})
			})
		})
	})
}

func (a *partnerScopeAuthority) WithCurrentPartnerRecipient(ctx context.Context, request authz.PartnerScopeRequest, action func(context.Context) error) error {
	if a.db == nil || a.members == nil || a.members.authority == nil || a.members.source == nil || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return a.currentRecipient(ctx, request, func(held context.Context, _ partnerRecipient) error { return action(held) })
}
