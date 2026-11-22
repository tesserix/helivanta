package globalapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

func (f supplierCompoundFixture) assertPublicSupplierGrant(t *testing.T, admin operatingDutyFixture, relationship suppliertrade.Relationship, member iam.PartnerGroupMemberProposal, kind, subject, reviewer string) uuid.UUID {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	expires := now.Add(5 * time.Minute)
	if member.Input.ExpiresAt.Before(expires) {
		expires = member.Input.ExpiresAt
	}
	if relationship.Terms.ExpiresAt.Before(expires) {
		expires = relationship.Terms.ExpiresAt
	}
	id := uuid.Must(uuid.NewV7())
	path := "/v1/iam/supplier-grants/" + id.String()
	input := supplieraccess.SupplierGrantInput{RelationshipID: relationship.ID, TermsSha256: relationship.TermsSha256, RecipientKind: kind, TemplateKey: "partner.shipment_reader.v1", ValidFrom: now, ExpiresAt: expires, Reason: "TEST public source-approved least-privilege hospital shipment disclosure"}
	if kind == "group" {
		input.RecipientGroupID = &member.GroupID
	} else {
		input.RecipientSubject = &subject
	}
	prepared := admin.call("PUT", path, "owner", input, 201)
	require.JSONEq(t, prepared, admin.call("PUT", path, "owner", input, 200))
	var proposal supplieraccess.SupplierGrant
	require.NoError(t, json.Unmarshal([]byte(prepared), &proposal))
	require.Equal(t, "pending_review", proposal.Status)
	command := path + "/commands/" + uuid.Must(uuid.NewV7()).String()
	decision := supplieraccess.SupplierGrantDecisionInput{ExpectedVersion: proposal.Version, Operation: "approve", Reason: "TEST independent source disclosure approval"}
	admin.call("PUT", command, "owner", decision, 409)
	offered := admin.call("PUT", command, "reviewer", decision, 200)
	require.JSONEq(t, offered, admin.call("PUT", command, "reviewer", decision, 200))
	home := "/v1/iam/hospitals/" + member.HospitalID.String() + "/supplier-grant-offers/" + id.String()
	var incoming supplieraccess.SupplierGrant
	require.Eventually(t, func() bool {
		page := f.hospitalCall(t, "GET", "/v1/iam/hospitals/"+member.HospitalID.String()+"/supplier-grant-offers?limit=50", reviewer, nil, 200)
		for _, raw := range page["data"].([]any) {
			if raw.(map[string]any)["id"] == id.String() {
				body, err := json.Marshal(raw)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(body, &incoming))
				return incoming.Status == "incoming"
			}
		}
		return false
	}, 20*time.Second, 100*time.Millisecond)
	require.Equal(t, proposal.InputSha256, incoming.InputSha256)
	f.hospitalCall(t, "GET", home, subject, nil, 403)
	accept := supplieraccess.SupplierGrantHospitalDecisionInput{ExpectedVersion: incoming.Version, Operation: "approve", Reason: "TEST hospital independently verifies the exact current recipient basis"}
	if kind == "group" {
		accept.ExpectedGroupVersion = &member.Input.ExpectedGroupVersion
	} else {
		accept.RecipientBasis = &supplieraccess.SupplierGrantRecipientBasisInput{StaffID: member.Input.StaffID, PostingID: member.Input.PostingID, PostingRevisionID: member.Input.PostingRevisionID, PostingVersion: member.Input.PostingVersion, DepartmentID: member.Input.DepartmentID, DepartmentRevisionID: member.Input.DepartmentRevisionID}
	}
	homeCommand := home + "/commands/" + uuid.Must(uuid.NewV7()).String()
	accepted := f.hospitalCall(t, "PUT", homeCommand, reviewer, accept, 200)
	require.Equal(t, accepted, f.hospitalCall(t, "PUT", homeCommand, reviewer, accept, 200))
	require.Equal(t, accepted, f.hospitalCall(t, "GET", homeCommand, reviewer, nil, 200))
	changed := accept
	changed.Reason = "TEST changed hospital disclosure decision"
	f.hospitalCall(t, "PUT", homeCommand, reviewer, changed, 409)
	require.Eventually(t, func() bool {
		var source supplieraccess.SupplierGrant
		require.NoError(t, json.Unmarshal([]byte(admin.call("GET", path, "owner", nil, 200)), &source))
		if source.Status != "approved" {
			return false
		}
		current := f.hospitalCall(t, "GET", home, reviewer, nil, 200)
		return current["status"] == "approved"
	}, 20*time.Second, 100*time.Millisecond)
	return id
}
