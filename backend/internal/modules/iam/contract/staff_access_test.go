package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
)

func TestScopeRequestPublishesOnlyOwnerVerificationInputs(t *testing.T) {
	proposal := uuid.MustParse("019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa")
	request := uuid.MustParse("019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb")
	hospital := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	encoded, err := json.Marshal(iamcontract.AssignmentScopeRequestedData{ProposalID: proposal, RequestID: request, HospitalID: hospital, ActorSubject: "synthetic-reviewer"})
	require.NoError(t, err)
	require.JSONEq(t, `{"proposal_id":"019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa","request_id":"019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb","hospital_id":"22222222-2222-4222-8222-222222222222","actor_subject":"synthetic-reviewer"}`, string(encoded))
}

func TestProjectionRequestAndNegativeScopeResultKeepMinimalWireShapes(t *testing.T) {
	id := uuid.MustParse("019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa")
	encoded, err := json.Marshal(iamcontract.AssignmentProjectionRequestedData{ProposalID: id})
	require.NoError(t, err)
	require.JSONEq(t, `{"proposal_id":"019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa"}`, string(encoded))
	encoded, err = json.Marshal(workforcecontract.AssignmentScopeVerifiedData{ProposalID: id, RequestID: id, HospitalID: id, Eligible: false})
	require.NoError(t, err)
	require.JSONEq(t, `{"proposal_id":"019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa","request_id":"019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa","hospital_id":"019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa","eligible":false}`, string(encoded))
}
