package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	organizationcontract "github.com/tesserix/helivanta/internal/modules/organization/contract"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

var errAssignmentSourceReplaced = errors.New("original assignment source replaced")

type assignmentGovernanceRequest struct {
	ProposalID, RequestID, HospitalID uuid.UUID
	Authority                         json.RawMessage
}

func assignmentHasGovernanceRequest(tx *gorm.DB, tenant string, row assignmentProposal) (bool, error) {
	var installed, native bool
	if err := tx.Raw(`SELECT to_regclass('public.iam_assignment_governance_requests') IS NOT NULL`).Scan(&installed).Error; err != nil || !installed {
		return false, err
	}
	if err := assignmentGovernanceSupport(tx); err != nil {
		return false, err
	}
	err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_assignment_governance_requests WHERE tenant_id=? AND proposal_id=? AND request_id=?)`, tenant, row.ID, row.RequestID).Scan(&native).Error
	return native, err
}

func assignmentGovernanceResult(evt events.Event) (workforcecontract.AssignmentGovernanceScopeVerifiedData, error) {
	var result workforcecontract.AssignmentGovernanceScopeVerifiedData
	tenant, err := uuid.Parse(evt.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != evt.TenantID || evt.Type != workforcecontract.SubjectAssignmentGovernanceScopeVerified || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 2048 {
		return result, errors.New("invalid native assignment scope envelope")
	}
	if governanceBody(evt.Data, []string{"proposal_id", "request_id", "hospital_id", "eligible"}) != nil {
		return result, errors.New("ambiguous native assignment scope result")
	}
	decoder := json.NewDecoder(bytes.NewReader(evt.Data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil {
		return result, errors.New("invalid native assignment scope result")
	}
	var extra any
	var fields map[string]json.RawMessage
	if !errors.Is(decoder.Decode(&extra), io.EOF) || json.Unmarshal(evt.Data, &fields) != nil || len(fields) != 4 || !bytes.Equal(bytes.TrimSpace(fields["eligible"]), []byte("true")) && !bytes.Equal(bytes.TrimSpace(fields["eligible"]), []byte("false")) || !validV7(result.ProposalID) || !validV7(result.RequestID) || result.HospitalID == uuid.Nil || result.HospitalID.Variant() != uuid.RFC4122 {
		return result, errors.New("invalid native assignment scope identity")
	}
	return result, nil
}

func proposalGovernanceScopeConsumer(deps platform.Deps) events.Consumer {
	return events.Consumer{Name: "iam-assignment-governance-scope", Subject: workforcecontract.SubjectAssignmentGovernanceScopeVerified, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		result, err := assignmentGovernanceResult(evt)
		if err != nil {
			return err
		}
		if err := assignmentGovernanceSupport(tx); err != nil {
			return err
		}
		row, current, err := lockPendingProposal(tx, evt.TenantID, result.ProposalID, result.RequestID, result.HospitalID)
		if err != nil || !current {
			return err
		}
		native, err := assignmentHasGovernanceRequest(tx, evt.TenantID, row)
		if err != nil || !native {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.iam_assignment_native_verifier',?,true),set_config('app.assignment_scope_verifier','workforce',true)`, result.RequestID.String()).Error; err != nil {
			return err
		}
		inserted := tx.Exec(`INSERT INTO iam_assignment_governance_verifications(tenant_id,request_id,proposal_id,hospital_id,eligible) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, evt.TenantID, result.RequestID, result.ProposalID, result.HospitalID, result.Eligible)
		if inserted.Error != nil || inserted.RowsAffected == 0 || row.ScopeKind != "department" {
			return inserted.Error
		}
		return recordDepartmentAnswer(tx, deps, evt.TenantID, row, "workforce", result.Eligible)
	}, AfterCommit: func(ctx context.Context, evt events.Event) error {
		result, err := assignmentGovernanceResult(evt)
		if err != nil {
			return err
		}
		return settleAssignmentGovernance(ctx, deps, evt.TenantID, result.ProposalID, result.RequestID, result.HospitalID)
	}}
}

func proposalDepartmentGovernanceAfterCommit(ctx context.Context, deps platform.Deps, evt events.Event) error {
	var result organizationcontract.AssignmentDepartmentVerifiedData
	if json.Unmarshal(evt.Data, &result) != nil {
		return errors.New("invalid retained department scope result")
	}
	return settleAssignmentGovernance(ctx, deps, evt.TenantID, result.ProposalID, result.RequestID, result.HospitalID)
}

func assignmentGovernanceAnswers(tx *gorm.DB, tenant string, row assignmentProposal) (bool, bool, error) {
	var owners []struct{ Eligible bool }
	if row.ScopeKind == "department" {
		if err := tx.Table("iam_assignment_department_verifications").Select("eligible").Where("tenant_id=? AND request_id=? AND proposal_id=?", tenant, row.RequestID, row.ID).Limit(2).Find(&owners).Error; err != nil {
			return false, false, err
		}
		for _, owner := range owners {
			if !owner.Eligible {
				return true, false, nil
			}
		}
	}
	var answers []struct{ Eligible bool }
	if err := tx.Table("iam_assignment_governance_verifications").Select("eligible").Where("tenant_id=? AND request_id=? AND proposal_id=? AND hospital_id=?", tenant, row.RequestID, row.ID, row.HospitalID).Limit(1).Find(&answers).Error; err != nil || len(answers) == 0 {
		return false, false, err
	}
	if !answers[0].Eligible {
		return true, false, nil
	}
	if row.ScopeKind != "department" {
		return true, true, nil
	}
	return len(owners) == 2, len(owners) == 2, nil
}

func settleAssignmentGovernance(ctx context.Context, deps platform.Deps, tenant string, proposal, request, hospital uuid.UUID) error {
	if deps.DB == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	var proof assignmentGovernanceProof
	var original assignmentGovernanceRequest
	current := false
	err := deps.DB.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		if err := departmentAwareAccess(tx); err != nil {
			return err
		}
		// Old event deliveries and pre-migration legacy fixtures have no native request.
		row := assignmentProposal{ID: proposal, RequestID: request}
		native, err := assignmentHasGovernanceRequest(tx, tenant, row)
		if err != nil || !native {
			return err
		}
		if err := assignmentGovernanceSupport(tx); err != nil {
			return err
		}
		if err := tx.Table("iam_assignment_governance_requests").Select("proposal_id,request_id,hospital_id,authority").Where("tenant_id=? AND proposal_id=? AND request_id=? AND hospital_id=?", tenant, proposal, request, hospital).Take(&original).Error; err != nil {
			return err
		}
		var pending assignmentProposal
		if err := tx.Where("tenant_id=? AND id=? AND request_id=? AND hospital_id=? AND status IN('pending_scope','pending_decision_scope')", tenant, proposal, request, hospital).Take(&pending).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		ready, _, err := assignmentGovernanceAnswers(tx, tenant, pending)
		current = ready
		return err
	})
	if err != nil || !current {
		return err
	}
	if json.Unmarshal(original.Authority, &proof) != nil || proof.TenantID != tenant || proof.HospitalID != hospital || !proposalSubject(proof.ActorSubject) || proof.AuthenticatedAt.IsZero() || proof.IdleDeadline.IsZero() || proof.Capability != authz.GovernanceAssignmentManage && proof.Capability != authz.GovernanceAssignmentReview {
		return authz.ErrScopedAuthorizationUnavailable
	}
	finish := func(commandCtx context.Context, fresh *assignmentGovernanceProof, denied bool) error {
		transaction := deps.DB.WithTenantCommand
		if denied {
			transaction = deps.DB.WithTenant
		}
		return transaction(commandCtx, tenant, func(tx *gorm.DB) error {
			if err := departmentAwareAccess(tx); err != nil {
				return err
			}
			if err := assignmentGovernanceContext(tx, hospital, proposal.String(), fresh); err != nil {
				return err
			}
			row, pending, err := lockPendingProposal(tx, tenant, proposal, request, hospital)
			if err != nil || !pending {
				return err
			}
			ready, eligible, err := assignmentGovernanceAnswers(tx, tenant, row)
			if err != nil || !ready {
				return err
			}
			mode := "authorized"
			if denied {
				mode, eligible = "denied", false
			}
			if err := tx.Exec(`SELECT set_config('app.iam_assignment_native_settlement',?,true)`, mode).Error; err != nil {
				return err
			}
			return settleProposalScope(tx, deps, tenant, row, eligible)
		})
	}
	if deps.GovernanceSources == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	p := authn.Principal{TenantID: tenant, Subject: proof.ActorSubject, AuthTime: proof.AuthenticatedAt, IdleDeadline: proof.IdleDeadline}
	req := authz.GovernanceSourceRequest{Principal: p, Capability: proof.Capability, Scope: authz.GovernanceEstablishmentScope{TenantID: tenant, Kind: "hospital", ID: &hospital}}
	err = deps.GovernanceSources.WithGovernanceSource(ctx, req, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
		fresh, err := assignmentProof(p, hospital, proof.Capability, source)
		if err != nil {
			return err
		}
		if !sameAssignmentSource(proof, fresh) {
			return errAssignmentSourceReplaced
		}
		return finish(commandCtx, &fresh, false)
	})
	if errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, authz.ErrScopedCredentialRevoked) || errors.Is(err, authz.ErrGovernanceScopeHidden) || errors.Is(err, errAssignmentSourceReplaced) {
		denialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return finish(denialCtx, nil, true)
	}
	return err
}
