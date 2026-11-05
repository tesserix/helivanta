package authz

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrPharmacyEmployerHidden answers a missing, foreign or hospital-owned Pharma organization alike.
var ErrPharmacyEmployerHidden = errors.New("pharmacy employer unavailable in this tenant")

// PharmacyEmployer is the Pharma-owned fact a standalone employer registration mirrors.
type PharmacyEmployer struct {
	PharmacyID    uuid.UUID
	Name          string
	OwnershipType string
}

// PharmacyEmployerGuard reads an employer-eligible Pharma organization inside the caller's guarded command.
type PharmacyEmployerGuard interface {
	CurrentPharmacyEmployer(ctx context.Context, tenant string, pharmacy uuid.UUID) (PharmacyEmployer, error)
}
