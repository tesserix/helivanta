package workforce

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/pkg/events"
)

func TestGovernanceAssignmentScopeRequestCannotCarryActorOrManagerAuthority(t *testing.T) {
	request := iamcontract.AssignmentGovernanceScopeRequestedData{ProposalID: uuid.Must(uuid.NewV7()), RequestID: uuid.Must(uuid.NewV7()), HospitalID: uuid.Must(uuid.NewV7())}
	raw, err := json.Marshal(request)
	require.NoError(t, err)
	valid := events.Event{TenantID: uuid.NewString(), Type: iamcontract.SubjectAssignmentGovernanceScopeRequested, Version: 1, Data: raw}
	decoded, err := assignmentGovernanceScopeRequest(valid)
	require.NoError(t, err)
	require.Equal(t, request, decoded)
	for name, mutate := range map[string]func(*events.Event){
		"legacy protocol": func(e *events.Event) { e.Type = iamcontract.SubjectAssignmentScopeRequested },
		"case alias":      func(e *events.Event) { e.Data = []byte(strings.Replace(string(raw), "proposal_id", "PROPOSAL_ID", 1)) },
		"duplicate request": func(e *events.Event) {
			e.Data = []byte(strings.TrimSuffix(string(raw), "}") + `,"request_id":"` + request.RequestID.String() + `"}`)
		},
		"version":     func(e *events.Event) { e.Version++ },
		"destination": func(e *events.Event) { e.DestinationTenantID = uuid.NewString() },
		"tenant":      func(e *events.Event) { e.TenantID = "unverified-client-tenant" },
		"actor": func(e *events.Event) {
			e.Data = []byte(strings.TrimSuffix(string(raw), "}") + `,"actor_subject":"synthetic-owner"}`)
		},
		"eligible":      func(e *events.Event) { e.Data = []byte(strings.TrimSuffix(string(raw), "}") + `,"eligible":true}`) },
		"trailing JSON": func(e *events.Event) { e.Data = append(append([]byte{}, raw...), []byte(`{}`)...) },
		"old proposal": func(e *events.Event) {
			changed := request
			changed.ProposalID = uuid.New()
			e.Data, _ = json.Marshal(changed)
		},
		"old request": func(e *events.Event) {
			changed := request
			changed.RequestID = uuid.New()
			e.Data, _ = json.Marshal(changed)
		},
		"nil hospital": func(e *events.Event) {
			changed := request
			changed.HospitalID = uuid.Nil
			e.Data, _ = json.Marshal(changed)
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := valid
			mutate(&changed)
			_, err := assignmentGovernanceScopeRequest(changed)
			require.Error(t, err)
		})
	}
}
