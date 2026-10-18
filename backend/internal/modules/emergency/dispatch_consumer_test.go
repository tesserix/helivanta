package emergency_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/emergency" //nolint:depguard // module under test
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	commonpb "go.temporal.io/api/common/v1"
	enums "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/serviceerror"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
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

func TestDispatchConsumerVerifiesDuplicateExecutionBeforeSignalling(t *testing.T) {
	for _, variant := range []string{"matching", "missing_history", "history_error", "wrong_type", "wrong_queue", "wrong_tenant", "wrong_base", "wrong_dispatch", "invalid_input"} {
		t.Run(variant, func(t *testing.T) {
			in := emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: newID(), DispatchID: newID()}
			data, err := json.Marshal(emergencycontract.DispatchChangedData{BaseID: in.BaseID, DispatchID: in.DispatchID, Version: 3})
			require.NoError(t, err)
			evt := events.Event{ID: newID(), Type: emergencycontract.SubjectDispatchChanged, Version: 1, TenantID: in.TenantID, Data: data}
			id, err := emergency.DispatchWorkflowID(in)
			require.NoError(t, err)
			run := newID()
			c := &mocks.Client{}
			history := &mocks.HistoryEventIterator{}
			c.On("ExecuteWorkflow", mock.Anything, mock.Anything, emergency.DispatchWorkflowName, in).Return(nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", run)).Once()
			c.On("GetWorkflowHistory", mock.Anything, id, run, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT).Return(history).Once()
			history.On("HasNext").Return(variant != "missing_history").Once()
			if variant != "missing_history" {
				previous := in
				if variant == "wrong_tenant" {
					previous.TenantID = testutil.TenantB
				}
				if variant == "wrong_base" {
					previous.BaseID = newID()
				}
				if variant == "wrong_dispatch" {
					previous.DispatchID = newID()
				}
				payload, err := converter.GetDefaultDataConverter().ToPayloads(previous)
				require.NoError(t, err)
				if variant == "invalid_input" {
					payload.Payloads[0].Data = []byte("not-json")
				}
				started := &historypb.WorkflowExecutionStartedEventAttributes{WorkflowType: &commonpb.WorkflowType{Name: emergency.DispatchWorkflowName}, TaskQueue: &taskqueuepb.TaskQueue{Name: emergency.DispatchTaskQueue}, Input: payload}
				if variant == "wrong_type" {
					started.WorkflowType.Name = "other-workflow"
				}
				if variant == "wrong_queue" {
					started.TaskQueue.Name = "other-queue"
				}
				event := &historypb.HistoryEvent{Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{WorkflowExecutionStartedEventAttributes: started}}
				var nextErr error
				if variant == "history_error" {
					nextErr = errors.New("private upstream text")
				}
				history.On("Next").Return(event, nextErr).Once()
			}
			if variant == "matching" {
				c.On("SignalWorkflow", mock.Anything, id, "", emergency.DispatchVersionSignal, int64(3)).Return(nil).Once()
			}
			err = emergency.DispatchConsumer(c).AfterCommit(t.Context(), evt)
			if variant == "matching" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private upstream text")
			}
			c.AssertExpectations(t)
			history.AssertExpectations(t)
		})
	}
}

func TestDispatchConsumerRejectsCompletedDescriptionWithoutRunReceipt(t *testing.T) {
	in := emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: newID(), DispatchID: newID()}
	data, err := json.Marshal(emergencycontract.DispatchChangedData{BaseID: in.BaseID, DispatchID: in.DispatchID, Version: 3})
	require.NoError(t, err)
	evt := events.Event{ID: newID(), Type: emergencycontract.SubjectDispatchChanged, Version: 1, TenantID: in.TenantID, Data: data}
	id, err := emergency.DispatchWorkflowID(in)
	require.NoError(t, err)
	c := &mocks.Client{}
	c.On("ExecuteWorkflow", mock.Anything, mock.Anything, emergency.DispatchWorkflowName, in).Return(nil, nil).Once()
	c.On("SignalWorkflow", mock.Anything, id, "", emergency.DispatchVersionSignal, int64(3)).Return(serviceerror.NewNotFound("closed")).Once()
	c.On("DescribeWorkflowExecution", mock.Anything, id, "").Return(&workflowservice.DescribeWorkflowExecutionResponse{WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enums.WORKFLOW_EXECUTION_STATUS_COMPLETED}}, nil).Once()
	history := &mocks.HistoryEventIterator{}
	c.On("GetWorkflowHistory", mock.Anything, id, "", false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT).Return(history).Maybe()
	history.On("HasNext").Return(false).Maybe()
	require.ErrorContains(t, emergency.DispatchConsumer(c).AfterCommit(t.Context(), evt), "reconciliation")
	c.AssertExpectations(t)
}

