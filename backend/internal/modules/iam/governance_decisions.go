package iam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type governanceDecisionRow struct {
	InputHash, ActorSubject string
	Receipt                 json.RawMessage
}

func loadGovernanceDecision(tx *gorm.DB, p authn.Principal, parent, command uuid.UUID, hash string) (GovernanceGrantDecisionReceipt, bool, error) {
	var row governanceDecisionRow
	err := tx.Table("iam_governance_decisions").Select("input_hash,actor_subject,receipt").Where("tenant_id=? AND grant_id=? AND id=?", p.TenantID, parent, command).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return GovernanceGrantDecisionReceipt{}, false, nil
	}
	if err != nil {
		return GovernanceGrantDecisionReceipt{}, false, err
	}
	if row.ActorSubject != p.Subject || row.InputHash != hash {
		return GovernanceGrantDecisionReceipt{}, false, errGovernanceConflict
	}
	var receipt GovernanceGrantDecisionReceipt
	err = json.Unmarshal(row.Receipt, &receipt)
	return receipt, err == nil, err
}

func governanceDecisionParent(tx *gorm.DB, p authn.Principal, parent uuid.UUID, in GovernanceGrantDecisionInput, authority governanceReviewAuthority) (GovernanceGrantReceipt, error) {
	row, err := loadGovernance(tx, p.TenantID, parent)
	if err != nil {
		return GovernanceGrantReceipt{}, err
	}
	var receipt GovernanceGrantReceipt
	if err = json.Unmarshal(row.Receipt, &receipt); err != nil {
		return receipt, err
	}
	receipt.Status, receipt.Version, receipt.UpdatedAt = row.Status, row.Version, row.UpdatedAt
	if p.Subject == receipt.Input.Subject || (p.Subject == receipt.IssuedBy && (in.Decision != "revoke" || authority.Kind != "owner")) {
		return receipt, errGovernanceDenied
	}
	if row.Version != in.ExpectedVersion {
		return receipt, errGovernanceConflict
	}
	switch in.Decision {
	case "approve", "reject":
		if row.Status != "pending_review" && row.Status != "provisional_active" {
			return receipt, errGovernanceConflict
		}
	case "revoke":
		if row.Status != "pending_review" && row.Status != "provisional_active" && row.Status != "active" {
			return receipt, errGovernanceConflict
		}
	default:
		return receipt, errGovernanceInput
	}
	return receipt, nil
}

func (h *governanceHandlers) decide(c *gin.Context) {
	p, ok := governancePrincipal(c, PermGovernanceReview)
	if !ok {
		return
	}
	parent, validParent := staffAccountID(c.Param("id"))
	command, validCommand := staffAccountID(c.Param("command"))
	if !validParent || !validCommand {
		governanceError(c, gorm.ErrRecordNotFound)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, governanceMaxRequestBytes))
	if err != nil || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		governanceError(c, errGovernanceInput)
		return
	}
	input, err := DecodeGovernanceDecision(raw)
	if err != nil {
		governanceError(c, err)
		return
	}
	if input.ExpectedVersion >= 9007199254740991 {
		governanceError(c, errGovernanceInput)
		return
	}
	raw, err = json.Marshal(input)
	if err != nil {
		governanceError(c, err)
		return
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	var result GovernanceGrantDecisionReceipt
	var target GovernanceGrantReceipt
	found := false
	err = h.withReviewer(c.Request.Context(), p, parent, false, PermGovernanceReview, func(_ context.Context, tx *gorm.DB, _ []string, authority governanceReviewAuthority) error {
		var err error
		result, found, err = loadGovernanceDecision(tx, p, parent, command, hash)
		if err != nil {
			return err
		}
		if found {
			return governanceAudit(tx, p, parent, "iam.governance.decision.receipt.read")
		}
		target, err = governanceDecisionParent(tx, p, parent, input, authority)
		return err
	})
	if err != nil {
		governanceError(c, err)
		return
	}
	if found {
		c.JSON(http.StatusAccepted, result)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	if input.Decision == "approve" {
		if err = requireNewGrantIdentity(ctx, h.identity, p.TenantID, target.Input.Subject); err != nil {
			governanceError(c, err)
			return
		}
	}
	err = h.withReviewer(ctx, p, parent, true, PermGovernanceReview, func(commandCtx context.Context, tx *gorm.DB, owners []string, authority governanceReviewAuthority) error {
		var err error
		result, found, err = loadGovernanceDecision(tx, p, parent, command, hash)
		if err != nil {
			return err
		}
		if found {
			return governanceAudit(tx, p, parent, "iam.governance.decision.receipt.read")
		}
		current, err := governanceDecisionParent(tx, p, parent, input, authority)
		if err != nil {
			return err
		}
		if current.Input.Subject != target.Input.Subject {
			return errGovernanceConflict
		}
		if input.Decision == "approve" {
			var members []member
			if err := tx.Select("id,role_key").Where("tenant_id=? AND subject=?", p.TenantID, current.Input.Subject).Order("role_key").Limit(len(authz.SystemRoles()) + 1).Clauses(clause.Locking{Strength: "SHARE"}).Find(&members).Error; err != nil {
				return err
			}
			if len(members) == 0 {
				return gorm.ErrRecordNotFound
			}
			if len(members) > len(authz.SystemRoles()) {
				return authz.ErrScopedAuthorizationUnavailable
			}
			epoch, err := proposalMembershipEpoch(tx, p.TenantID, current.Input.Subject)
			if err != nil {
				return err
			}
			if epoch != current.Input.ExpectedMembershipEpoch {
				return errGovernanceConflict
			}
			var expired bool
			if err := tx.Raw(`SELECT clock_timestamp()>=?::timestamptz`, current.Input.ExpiresAt).Scan(&expired).Error; err != nil {
				return err
			}
			if expired {
				return errGovernanceConflict
			}
			if h.topology == nil {
				return authz.ErrScopedAuthorizationUnavailable
			}
			return h.topology.WithGovernanceTopology(commandCtx, authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: current.Input.ScopeKind, ID: current.Input.ScopeID}, func(_ context.Context, ref authz.GovernanceTopologyReference) error {
				if ref.HospitalCount > 1 && len(owners) < 2 {
					return errGovernanceDenied
				}
				result, err = h.recordGovernanceDecision(tx, p, current, command, input, hash, owners, authority)
				return err
			})
		}
		result, err = h.recordGovernanceDecision(tx, p, current, command, input, hash, owners, authority)
		return err
	})
	if err != nil {
		governanceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, result)
}

