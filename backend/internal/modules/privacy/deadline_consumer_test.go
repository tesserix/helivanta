package privacy_test

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/privacy" //nolint:depguard // module under test
	privacycontract "github.com/tesserix/helivanta/internal/modules/privacy/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	commonpb "go.temporal.io/api/common/v1"
	enums "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/serviceerror"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/mocks"
	"testing"
)

func TestDeadlineConsumerStartsAndSignalsOnlyItsCommittedScope(t *testing.T) {
	in := privacycontract.DeadlineWorkflowInput{TenantID: testutil.TenantA, RequestID: uuid.Must(uuid.NewV7()).String()}
	data, err := json.Marshal(map[string]any{"request_id": in.RequestID, "version": 1})
	require.NoError(t, err)
	evt := events.Event{ID: uuid.Must(uuid.NewV7()).String(), Type: "helivanta.in.privacy.deadline_changed.v1", Version: 1, TenantID: in.TenantID, Data: data}
	c := new(mocks.Client)
	id := "privacy-deadline-" + in.TenantID + "-" + in.RequestID
	c.On("ExecuteWorkflow", mock.Anything, mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
		return options.ID == id && options.TaskQueue == privacy.DeadlineTaskQueue
	}), privacy.DeadlineWorkflowName, in).Return(nil, nil).Once()
	c.On("SignalWorkflow", mock.Anything, id, "", privacy.DeadlineChangedSignal, int64(1)).Return(nil).Once()
	consumer := privacy.DeadlineConsumer(c)
	require.NoError(t, consumer.Handle(t.Context(), nil, evt))
	require.NoError(t, consumer.AfterCommit(t.Context(), evt))
	c.AssertExpectations(t)
}

func TestDeadlineConsumerRejectsPrivateOrAmbiguousEventPayloads(t *testing.T) {
	id := uuid.Must(uuid.NewV7()).String()
	valid := events.Event{Type: privacycontract.SubjectDeadlineChanged, Version: 1, TenantID: testutil.TenantA, Data: json.RawMessage(`{"request_id":"` + id + `","version":1}`)}
	changes := []func(*events.Event){
		func(e *events.Event) { e.Version = 2 }, func(e *events.Event) { e.TenantID = "" }, func(e *events.Event) { e.DestinationTenantID = testutil.TenantB },
		func(e *events.Event) {
			e.Data = json.RawMessage(`{"request_id":"` + id + `","version":1,"patient":"private-data"}`)
		},
		func(e *events.Event) { e.Data = append(e.Data, []byte(` {}`)...) }, func(e *events.Event) { e.Data = json.RawMessage(`null`) },
		func(e *events.Event) { e.Data = json.RawMessage(`{"request_id":"` + id + `","version":0}`) },
	}
	for _, change := range changes {
		evt := valid
		change(&evt)
		consumer := privacy.DeadlineConsumer(nil)
		require.Error(t, consumer.Handle(t.Context(), nil, evt))
		err := consumer.AfterCommit(t.Context(), evt)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-data")
	}
	require.Error(t, privacy.DeadlineConsumer(nil).AfterCommit(t.Context(), valid))
}

func TestDeadlineConsumerVerifiesDuplicateExecutionBeforeSignalling(t *testing.T) {
	for _, variant := range []string{"matching", "missing_history", "history_error", "wrong_type", "wrong_queue", "wrong_tenant", "wrong_case"} {
		t.Run(variant, func(t *testing.T) {
			in := privacycontract.DeadlineWorkflowInput{TenantID: testutil.TenantA, RequestID: uuid.Must(uuid.NewV7()).String()}
			data, err := json.Marshal(privacycontract.DeadlineChangedData{RequestID: in.RequestID, Version: 3})
			require.NoError(t, err)
			evt := events.Event{Type: privacycontract.SubjectDeadlineChanged, Version: 1, TenantID: in.TenantID, Data: data}
			id, err := privacy.DeadlineWorkflowID(in)
			require.NoError(t, err)
			run := uuid.Must(uuid.NewV7()).String()
			c := new(mocks.Client)
			history := new(mocks.HistoryEventIterator)
			c.On("ExecuteWorkflow", mock.Anything, mock.Anything, privacy.DeadlineWorkflowName, in).Return(nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", run)).Once()
			c.On("GetWorkflowHistory", mock.Anything, id, run, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT).Return(history).Once()
			history.On("HasNext").Return(variant != "missing_history").Once()
			if variant != "missing_history" {
				previous := in
				if variant == "wrong_tenant" {
					previous.TenantID = testutil.TenantB
				}
				if variant == "wrong_case" {
					previous.RequestID = uuid.Must(uuid.NewV7()).String()
				}
				payload, err := converter.GetDefaultDataConverter().ToPayloads(previous)
				require.NoError(t, err)
				started := &historypb.WorkflowExecutionStartedEventAttributes{WorkflowType: &commonpb.WorkflowType{Name: privacy.DeadlineWorkflowName}, TaskQueue: &taskqueuepb.TaskQueue{Name: privacy.DeadlineTaskQueue}, Input: payload}
				if variant == "wrong_type" {
					started.WorkflowType.Name = "other"
				}
				if variant == "wrong_queue" {
					started.TaskQueue.Name = "other"
				}
				event := &historypb.HistoryEvent{Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{WorkflowExecutionStartedEventAttributes: started}}
				var nextErr error
				if variant == "history_error" {
					nextErr = errors.New("private upstream text")
				}
				history.On("Next").Return(event, nextErr).Once()
			}
			if variant == "matching" {
				c.On("SignalWorkflow", mock.Anything, id, "", privacy.DeadlineChangedSignal, int64(3)).Return(nil).Once()
			}
			err = privacy.DeadlineConsumer(c).AfterCommit(t.Context(), evt)
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
