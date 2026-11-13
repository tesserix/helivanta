package authz

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
)

type GovernanceCapability string

const GovernanceAccountLinkReview GovernanceCapability = "workforce.account_link.review"

type GovernanceSourceKind string

const (
	GovernanceOwnerSource GovernanceSourceKind = "owner"
	GovernanceGrantSource GovernanceSourceKind = "grant"
)

// GovernanceSourceRequest is built by a product from its authenticated principal and canonical employer.
type GovernanceSourceRequest struct {
	Principal  authn.Principal
	Capability GovernanceCapability
	Scope      GovernanceEstablishmentScope
}

// GovernanceSourceEvidence is attributable evidence, with authority retained only during the callback.
type GovernanceSourceEvidence struct {
	Kind                     GovernanceSourceKind
	OwnerMembershipID        uuid.UUID
	OwnerCount               int64
	GrantID                  uuid.UUID
	Version, MembershipEpoch int64
	TemplateKey              string
	Scope                    GovernanceEstablishmentScope
	ExpiresAt                time.Time
}

type CurrentGovernanceSourceGuard interface {
	WithGovernanceSource(context.Context, GovernanceSourceRequest, func(context.Context, GovernanceSourceEvidence) error) error
}
