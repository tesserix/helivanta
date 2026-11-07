package authz

import (
	"context"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"time"
)

const (
	PartnerCatalogueRead Permission = "partner.catalogue.read"
	PartnerShipmentRead  Permission = "partner.shipment.read"
	PartnerInvoiceRead   Permission = "partner.invoice.read"
)

type PartnerScopeRequest struct {
	Principal           authn.Principal
	HospitalID, GrantID uuid.UUID
	Product             string
	Permission          Permission
}
type PartnerScopeGrant struct {
	SourceTenantID                         string
	RecipientTenantID                      string
	HospitalID, GrantID, RelationshipID    uuid.UUID
	Product                                string
	SourceOrganizationID, SourceLocationID uuid.UUID
	BuyerOrganizationID, BuyerLocationID   uuid.UUID
	Category                               string
	Permission                             Permission
	ProjectionHash                         string
	ValidUntil                             time.Time
}
type PartnerScopeAuthorizer interface {
	WithPartnerScope(context.Context, PartnerScopeRequest, func(context.Context, PartnerScopeGrant) error) error
}

// PartnerRecipientSource verifies hospital-local eligibility only. It does not authorize a supplier disclosure.
type PartnerRecipientSource interface {
	WithCurrentPartnerRecipient(context.Context, PartnerScopeRequest, func(context.Context) error) error
}
