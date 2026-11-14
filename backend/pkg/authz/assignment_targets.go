package authz

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/pagination"
)

// ErrAssignmentTargetChanged requires a fresh choice after canonical source drift.
var ErrAssignmentTargetChanged = errors.New("assignment target changed")

// AssignmentTargetReference contains current owner facts, never a portable authority.
type AssignmentTargetReference struct {
	StaffID, LinkID, GenerationID              uuid.UUID
	StaffVersion, LinkVersion, EmploymentEpoch int64
	Subject                                    string
	EligibleUntil                              time.Time
}

// AssignmentTargetCandidate retains a scan boundary even when qualification fails.
type AssignmentTargetCandidate struct {
	StaffID   uuid.UUID
	CreatedAt time.Time
	Reference *AssignmentTargetReference
}

// GovernedAssignmentTargetReader reads canonical Staff only under a held IAM source.
type GovernedAssignmentTargetReader interface {
	ReadAssignmentTargetCandidates(context.Context, GovernedAssignmentSelectionRequest, pagination.Params) ([]AssignmentTargetCandidate, error)
	RecheckAssignmentTargets(context.Context, GovernedAssignmentSelectionRequest, []AssignmentTargetReference) error
}
