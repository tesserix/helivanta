package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	organizationcontract "github.com/tesserix/helivanta/internal/modules/organization/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func validV7(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }

// proposalDepartmentScopeConsumer records Organization's answer; it never settles a proposal alone (ADR-0055 §5).
func proposalDepartmentScopeConsumer(deps platform.Deps) events.Consumer {
	subject := organizationcontract.SubjectAssignmentDepartmentVerified
	return events.Consumer{Name: "iam-assignment-department-scope", Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		tenant, err := uuid.Parse(evt.TenantID)
		if err != nil || tenant == uuid.Nil || tenant.String() != evt.TenantID || evt.Type != subject || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 2048 {
			return errors.New("invalid assignment department envelope")
		}
		if governanceBody(evt.Data, []string{"proposal_id", "request_id", "hospital_id", "department_id", "eligible"}) != nil {
			return errors.New("ambiguous assignment department result")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(evt.Data, &fields) != nil || len(fields) != 5 {
			return errors.New("incomplete assignment department result")
		}
		var result organizationcontract.AssignmentDepartmentVerifiedData
		d := json.NewDecoder(bytes.NewReader(evt.Data))
		d.DisallowUnknownFields()
		if d.Decode(&result) != nil {
			return errors.New("invalid assignment department result")
		}
		var extra any
		if !errors.Is(d.Decode(&extra), io.EOF) || !validV7(result.ProposalID) || !validV7(result.RequestID) || !validV7(result.DepartmentID) || result.HospitalID == uuid.Nil {
			return errors.New("invalid assignment department identity")
		}
		row, current, err := lockPendingProposal(tx, evt.TenantID, result.ProposalID, result.RequestID, result.HospitalID)
		if err != nil || !current || row.ScopeKind != "department" || row.DepartmentID == nil || *row.DepartmentID != result.DepartmentID {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.assignment_scope_verifier','organization',true)`).Error; err != nil {
			return err
		}
		return recordDepartmentAnswer(tx, deps, evt.TenantID, row, "organization", result.Eligible)
	}, AfterCommit: func(ctx context.Context, evt events.Event) error {
		return proposalDepartmentGovernanceAfterCommit(ctx, deps, evt)
	}}
}

// recordDepartmentAnswer keeps the first answer per owner and request; any refusal settles, else both must confirm.
func recordDepartmentAnswer(tx *gorm.DB, deps platform.Deps, tenant string, row assignmentProposal, owner string, eligible bool) error {
	if err := tx.Exec(`SELECT set_config('app.assignment_scope_result',?,true)`, row.RequestID.String()).Error; err != nil {
		return err
	}
	inserted := tx.Exec(`INSERT INTO iam_assignment_department_verifications(tenant_id,proposal_id,request_id,owner,eligible) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, tenant, row.ID, row.RequestID, owner, eligible)
	if inserted.Error != nil || inserted.RowsAffected == 0 {
		return inserted.Error
	}
	native, err := assignmentHasGovernanceRequest(tx, tenant, row)
	if err != nil || native {
		return err
	}
	if !eligible {
		return settleProposalScope(tx, deps, tenant, row, false)
	}
	var confirmed int64
	if err := tx.Table("iam_assignment_department_verifications").Where("tenant_id=? AND proposal_id=? AND request_id=? AND eligible", tenant, row.ID, row.RequestID).Count(&confirmed).Error; err != nil || confirmed < 2 {
		return err
	}
	return settleProposalScope(tx, deps, tenant, row, true)
}
