package emergency_test

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/emergency" //nolint:depguard // module under test
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
	"testing"
)

func TestDispatchConsumerStartsAndSignalsOnlyItsOpaqueCommittedScope(t *testing.T) {
	in := emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: newID(), DispatchID: newID()}
	data, err := json.Marshal(emergencycontract.DispatchChangedData{BaseID: in.BaseID, DispatchID: in.DispatchID, Version: 1})
	require.NoError(t, err)
	evt := events.Event{ID: newID(), Type: emergencycontract.SubjectDispatchChanged, Version: 1, TenantID: in.TenantID, Data: data}
	c := &mocks.Client{}
	id, err := emergency.DispatchWorkflowID(in)
	require.NoError(t, err)
	c.On("ExecuteWorkflow", mock.Anything, mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
		return options.ID == id && options.TaskQueue == emergency.DispatchTaskQueue
	}), emergency.DispatchWorkflowName, in).Return(nil, nil).Once()
	c.On("SignalWorkflow", mock.Anything, id, "", emergency.DispatchVersionSignal, int64(1)).Return(nil).Once()
	consumer := emergency.DispatchConsumer(c)
	require.NoError(t, consumer.Handle(context.Background(), nil, evt))
	require.NoError(t, consumer.AfterCommit(context.Background(), evt))
	c.AssertExpectations(t)
}
func TestDispatchConsumerRejectsUnknownPayloadFieldsAndDirectedEnvelopes(t *testing.T) {
	in := emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: newID(), DispatchID: newID()}
	data, err := json.Marshal(emergencycontract.DispatchChangedData{BaseID: in.BaseID, DispatchID: in.DispatchID, Version: 1})
	require.NoError(t, err)
	consumer := emergency.DispatchConsumer(nil)
	for _, evt := range []events.Event{
		{ID: newID(), Type: emergencycontract.SubjectDispatchChanged, Version: 1, TenantID: in.TenantID, DestinationTenantID: testutil.TenantB, Data: data},
		{ID: newID(), Type: emergencycontract.SubjectDispatchChanged, Version: 1, TenantID: in.TenantID, Data: json.RawMessage(`{"base_id":"` + in.BaseID + `","dispatch_id":"` + in.DispatchID + `","version":1,"patient":"must-not-enter-history"}`)},
	} {
		require.Error(t, consumer.Handle(context.Background(), nil, evt))
	}
}