func (h *governanceHandlers) recordGovernanceDecision(tx *gorm.DB, p authn.Principal, parent GovernanceGrantReceipt, command uuid.UUID, input GovernanceGrantDecisionInput, hash string, owners []string, authority governanceReviewAuthority) (GovernanceGrantDecisionReceipt, error) {
	if h.bus == nil {
		return GovernanceGrantDecisionReceipt{}, authz.ErrScopedAuthorizationUnavailable
	}
	var now time.Time
	if err := tx.Raw(`SELECT GREATEST(clock_timestamp(),updated_at+interval '1 microsecond') FROM iam_governance_grants WHERE tenant_id=? AND id=?`, p.TenantID, parent.ID).Scan(&now).Error; err != nil {
		return GovernanceGrantDecisionReceipt{}, err
	}
	result := GovernanceGrantDecisionReceipt{ID: command, TenantID: parent.TenantID, GrantID: parent.ID, Input: input, ResultVersion: input.ExpectedVersion + 1, ActorSubject: p.Subject, CreatedAt: now.UTC()}
	raw, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if err := tx.Exec(`SELECT set_config('app.iam_governance_command','decision',true),set_config('app.iam_governance_decision_id',?,true)`, command.String()).Error; err != nil {
		return result, err
	}
	var sourceVersion *int64
	if authority.GrantID != nil {
		sourceVersion = &authority.GrantVersion
	}
	if err := tx.Exec(`INSERT INTO iam_governance_decisions(tenant_id,id,grant_id,grant_subject,expected_version,result_version,decision,actor_subject,actor_membership_epoch,authority_kind,authority_grant_id,authority_grant_version,input_hash,receipt,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?,?)`, p.TenantID, command, parent.ID, parent.Input.Subject, input.ExpectedVersion, result.ResultVersion, input.Decision, p.Subject, authority.MembershipEpoch, authority.Kind, authority.GrantID, sourceVersion, hash, string(raw), now, now).Error; err != nil {
		return result, err
	}
	next := map[string]string{"approve": "active", "reject": "rejected", "revoke": "revoked"}[input.Decision]
	updated := tx.Table("iam_governance_grants").Where("tenant_id=? AND id=? AND version=?", p.TenantID, parent.ID, input.ExpectedVersion).Updates(map[string]any{"status": next, "version": result.ResultVersion, "updated_at": now})
	if updated.Error != nil {
		return result, updated.Error
	}
	if updated.RowsAffected != 1 {
		return result, errGovernanceConflict
	}
	for _, owner := range owners {
		id, err := uuid.NewV7()
		if err != nil {
			return result, err
		}
		if err := tx.Exec(`INSERT INTO iam_governance_owner_notices(tenant_id,id,grant_id,grant_version,recipient_subject,version,created_at,updated_at)VALUES(?,?,?,?,?,1,?,?)`, p.TenantID, id, parent.ID, result.ResultVersion, owner, now, now).Error; err != nil {
			return result, err
		}
	}
	if err := governanceAudit(tx, p, parent.ID, "iam.governance."+next); err != nil {
		return result, err
	}
	if err := h.projectGovernanceDecision(tx, p, parent.Input.Subject, next); err != nil {
		return result, err
	}
	return result, nil
}

func (h *governanceHandlers) projectGovernanceDecision(tx *gorm.DB, p authn.Principal, subject, status string) error {
	eventSubject, eventType := iamcontract.SubjectMemberGranted, "MemberGranted"
	if status == "active" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_members(tenant_id,id,subject,role_key)VALUES(?,?,?,?)ON CONFLICT(tenant_id,subject,role_key)DO NOTHING`, p.TenantID, id, subject, authz.RoleGovernanceDelegate).Error; err != nil {
			return err
		}
	} else {
		var other bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_governance_grants WHERE tenant_id=? AND subject=? AND status IN('active','provisional_active') AND expires_at>clock_timestamp())`, p.TenantID, subject).Scan(&other).Error; err != nil {
			return err
		}
		if other {
			return nil
		}
		removed := tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND subject=? AND role_key=?`, p.TenantID, subject, authz.RoleGovernanceDelegate)
		if removed.Error != nil {
			return removed.Error
		}
		if removed.RowsAffected == 0 {
			return nil
		}
		eventSubject, eventType = iamcontract.SubjectMemberRevoked, "MemberRevoked"
	}
	data, err := json.Marshal(iamcontract.MemberChangedData{Subject: subject, RoleKey: string(authz.RoleGovernanceDelegate)})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, eventSubject, events.Event{Type: eventType, Version: 1, TenantID: p.TenantID, Data: data})
}
