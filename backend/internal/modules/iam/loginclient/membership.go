package loginclient

import "context"

// MembershipEligible admits only a verified active human owned by this tenant.
func (c *Client) MembershipEligible(ctx context.Context, tenant, subject string) (bool, error) {
	snapshot, err := c.identitySnapshot(ctx, tenant, subject, IdentityReadMembership)
	if err != nil {
		return false, err
	}
	return snapshot.MembershipEligible, nil
}
