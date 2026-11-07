package supplieraccess

import (
	"errors"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"time"
)

var ErrInvalid = errors.New("invalid supplier grant")
var ErrConflict = errors.New("supplier grant state changed")
var ErrIneligible = errors.New("supplier grant is not currently eligible")

type Template struct {
	Permission    string
	GroupPurposes []string
}

func Prepare(id uuid.UUID, relationship suppliertrade.Relationship, input SupplierGrantInput, actor string, at time.Time) (SupplierGrant, error) {
	return SupplierGrant{}, ErrIneligible
}
func Decide(current SupplierGrant, expected int64, operation, actor, homeBasis string, at time.Time) (SupplierGrant, *SupplierGrantExchange, error) {
	return SupplierGrant{}, nil, ErrIneligible
}
func Apply(current *SupplierGrant, exchange SupplierGrantExchange, destination uuid.UUID, at time.Time) (SupplierGrant, *SupplierGrantExchange, error) {
	return SupplierGrant{}, nil, ErrIneligible
}
func Current(grant SupplierGrant, relationship suppliertrade.Relationship, at time.Time) bool {
	return false
}
