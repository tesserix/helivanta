package iam

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"github.com/tesserix/onboarding-service/api/staffclient"
)

func TestStaffMFAResetRejectsInvalidAuthorityBeforeSourceOrNativeIO(t *testing.T) {
	input := customercontract.StaffMFAResetInput{CommandID: "019f39c2-1738-7998-b5de-6d4214f614e0", ExpectedLinkID: "019f39c2-1738-7998-b5de-6d4214f614e1", ExpectedLinkVersion: 1, ExpectedStaffVersion: 1, Reason: "Lost authenticator confirmed"}
	now := time.Now().UTC()
	valid := authn.Principal{TenantID: "11111111-1111-4111-8111-111111111111", Subject: "admin-user", AuthTime: now.Add(-time.Minute), IdleDeadline: now.Add(time.Hour)}
	source := func(context.Context, customercontract.StaffMFAResetInput) (customercontract.StaffMFARecoveryTarget, error) {
		t.Fatal("invalid authority reached Workforce")
		return customercontract.StaffMFARecoveryTarget{}, nil
	}
	for _, tc := range []struct {
		name   string
		mutate func(*authn.Principal)
		want   error
	}{
		{"expired", func(p *authn.Principal) { p.IdleDeadline = now.Add(-time.Second) }, authz.ErrScopedCredentialRevoked},
		{"stale_authentication", func(p *authn.Principal) { p.AuthTime = now.Add(-6 * time.Minute) }, authz.ErrScopedCredentialRevoked},
		{"missing_authentication", func(p *authn.Principal) { p.AuthTime = time.Time{} }, authz.ErrScopedCredentialRevoked},
		{"future_authentication", func(p *authn.Principal) { p.AuthTime = now.Add(time.Minute) }, authz.ErrScopedCredentialRevoked},
		{"invalid_tenant", func(p *authn.Principal) { p.TenantID = "foreign" }, staffclient.ErrInvalid},
		{"invalid_actor", func(p *authn.Principal) { p.Subject = "" }, staffclient.ErrInvalid},
		{"dependencies_absent", func(*authn.Principal) {}, staffclient.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := valid
			tc.mutate(&p)
			service := New(nil).StaffMFARecovery(platform.Deps{})
			out, err := service.Reset(t.Context(), p, input, source)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out.State)
		})
	}
}
