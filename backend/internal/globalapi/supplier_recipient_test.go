package globalapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertSupplierRecipientCurrentMembership(t *testing.T, db *tenantdb.DB, authority authz.ScopedAuthorizer, staff authz.CurrentPartnerMemberGuard, tenant, subject string, memberData map[string]any, call func(*testing.T, string, string, string, any, int) map[string]any, reviewer string) {
	t.Helper()
	raw, err := json.Marshal(memberData)
	require.NoError(t, err)
	var member iam.PartnerGroupMemberProposal
	require.NoError(t, json.Unmarshal(raw, &member))
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := uuid.Must(uuid.NewV7())
	home := uuid.MustParse(tenant)
	source := uuid.New()
	group := member.GroupID
	scope := supplieraccess.SupplierGrantScope{SourceTenantID: source, RecipientTenantID: home, HospitalID: member.HospitalID, SourceProduct: "supply", SourceOrganizationID: uuid.Must(uuid.NewV7()), SourceLocationID: uuid.Must(uuid.NewV7())}
	input := supplieraccess.SupplierGrantInput{RelationshipID: uuid.Must(uuid.NewV7()), TermsSha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RecipientKind: "group", RecipientGroupID: &group, TemplateKey: "partner.catalogue_viewer.v1", ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), Reason: "TEST stored bilateral consent; recipient component only"}
	digest, err := supplieraccess.InputHash(scope, input)
	require.NoError(t, err)
	basis, err := json.Marshal(struct {
		GroupVersion int64                     `json:"group_version,omitempty"`
		Actor        authz.StaffActorReference `json:"actor"`
	}{GroupVersion: member.Input.ExpectedGroupVersion})
	require.NoError(t, err)
	sum := sha256.Sum256(basis)
	basisHash := hex.EncodeToString(sum[:])
	local, remote := "TEST-hospital-reviewer", "TEST-source-reviewer"
	grant := supplieraccess.SupplierGrant{ID: id, OwnerTenantID: home, Scope: scope, Input: input, InputSha256: digest, Side: "hospital", Status: "approved", Version: 3, AuthorityEpoch: 2, RemoteEpoch: 2, PreparedBy: "system:iam-supplier-grants", SignedBy: &local, RemoteSignedBy: &remote, HomeBasisSha256: &basisHash, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "TEST-fixture"), tenant, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT set_config('app.iam_supplier_access','hospital',true),set_config('app.iam_supplier_hospital',?,true)`, member.HospitalID.String()).Error; e != nil {
			return e
		}
		document, e := json.Marshal(grant)
		if e != nil {
			return e
		}
		if e = tx.Exec(`INSERT INTO iam_supplier_grants(tenant_id,id,origin_tenant_id,origin_record_id,hospital_id,relationship_id,document,input_hash,version,created_at,updated_at)VALUES(?,?,?,?,?,?,?::jsonb,?,?,?,?)`, home, id, source, id, member.HospitalID, input.RelationshipID, string(document), digest, grant.Version, now, now).Error; e != nil {
			return e
		}
		return tx.Exec(`INSERT INTO iam_supplier_grant_bases(tenant_id,grant_id,hospital_id,document,basis_hash,actor_subject,created_at,updated_at)VALUES(?,?,?,?::jsonb,?,?,?,?)`, home, id, member.HospitalID, string(basis), basisHash, "TEST-fixture", now, now).Error
	}))
	request := authz.PartnerScopeRequest{Principal: authn.Principal{TenantID: tenant, Subject: subject, AuthTime: time.Now(), IdleDeadline: time.Now().Add(time.Hour)}, HospitalID: member.HospitalID, GrantID: id, Product: "supply", Permission: authz.PartnerCatalogueRead}
	authorizer := iam.NewPartnerScopeAuthorizer(platform.Deps{DB: db, ScopedAuthorizer: authority, PartnerMembers: staff})
	recipientSource := authorizer.(authz.PartnerRecipientSource)
	called := false
	require.NoError(t, recipientSource.WithCurrentPartnerRecipient(t.Context(), request, func(ctx context.Context) error {
		called = true
		require.True(t, db.GuardedCommandActive(ctx))
		return nil
	}))
	require.True(t, called)
	called = false
	require.ErrorIs(t, authorizer.WithPartnerScope(t.Context(), request, func(context.Context, authz.PartnerScopeGrant) error { called = true; return nil }), authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called, "hospital-local eligibility is not authority to release supplier data without its other owners")
	wrong := request
	wrong.HospitalID = uuid.New()
	called = false
	require.Error(t, recipientSource.WithCurrentPartnerRecipient(t.Context(), wrong, func(context.Context) error { called = true; return nil }))
	require.False(t, called)
	path := "/v1/iam/hospitals/" + member.HospitalID.String() + "/partner-groups/" + group.String() + "/member-proposals/" + member.ID.String() + "/commands/" + uuid.Must(uuid.NewV7()).String()
	call(t, "PUT", path, reviewer, map[string]any{"expected_version": member.Version, "operation": "suspend", "reason": "TEST immediate supplier recipient reduction"}, 200)
	called = false
	require.Error(t, recipientSource.WithCurrentPartnerRecipient(t.Context(), request, func(context.Context) error { called = true; return nil }))
	require.False(t, called, "a retained approved supplier grant cannot survive current hospital membership suspension")
}
