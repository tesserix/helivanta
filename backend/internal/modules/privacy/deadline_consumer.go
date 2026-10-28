package privacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	privacycontract "github.com/tesserix/helivanta/internal/modules/privacy/contract"
	"github.com/tesserix/helivanta/pkg/events"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"gorm.io/gorm"
	"io"
)

// DeadlineWorkflowID binds a clock to its immutable tenant and case identity.
func DeadlineWorkflowID(in privacycontract.DeadlineWorkflowInput) (string, error) {
	if !validDeadlineInput(in) {
		return "", errors.New("invalid deadline workflow scope")
	}
	return fmt.Sprintf("privacy-deadline-%s-%s", in.TenantID, in.RequestID), nil
}
func deadlineEventInput(evt events.Event) (privacycontract.DeadlineWorkflowInput, int64, error) {
	var data privacycontract.DeadlineChangedData
	if evt.Type != privacycontract.SubjectDeadlineChanged || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 512 {
		return privacycontract.DeadlineWorkflowInput{}, 0, errors.New("invalid deadline envelope")
	}
	decoder := json.NewDecoder(bytes.NewReader(evt.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return privacycontract.DeadlineWorkflowInput{}, 0, errors.New("invalid deadline payload")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return privacycontract.DeadlineWorkflowInput{}, 0, errors.New("invalid deadline payload")
	}
	in := privacycontract.DeadlineWorkflowInput{TenantID: evt.TenantID, RequestID: data.RequestID}
	if !validDeadlineInput(in) || data.Version < 1 {
		return in, 0, errors.New("invalid deadline scope")
	}
	return in, data.Version, nil
}
func verifyDeadlineExecution(ctx context.Context, c client.Client, id, run string, in privacycontract.DeadlineWorkflowInput) error {
	history := c.GetWorkflowHistory(ctx, id, run, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	if !history.HasNext() {
		return errors.New("deadline workflow receipt unavailable")
	}
	event, err := history.Next()
	if err != nil {
		return errors.New("deadline workflow receipt unavailable")
	}
	started := event.GetWorkflowExecutionStartedEventAttributes()
	if started == nil || started.GetWorkflowType().GetName() != DeadlineWorkflowName || started.GetTaskQueue().GetName() != DeadlineTaskQueue {
		return errors.New("deadline workflow receipt mismatch")
	}
	var previous privacycontract.DeadlineWorkflowInput
	if err := converter.GetDefaultDataConverter().FromPayloads(started.GetInput(), &previous); err != nil || previous != in {
		return errors.New("deadline workflow receipt mismatch")
	}
	return nil
}

// DeadlineConsumer starts and signals only matching durable workflow executions.
func DeadlineConsumer(c client.Client) events.Consumer {
	return events.Consumer{
		Name: "privacy-deadline", Subject: privacycontract.SubjectDeadlineChanged,
		Handle: func(_ context.Context, _ *gorm.DB, evt events.Event) error {
			_, _, err := deadlineEventInput(evt)
			return err
		},
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			in, version, err := deadlineEventInput(evt)
			if err != nil {
				return err
			}
			if c == nil {
				return errors.New("deadline workflow client unavailable")
			}
			id, err := DeadlineWorkflowID(in)
			if err != nil {
				return err
			}
			_, err = c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
				ID: id, TaskQueue: DeadlineTaskQueue,
				WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
				WorkflowIDConflictPolicy:                 enums.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
				WorkflowExecutionErrorWhenAlreadyStarted: true,
			}, DeadlineWorkflowName, in)
			if err != nil {
				var duplicate *serviceerror.WorkflowExecutionAlreadyStarted
				if !errors.As(err, &duplicate) || duplicate.RunId == "" {
					return errors.New("deadline workflow start unavailable")
				}
				if err := verifyDeadlineExecution(ctx, c, id, duplicate.RunId, in); err != nil {
					return err
				}
			}
			if err := c.SignalWorkflow(ctx, id, "", DeadlineChangedSignal, version); err != nil {
				var closed *serviceerror.NotFound
				if !errors.As(err, &closed) {
					return errors.New("deadline workflow signal unavailable")
				}
				description, err := c.DescribeWorkflowExecution(ctx, id, "")
				if err != nil || description.GetWorkflowExecutionInfo().GetStatus() != enums.WORKFLOW_EXECUTION_STATUS_COMPLETED || description.GetWorkflowExecutionInfo().GetExecution().GetRunId() == "" {
					return errors.New("deadline workflow needs reconciliation")
				}
				if err := verifyDeadlineExecution(ctx, c, id, description.GetWorkflowExecutionInfo().GetExecution().GetRunId(), in); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
