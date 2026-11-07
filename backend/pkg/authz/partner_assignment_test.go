package authz

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
)

func syntheticPartnerAssignment(t *testing.T) PartnerAssignment {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	member := "TEST-home-user"
	scope := supplieraccess.SupplierGrantScope{SourceTenantID: uuid.New(), RecipientTenantID: uuid.New(), HospitalID: uuid.New(), SourceProduct: "pharmacy", SourceOrganizationID: uuid.Must(uuid.NewV7()), SourceLocationID: uuid.Must(uuid.NewV7())}
	input := supplieraccess.SupplierGrantInput{RelationshipID: uuid.Must(uuid.NewV7()), TermsSha256: strings.Repeat("a", 64), RecipientKind: "subject", RecipientSubject: &member, TemplateKey: "partner.invoice_viewer.v1", ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Reason: "TEST only: specifically approved invoice view"}
	hash, err := supplieraccess.InputHash(scope, input)
	require.NoError(t, err)
	supplier, home, basis := "TEST-source-reviewer", "TEST-home-reviewer", strings.Repeat("b", 64)
	grant := supplieraccess.SupplierGrant{ID: uuid.Must(uuid.NewV7()), OwnerTenantID: scope.SourceTenantID, Scope: scope, Input: input, InputSha256: hash, Side: "source", Status: "approved", Version: 3, AuthorityEpoch: 2, RemoteEpoch: 2, PreparedBy: "TEST-preparer", SignedBy: &supplier, RemoteSignedBy: &home, HomeBasisSha256: &basis, CreatedAt: now, UpdatedAt: now}
	return PartnerAssignment{Grant: grant, Subject: member, MembershipEpoch: 1, RecipientHash: strings.Repeat("c", 64)}
}
func TestPartnerAssignmentNeverCreatesSupplierMembershipOrTenantWidePermissions(t *testing.T) {
	client, err := NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	assignment := syntheticPartnerAssignment(t)
	home, source := assignment.Grant.Scope.RecipientTenantID.String(), assignment.Grant.Scope.SourceTenantID.String()
	require.NoError(t, client.ProjectPartnerAssignment(t.Context(), assignment))
	allowed, err := client.CheckPartnerAssignment(t.Context(), assignment, assignment.Subject, PartnerInvoiceRead)
	require.NoError(t, err)
	require.False(t, allowed)
	require.NoError(t, client.GrantTenantRole(t.Context(), home, RoleStaffMember))
	require.NoError(t, client.GrantRole(t.Context(), home, assignment.Subject, RoleStaffMember))
	allowed, err = client.CheckPartnerAssignment(t.Context(), assignment, assignment.Subject, PartnerInvoiceRead)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = client.CheckPartnerAssignment(t.Context(), assignment, "TEST-other-user", PartnerInvoiceRead)
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = client.CheckPartnerAssignment(t.Context(), assignment, assignment.Subject, PartnerCatalogueRead)
	require.NoError(t, err)
	require.False(t, allowed)
	homePermissions, err := client.Resolve(t.Context(), assignment.Subject, home)
	require.NoError(t, err)
	require.False(t, homePermissions.Has(PartnerInvoiceRead))
	sourcePermissions, err := client.Resolve(t.Context(), assignment.Subject, source)
	require.NoError(t, err)
	require.False(t, sourcePermissions.Has(PartnerInvoiceRead))
	require.Len(t, sourcePermissions, 0)
	for name, change := range map[string]func(*PartnerAssignment){"recipient eligibility": func(v *PartnerAssignment) { v.RecipientHash = strings.Repeat("d", 64) }, "membership": func(v *PartnerAssignment) { v.MembershipEpoch++ }, "grant revision": func(v *PartnerAssignment) { v.Grant.Version++ }, "source epoch": func(v *PartnerAssignment) { v.Grant.AuthorityEpoch++ }, "hospital epoch": func(v *PartnerAssignment) { v.Grant.RemoteEpoch++ }} {
		t.Run(name, func(t *testing.T) {
			different := assignment
			change(&different)
			allowed, err := client.CheckPartnerAssignment(t.Context(), different, assignment.Subject, PartnerInvoiceRead)
			require.NoError(t, err)
			require.False(t, allowed)
		})
	}
	require.NoError(t, client.RevokeRole(t.Context(), home, assignment.Subject, RoleStaffMember))
	allowed, err = client.CheckPartnerAssignment(t.Context(), assignment, assignment.Subject, PartnerInvoiceRead)
	require.NoError(t, err)
	require.False(t, allowed)
}
