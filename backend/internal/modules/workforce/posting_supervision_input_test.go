package workforce

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPostingSupervisionNullDoesNotBecomeLegacyOmission(t *testing.T) {
	legacy := postingProposalJSON(t)
	var omitted StaffPostingProposalInput
	require.NoError(t, json.Unmarshal(legacy, &omitted))
	require.True(t, validPostingProposalInput(&omitted))
	require.Nil(t, omitted.Supervision)
	for _, tc := range []struct{ name, field string }{
		{"null intent", `"supervision":null`},
		{"null root Staff", `"supervision":{"kind":"hospital_management","supervisor_staff_id":null}`},
		{"null root supervisor", `"supervision":{"kind":"hospital_management","supervisor_proposal_id":null}`},
		{"null root revision", `"supervision":{"kind":"hospital_management","supervisor_revision_id":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := append(bytes.Clone(legacy[:len(legacy)-1]), []byte(","+tc.field+"}")...)
			require.True(t, json.Valid(body), "the regression must use valid JSON")
			var in StaffPostingProposalInput
			require.Error(t, json.Unmarshal(body, &in), "explicit null must not become an omitted non-operating intent")
		})
	}
}

func TestPostingManagementNullDoesNotBecomeOmittedAttestation(t *testing.T) {
	legacy := postingDecisionJSON(t)
	var omitted StaffPostingDecisionInput
	require.NoError(t, json.Unmarshal(legacy, &omitted))
	require.True(t, validPostingDecisionInput(&omitted))
	require.Nil(t, omitted.HospitalManagementReviewed)
	body := append(bytes.Clone(legacy[:len(legacy)-1]), []byte(`,"hospital_management_reviewed":null}`)...)
	require.True(t, json.Valid(body))
	var in StaffPostingDecisionInput
	require.Error(t, json.Unmarshal(body, &in))
}

func TestPostingSupervisionTypesPreserveStrictHTTPDecoding(t *testing.T) {
	proposal, decision := postingProposalJSON(t), postingDecisionJSON(t)
	appendField := func(body []byte, field string) []byte {
		return append(bytes.Clone(body[:len(body)-1]), []byte(","+field+"}")...)
	}
	for _, tc := range []struct {
		name  string
		body  []byte
		into  any
		valid bool
	}{
		{"legacy proposal", proposal, &StaffPostingProposalInput{}, true},
		{"explicit root", appendField(proposal, `"supervision":{"kind":"hospital_management"}`), &StaffPostingProposalInput{}, true},
		{"legacy decision", decision, &StaffPostingDecisionInput{}, true},
		{"unknown proposal field", appendField(proposal, `"unexpected":true`), &StaffPostingProposalInput{}, false},
		{"unknown intent field", appendField(proposal, `"supervision":{"kind":"hospital_management","unexpected":true}`), &StaffPostingProposalInput{}, false},
		{"unknown decision field", appendField(decision, `"unexpected":true`), &StaffPostingDecisionInput{}, false},
		{"multiple proposal objects", append(bytes.Clone(proposal), []byte(` {}`)...), &StaffPostingProposalInput{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest("PUT", "/", bytes.NewReader(tc.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			require.Equal(t, tc.valid, decodeSized(ctx, tc.into, 8192), response.Body.String())
			if !tc.valid {
				require.Equal(t, 400, response.Code)
				require.NotContains(t, response.Body.String(), "unexpected", "raw decoder details are not exposed")
			}
		})
	}
}

func postingProposalJSON(t *testing.T) []byte {
	t.Helper()
	zero, one, examined := int64(0), int64(1), true
	id := uuid.Must(uuid.NewV7())
	body, err := json.Marshal(StaffPostingProposalInput{ExpectedVersion: &zero, DraftID: id, DraftRevisionID: id, DraftVersion: &one, GenerationID: id, StaffVersion: &one, EmploymentEpoch: &one, Reason: "Synthetic root intent", SourcesExamined: &examined})
	require.NoError(t, err)
	return body
}

func postingDecisionJSON(t *testing.T) []byte {
	t.Helper()
	one, examined := int64(1), true
	body, err := json.Marshal(StaffPostingDecisionInput{ExpectedVersion: &one, ExpectedRevisionID: uuid.Must(uuid.NewV7()), Decision: "approve", Reason: "Synthetic management decision", SourcesExamined: &examined})
	require.NoError(t, err)
	return body
}
