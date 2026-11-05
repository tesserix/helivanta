package workforce

import (
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
)

func currentPostingUseDeadline(row StaffPostingProposal, source EmploymentPeriodSource, generation employmentGenerationRecord, state string, generationUntil, checked time.Time) (time.Time, error) {
	if checked.IsZero() || checked.Year() < 1 || checked.Year() > 9999 || generationUntil.IsZero() || row.EligibilityUntil.IsZero() || generation.ID == uuid.Nil {
		return time.Time{}, authz.ErrScopedAuthorizationUnavailable
	}
	if row.Status != "approved" || state != "effective" || row.GenerationID != generation.ID ||
		source.StaffVersion != row.StaffVersion || source.EmploymentEpoch != row.EmploymentEpoch || generation.EmploymentEpoch != row.EmploymentEpoch ||
		row.StartsAt.After(checked) || generation.StartsAt.After(checked) || row.StartsAt.Before(generation.StartsAt) {
		return time.Time{}, authz.ErrScopedCommandDenied
	}
	until := postingApprovalDeadline(row)
	if generationUntil.Before(until) {
		until = generationUntil
	}
	if generation.EndsAt != nil && generation.EndsAt.Before(until) {
		until = *generation.EndsAt
	}
	if !checked.Before(until) {
		return time.Time{}, authz.ErrScopedCommandDenied
	}
	return until.UTC(), nil
}
