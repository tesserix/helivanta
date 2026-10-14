package workforce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/events"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"gorm.io/gorm"
)

// DocumentWorkflowID includes exact origin scope and a committed retry attempt.
func DocumentWorkflowID(in workforcecontract.DocumentScanInput) (string, error) {
	if !validScanInput(in) {
		return "", ErrDocumentNotReady
	}
	return fmt.Sprintf("wf-doc-%s-%s-%s-%s-a%d", in.TenantID, in.HospitalID, in.StaffID, in.FileID, in.ScanAttempt), nil
}

func documentEventInput(evt events.Event) (workforcecontract.DocumentScanInput, error) {
	if evt.Type != DocumentScanRequested || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 1024 {
		return workforcecontract.DocumentScanInput{}, errors.New("document scan envelope invalid")
	}
	var data workforcecontract.DocumentScanRequestedData
	dec := json.NewDecoder(bytes.NewReader(evt.Data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		return workforcecontract.DocumentScanInput{}, errors.New("document scan payload invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return workforcecontract.DocumentScanInput{}, errors.New("document scan payload invalid")
	}
	in := workforcecontract.DocumentScanInput{TenantID: evt.TenantID, HospitalID: data.HospitalID, StaffID: data.StaffID, FileID: data.FileID, ScanAttempt: data.ScanAttempt}
	if !validScanInput(in) {
		return workforcecontract.DocumentScanInput{}, errors.New("document scan scope invalid")
	}
	return in, nil
}

// DocumentScanConsumer acknowledges only an accepted, matching durable execution.
func DocumentScanConsumer(c client.Client) events.Consumer {
	return events.Consumer{
		Name: "workforce-document-scan", Subject: DocumentScanRequested,
		Handle: func(_ context.Context, _ *gorm.DB, evt events.Event) error {
			_, err := documentEventInput(evt)
			return err
		},
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			in, err := documentEventInput(evt)
			if err != nil {
				return err
			}
			if c == nil {
				return errors.New("document workflow client unavailable")
			}
			id, err := DocumentWorkflowID(in)
			if err != nil {
				return err
			}
			_, err = c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
				ID: id, TaskQueue: DocumentTaskQueue,
				WorkflowExecutionTimeout:                 45 * time.Minute,
				WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
				WorkflowIDConflictPolicy:                 enums.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
				WorkflowExecutionErrorWhenAlreadyStarted: true,
			}, DocumentWorkflowName, in)
			if err == nil {
				return nil
			}
			var duplicate *serviceerror.WorkflowExecutionAlreadyStarted
			if !errors.As(err, &duplicate) || duplicate.RunId == "" {
				return errors.New("document workflow start unavailable")
			}
			it := c.GetWorkflowHistory(ctx, id, duplicate.RunId, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
			if !it.HasNext() {
				return errors.New("document workflow receipt unavailable")
			}
			first, err := it.Next()
			if err != nil {
				return errors.New("document workflow receipt unavailable")
			}
			started := first.GetWorkflowExecutionStartedEventAttributes()
			if started == nil || started.GetWorkflowType().GetName() != DocumentWorkflowName || started.GetTaskQueue().GetName() != DocumentTaskQueue {
				return errors.New("document workflow receipt mismatch")
			}
			var previous workforcecontract.DocumentScanInput
			if err := converter.GetDefaultDataConverter().FromPayloads(started.GetInput(), &previous); err != nil || previous != in {
				return errors.New("document workflow receipt mismatch")
			}
			return nil
		},
	}
}
