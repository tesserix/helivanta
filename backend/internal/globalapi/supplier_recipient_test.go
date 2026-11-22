package globalapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertSupplierRecipientCurrentMembership(t *testing.T, db *tenantdb.DB, authority authz.ScopedAuthorizer, staff authz.CurrentPartnerMemberGuard, tenant, subject string, memberData map[string]any, call func(*testing.T, string, string, string, any, int) map[string]any, reviewer string) {
	t.Helper()
	raw, err := json.Marshal(memberData)
	require.NoError(t, err)
	var member iam.PartnerGroupMemberProposal
	require.NoError(t, json.Unmarshal(raw, &member))
	groupCatalogue := seedSupplierRecipientFixture(t, db, tenant, subject, member, "group", "supply", "partner.catalogue_viewer.v1")
	groupShipments := seedSupplierRecipientFixture(t, db, tenant, subject, member, "group", "pharmacy", "partner.shipment_reader.v1")
	namedShipments := seedSupplierRecipientFixture(t, db, tenant, subject, member, "subject", "pharmacy", "partner.shipment_reader.v1")

	directoryPath := "/v1/iam/hospitals/" + member.HospitalID.String() + "/supplier-shipment-grants"
	directory := call(t, "GET", directoryPath+"?limit=1", subject, nil, 200)
	first := directory["data"].([]any)
	require.Len(t, first, 1)
	row := first[0].(map[string]any)
	require.Len(t, row, 6, "read-only discovery must withhold consent reasons, signers and private basis")
	for _, key := range []string{"id", "scope", "relationship_id", "terms_sha256", "recipient_kind", "created_at"} {
		require.Contains(t, row, key)
	}
	page := directory["page"].(map[string]any)
	require.Equal(t, true, page["has_more"])
	next := call(t, "GET", directoryPath+"?limit=1&cursor="+page["next_cursor"].(string), subject, nil, 200)
	second := next["data"].([]any)
	require.Len(t, second, 1)
	require.NotEqual(t, row["id"], second[0].(map[string]any)["id"])
	call(t, "GET", "/v1/iam/hospitals/"+member.HospitalID.String()+"/supplier-grant-offers", subject, nil, 403)
	call(t, "GET", directoryPath+"?unknown=1", subject, nil, 400)
	call(t, "GET", directoryPath+"?limit=1&cursor="+page["next_cursor"].(string), reviewer, nil, 400)
	call(t, "GET", strings.Replace(directoryPath, member.HospitalID.String(), uuid.NewString(), 1)+"?limit=1&cursor="+page["next_cursor"].(string), subject, nil, 400)
	authorizer := iam.NewPartnerScopeAuthorizer(platform.Deps{DB: db, ScopedAuthorizer: authority, PartnerMembers: staff})
	recipientSource := authorizer.(authz.PartnerRecipientSource)
	assertRecipient := func(request authz.PartnerScopeRequest, allowed bool) {
		t.Helper()
		called := false
		err := recipientSource.WithCurrentPartnerRecipient(t.Context(), request, func(ctx context.Context) error {
			called = true
			require.True(t, db.GuardedCommandActive(ctx))
			return nil
		})
		if allowed {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
		require.Equal(t, allowed, called, "recipient eligibility must hold through the actual guarded callback")
	}
	for _, request := range []authz.PartnerScopeRequest{groupCatalogue, groupShipments, namedShipments} {
		assertRecipient(request, true)
		called := false
		require.ErrorIs(t, authorizer.WithPartnerScope(t.Context(), request, func(context.Context, authz.PartnerScopeGrant) error { called = true; return nil }), authz.ErrScopedAuthorizationUnavailable)
		require.False(t, called, "hospital-local eligibility is not authority to release supplier data without its other owners")
		wrong := request
		wrong.HospitalID = uuid.New()
		assertRecipient(wrong, false)
	}
	for _, stale := range []string{"membership", "generation"} {
		changed := member
		if stale == "membership" {
			changed.MemberMembershipEpoch++
		} else {
			changed.MemberGenerationID = uuid.Must(uuid.NewV7())
		}
		request := seedSupplierRecipientFixture(t, db, tenant, subject, changed, "subject", "pharmacy", "partner.shipment_reader.v1")
		assertRecipient(request, false)
	}
	invoice := seedSupplierRecipientFixture(t, db, tenant, subject, member, "group", "supply", "partner.invoice_viewer.v1")
	assertRecipient(invoice, false)
	path := "/v1/iam/hospitals/" + member.HospitalID.String() + "/partner-groups/" + member.GroupID.String() + "/member-proposals/" + member.ID.String() + "/commands/" + uuid.Must(uuid.NewV7()).String()
	call(t, "PUT", path, reviewer, map[string]any{"expected_version": member.Version, "operation": "suspend", "reason": "TEST immediate supplier recipient reduction"}, 200)
	assertRecipient(groupCatalogue, false)
	assertRecipient(groupShipments, false)
	assertRecipient(namedShipments, true)
}

type supplierRecipientBasisFixture struct {
	GroupVersion    int64                                            `json:"group_version,omitempty"`
	Named           *supplieraccess.SupplierGrantRecipientBasisInput `json:"named,omitempty"`
	Identity        *authz.StaffActorReference                       `json:"identity,omitempty"`
	MembershipEpoch int64                                            `json:"membership_epoch,omitempty"`
	Actor           authz.StaffActorReference                        `json:"actor"`
}

func seedSupplierRecipientFixture(t *testing.T, db *tenantdb.DB, tenant, subject string, member iam.PartnerGroupMemberProposal, kind, product, template string, relationships ...suppliertrade.Relationship) authz.PartnerScopeRequest {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := uuid.Must(uuid.NewV7())
	home := uuid.MustParse(tenant)
	source := uuid.New()
	group := member.GroupID
	scope := supplieraccess.SupplierGrantScope{SourceTenantID: source, RecipientTenantID: home, HospitalID: member.HospitalID, SourceProduct: product, SourceOrganizationID: uuid.Must(uuid.NewV7()), SourceLocationID: uuid.Must(uuid.NewV7())}
	input := supplieraccess.SupplierGrantInput{RelationshipID: uuid.Must(uuid.NewV7()), TermsSha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RecipientKind: kind, TemplateKey: template, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), Reason: "TEST stored bilateral consent; recipient component only"}
	require.LessOrEqual(t, len(relationships), 1)
	if len(relationships) == 1 {
		relationship := relationships[0]
		require.Equal(t, home, relationship.Terms.Buyer.TenantID)
		require.NotNil(t, relationship.Terms.Buyer.HospitalID)
		require.Equal(t, member.HospitalID, *relationship.Terms.Buyer.HospitalID)
		source = relationship.Terms.Seller.TenantID
		scope.SourceTenantID, scope.SourceOrganizationID, scope.SourceLocationID = source, relationship.Terms.Seller.OrganizationID, relationship.Terms.Seller.LocationID
		scope.SourceProduct = relationship.Terms.Seller.Product
		require.Equal(t, product, scope.SourceProduct)
		input.RelationshipID, input.TermsSha256 = relationship.ID, relationship.TermsSha256
		input.ExpiresAt = now.Add(5 * time.Minute)
		if member.Input.ExpiresAt.Before(input.ExpiresAt) {
			input.ExpiresAt = member.Input.ExpiresAt
		}
		if relationship.Terms.ExpiresAt.Before(input.ExpiresAt) {
			input.ExpiresAt = relationship.Terms.ExpiresAt
		}
		input.Reason = "TEST retained bilateral consent for actual compound admission"
	}
	basisInput := supplierRecipientBasisFixture{}
	if kind == "group" {
		input.RecipientGroupID = &group
		basisInput.GroupVersion = member.Input.ExpectedGroupVersion
	} else {
		input.RecipientSubject = &subject
		basisInput.Named = &supplieraccess.SupplierGrantRecipientBasisInput{StaffID: member.Input.StaffID, PostingID: member.Input.PostingID, PostingRevisionID: member.Input.PostingRevisionID, PostingVersion: member.Input.PostingVersion, DepartmentID: member.Input.DepartmentID, DepartmentRevisionID: member.Input.DepartmentRevisionID}
		basisInput.Identity = &authz.StaffActorReference{StaffID: member.MemberStaffID, LinkID: member.MemberLinkID, LinkVersion: member.MemberLinkVersion, GenerationID: member.MemberGenerationID, EmploymentEpoch: member.MemberEmploymentEpoch}
		basisInput.MembershipEpoch = member.MemberMembershipEpoch
	}
	digest, err := supplieraccess.InputHash(scope, input)
	require.NoError(t, err)
	basis, err := json.Marshal(basisInput)
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
	if len(relationships) == 1 {
		sourceGrant := grant
		sourceGrant.OwnerTenantID, sourceGrant.Side, sourceGrant.PreparedBy = source, "source", "TEST-source-preparer"
		sourceGrant.SignedBy, sourceGrant.RemoteSignedBy = grant.RemoteSignedBy, grant.SignedBy
		require.NoError(t, supplieraccess.ValidateRecord(sourceGrant))
		require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "TEST-fixture"), source.String(), func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.iam_supplier_access','source',true),set_config('app.iam_supplier_hospital','',true)`).Error; err != nil {
				return err
			}
			document, err := json.Marshal(sourceGrant)
			if err != nil {
				return err
			}
			if err = tx.Exec(`INSERT INTO iam_supplier_grants(tenant_id,id,origin_tenant_id,origin_record_id,hospital_id,relationship_id,document,input_hash,version,created_at,updated_at)VALUES(?,?,?,?,?,?,?::jsonb,?,?,?,?)`, source, id, source, id, member.HospitalID, input.RelationshipID, string(document), digest, sourceGrant.Version, now, now).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO iam_supplier_grant_history(tenant_id,origin_tenant_id,origin_record_id,grant_id,hospital_id,version,document,actor_subject,created_at,updated_at)VALUES(?,?,?,?,?,?,?::jsonb,?,?,?)`, source, source, id, id, member.HospitalID, sourceGrant.Version, string(document), "TEST-fixture", now, now).Error
		}))
	}
	definition, ok := supplieraccess.TemplateFor(template)
	require.True(t, ok)
	return authz.PartnerScopeRequest{Principal: authn.Principal{TenantID: tenant, Subject: subject, AuthTime: time.Now(), IdleDeadline: time.Now().Add(time.Hour)}, HospitalID: member.HospitalID, GrantID: id, Product: product, Permission: authz.Permission(definition.Permission)}
}