func TestDispatchConsumerAcceptsOnlyCompletedMatchingReceipts(t *testing.T) {
	for _, state := range []enums.WorkflowExecutionStatus{enums.WORKFLOW_EXECUTION_STATUS_COMPLETED, enums.WORKFLOW_EXECUTION_STATUS_FAILED, enums.WORKFLOW_EXECUTION_STATUS_CANCELED, enums.WORKFLOW_EXECUTION_STATUS_TERMINATED} {
		t.Run(state.String(), func(t *testing.T) {
			in := emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: newID(), DispatchID: newID()}
			data, err := json.Marshal(emergencycontract.DispatchChangedData{BaseID: in.BaseID, DispatchID: in.DispatchID, Version: 8})
			require.NoError(t, err)
			evt := events.Event{ID: newID(), Type: emergencycontract.SubjectDispatchChanged, Version: 1, TenantID: in.TenantID, Data: data}
			id, err := emergency.DispatchWorkflowID(in)
			require.NoError(t, err)
			run := newID()
			c := &mocks.Client{}
			c.On("ExecuteWorkflow", mock.Anything, mock.Anything, emergency.DispatchWorkflowName, in).Return(nil, nil).Once()
			c.On("SignalWorkflow", mock.Anything, id, "", emergency.DispatchVersionSignal, int64(8)).Return(serviceerror.NewNotFound("closed")).Once()
			c.On("DescribeWorkflowExecution", mock.Anything, id, "").Return(&workflowservice.DescribeWorkflowExecutionResponse{WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: state, Execution: &commonpb.WorkflowExecution{WorkflowId: id, RunId: run}}}, nil).Once()
			if state == enums.WORKFLOW_EXECUTION_STATUS_COMPLETED {
				history := &mocks.HistoryEventIterator{}
				payload, err := converter.GetDefaultDataConverter().ToPayloads(in)
				require.NoError(t, err)
				started := &historypb.WorkflowExecutionStartedEventAttributes{WorkflowType: &commonpb.WorkflowType{Name: emergency.DispatchWorkflowName}, TaskQueue: &taskqueuepb.TaskQueue{Name: emergency.DispatchTaskQueue}, Input: payload}
				c.On("GetWorkflowHistory", mock.Anything, id, run, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT).Return(history).Once()
				history.On("HasNext").Return(true).Once()
				history.On("Next").Return(&historypb.HistoryEvent{Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{WorkflowExecutionStartedEventAttributes: started}}, nil).Once()
				require.NoError(t, emergency.DispatchConsumer(c).AfterCommit(t.Context(), evt))
				history.AssertExpectations(t)
			} else {
				require.ErrorContains(t, emergency.DispatchConsumer(c).AfterCommit(t.Context(), evt), "reconciliation")
			}
			c.AssertExpectations(t)
		})
	}
}

func TestExistingTripsKeepTheirConsumersWhenNewEntryIsSuspended(t *testing.T) {
	deps := platform.Deps{Workflows: &mocks.Client{}}
	module := &emergency.Module{}
	require.Empty(t, module.Consumers(deps))
	deps.EmergencyWorkflowsEnabled = true
	consumers := module.Consumers(deps)
	require.Len(t, consumers, 2)
	require.Equal(t, "emergency-dispatch", consumers[0].Name)
	require.Equal(t, "emergency-receiving", consumers[1].Name)
}
