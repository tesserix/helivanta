package emergency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/pkg/events"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"gorm.io/gorm"
	"io"
)

// DispatchWorkflowID binds all origin scope; a reassignment gets a new dispatch identity.
func DispatchWorkflowID(in emergencycontract.DispatchWorkflowInput) (string, error) {
	if !validDispatchWorkflowInput(in) {
		return "", errors.New("invalid dispatch workflow scope")
	}
	return fmt.Sprintf("emergency-dispatch-%s-%s-%s", in.TenantID, in.BaseID, in.DispatchID), nil
}
func dispatchEventInput(evt events.Event) (emergencycontract.DispatchWorkflowInput, int64, error) {
	var data emergencycontract.DispatchChangedData
	if evt.Type != emergencycontract.SubjectDispatchChanged || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 512 {
		return emergencycontract.DispatchWorkflowInput{}, 0, errors.New("invalid dispatch envelope")
	}
	decoder := json.NewDecoder(bytes.NewReader(evt.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return emergencycontract.DispatchWorkflowInput{}, 0, errors.New("invalid dispatch payload")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return emergencycontract.DispatchWorkflowInput{}, 0, errors.New("invalid dispatch payload")
	}
	in := emergencycontract.DispatchWorkflowInput{TenantID: evt.TenantID, BaseID: data.BaseID, DispatchID: data.DispatchID}
	if !validDispatchWorkflowInput(in) || data.Version < 1 {
		return in, 0, errors.New("invalid dispatch scope")
	}
	return in, data.Version, nil
}
func verifyDispatchExecution(ctx context.Context, c client.Client, id, run string, in emergencycontract.DispatchWorkflowInput) error {
	history := c.GetWorkflowHistory(ctx, id, run, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	if !history.HasNext() {
		return errors.New("dispatch workflow receipt unavailable")
	}
	event, err := history.Next()
	if err != nil {
		return errors.New("dispatch workflow receipt unavailable")
	}
	started := event.GetWorkflowExecutionStartedEventAttributes()
	if started == nil || started.GetWorkflowType().GetName() != DispatchWorkflowName || started.GetTaskQueue().GetName() != DispatchTaskQueue {
		return errors.New("dispatch workflow receipt mismatch")
	}
	var previous emergencycontract.DispatchWorkflowInput
	if err := converter.GetDefaultDataConverter().FromPayloads(started.GetInput(), &previous); err != nil || previous != in {
		return errors.New("dispatch workflow receipt mismatch")
	}
	return nil
}

// DispatchConsumer starts and signals only matching durable workflow executions.
func DispatchConsumer(c client.Client) events.Consumer {
	return events.Consumer{
		Name: "emergency-dispatch", Subject: emergencycontract.SubjectDispatchChanged,
		Handle: func(_ context.Context, _ *gorm.DB, evt events.Event) error {
			_, _, err := dispatchEventInput(evt)
			return err
		},
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			in, version, err := dispatchEventInput(evt)
			if err != nil {
				return err
			}
			if c == nil {
				return errors.New("dispatch workflow client unavailable")
			}
			id, err := DispatchWorkflowID(in)
			if err != nil {
				return err
			}
			_, err = c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
				ID: id, TaskQueue: DispatchTaskQueue,
				WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
				WorkflowIDConflictPolicy:                 enums.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
				WorkflowExecutionErrorWhenAlreadyStarted: true,
			}, DispatchWorkflowName, in)
			if err != nil {
				var duplicate *serviceerror.WorkflowExecutionAlreadyStarted
				if !errors.As(err, &duplicate) || duplicate.RunId == "" {
					return errors.New("dispatch workflow start unavailable")
				}
				if err := verifyDispatchExecution(ctx, c, id, duplicate.RunId, in); err != nil {
					return err
				}
			}
			if err := c.SignalWorkflow(ctx, id, "", DispatchVersionSignal, version); err != nil {
				var closed *serviceerror.NotFound
				if !errors.As(err, &closed) {
					return errors.New("dispatch workflow signal unavailable")
				}
				description, err := c.DescribeWorkflowExecution(ctx, id, "")
				if err != nil || description.GetWorkflowExecutionInfo().GetStatus() != enums.WORKFLOW_EXECUTION_STATUS_COMPLETED {
					return errors.New("dispatch workflow needs reconciliation")
				}
				if err := verifyDispatchExecution(ctx, c, id, description.GetWorkflowExecutionInfo().GetExecution().GetRunId(), in); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
