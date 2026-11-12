package iam

import (
	"context"
	"errors"
)

var errGrantIdentityIneligible = errors.New("target identity is not eligible for a new grant")

func requireNewGrantIdentity(ctx context.Context, reader MembershipIdentityReader, tenant, subject string) error {
	if reader == nil {
		return errMembershipAuthority
	}
	eligible, err := reader.MembershipEligible(ctx, tenant, subject)
	if err != nil {
		return errors.Join(errMembershipAuthority, err)
	}
	if !eligible {
		return errGrantIdentityIneligible
	}
	return nil
}
