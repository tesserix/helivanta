package authz

import (
	"context"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
)

type PartnerAssignment struct {
	Grant           supplieraccess.SupplierGrant `json:"grant"`
	Subject         string                       `json:"subject"`
	MembershipEpoch int64                        `json:"membership_epoch"`
	RecipientHash   string                       `json:"recipient_hash"`
}

func (c *Client) ProjectPartnerAssignment(context.Context, PartnerAssignment) error {
	return ErrScopedAuthorizationUnavailable
}
func (c *Client) CheckPartnerAssignment(context.Context, PartnerAssignment, string, Permission) (bool, error) {
	return false, ErrScopedAuthorizationUnavailable
}
