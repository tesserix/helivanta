package authz

import (
	"context"
	"errors"
	"github.com/google/uuid"
)

var ErrSupplierEmployerHidden = errors.New("supplier employer unavailable in this tenant")

// SupplierEmployer is an owned SupplyConnect structural fact, without operating authority.
type SupplierEmployer struct {
	SupplierID    uuid.UUID
	Name          string
	SourceVersion int64
}
type SupplierEmployerGuard interface {
	CurrentSupplierEmployer(context.Context, string, uuid.UUID) (SupplierEmployer, error)
}
