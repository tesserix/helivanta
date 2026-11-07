package supplieraccess_test

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"testing"
	"time"
)

func grantFixture(t *testing.T) (suppliertrade.Relationship, supplieraccess.SupplierGrantInput, time.Time) {
	t.Helper()
	at := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	source, home, hospital := uuid.New(), uuid.New(), uuid.New()
	binding, registration := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	version := int64(2)
	terms := suppliertrade.Terms{
		Seller:   suppliertrade.Endpoint{TenantID: source, Product: "supply", OrganizationID: uuid.Must(uuid.NewV7()), LocationID: uuid.Must(uuid.NewV7()), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.Must(uuid.NewV7()), LicenceVersion: 1, Jurisdiction: "IN-MH"},
		Buyer:    suppliertrade.Endpoint{TenantID: home, Product: "pharmacy", OrganizationID: uuid.Must(uuid.NewV7()), LocationID: uuid.Must(uuid.NewV7()), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.Must(uuid.NewV7()), LicenceVersion: 1, Jurisdiction: "IN-OD", HospitalID: &hospital, BindingID: &binding, BindingVersion: &version, RegistrationID: &registration, RegistrationVersion: &version},
		Category: "prescription", Currency: "INR", ValidFrom: at.Add(-time.Hour), ExpiresAt: at.Add(24 * time.Hour), TermsReference: "TEST reviewed trade terms"}
	digest, err := suppliertrade.TermsDigest(terms)
	require.NoError(t, err)
	seller, buyer := "TEST-commercial-supplier", "TEST-commercial-hospital"
	relationship := suppliertrade.Relationship{ID: uuid.Must(uuid.NewV7()), OwnerTenantID: source, Side: "seller", Terms: terms, TermsSha256: digest, Status: "active", Version: 3, AuthorityEpoch: 2, RemoteEpoch: 2, SignedBy: &seller, RemoteSignedBy: &buyer, CreatedAt: at.Add(-time.Hour), UpdatedAt: at}
	group := uuid.Must(uuid.NewV7())
	return relationship, supplieraccess.SupplierGrantInput{RelationshipID: relationship.ID, TermsSha256: digest, RecipientKind: "group", RecipientGroupID: &group, TemplateKey: "partner.catalogue_viewer.v1", ValidFrom: at, ExpiresAt: at.Add(time.Hour), Reason: "TEST only: hospital procurement catalogue access"}, at
}

func TestSupplierGrantRequiresIndependentSourceReviewHospitalAcceptanceAndExactAcknowledgement(t *testing.T) {
	relation, input, at := grantFixture(t)
	source, err := supplieraccess.Prepare(uuid.Must(uuid.NewV7()), relation, input, "TEST-maker", at)
	require.NoError(t, err)
	require.Equal(t, "pending_review", source.Status)
	require.False(t, supplieraccess.Current(source, relation, at))
	_, _, err = supplieraccess.Decide(source, source.Version, "approve", "TEST-maker", "", at)
	require.ErrorIs(t, err, supplieraccess.ErrConflict)
	source, offer, err := supplieraccess.Decide(source, source.Version, "approve", "TEST-source-reviewer", "", at)
	require.NoError(t, err)
	require.Equal(t, "offered", source.Status)
	require.NotNil(t, offer)
	require.False(t, supplieraccess.Current(source, relation, at))
	hospital, reply, err := supplieraccess.Apply(nil, *offer, relation.Terms.Buyer.TenantID, at)
	require.NoError(t, err)
	require.Nil(t, reply)
	require.Equal(t, "incoming", hospital.Status)
	homeBasis := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hospital, accepted, err := supplieraccess.Decide(hospital, hospital.Version, "approve", "TEST-home-reviewer", homeBasis, at)
	require.NoError(t, err)
	require.Equal(t, "accepted_pending_ack", hospital.Status)
	require.False(t, supplieraccess.Current(hospital, relation, at))
	source, ack, err := supplieraccess.Apply(&source, *accepted, relation.Terms.Seller.TenantID, at)
	require.NoError(t, err)
	require.NotNil(t, ack)
	require.Equal(t, offer.SenderEpoch, ack.SenderEpoch)
	hospital, reply, err = supplieraccess.Apply(&hospital, *ack, relation.Terms.Buyer.TenantID, at)
	require.NoError(t, err)
	require.Nil(t, reply)
	require.True(t, supplieraccess.Current(source, relation, at))
	require.True(t, supplieraccess.Current(hospital, relation, at))
	require.Equal(t, "approved", hospital.Status)
	require.Equal(t, source.AuthorityEpoch, hospital.RemoteEpoch)
	require.Equal(t, hospital.AuthorityEpoch, source.RemoteEpoch)
	require.Equal(t, &homeBasis, source.HomeBasisSha256)
	require.Equal(t, &homeBasis, hospital.HomeBasisSha256)
}
