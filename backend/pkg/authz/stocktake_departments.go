package authz

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/pagination"
)

// StocktakeDepartment is Organization's bounded current department projection.
type StocktakeDepartment struct {
	ID, RevisionID uuid.UUID
	Name, Code     string
	CreatedAt      time.Time
}

// StocktakeDepartmentReader requires held read/source grants on the requested root.
type StocktakeDepartmentReader interface {
	StocktakeDepartments(context.Context, StaffActorScope, pagination.Params) ([]StocktakeDepartment, error)
}
