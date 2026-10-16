package supply

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own module and data-only contract
	"github.com/tesserix/helivanta/pkg/events"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"gorm.io/gorm"
)

// EvidenceWorkflowID includes exact origin scope and a committed retry attempt.
func EvidenceWorkflowID(in supplycontract.EvidenceScanInput) (string, error) {
	if !validEvidenceScanInput(in) {
		return "", ErrEvidenceNotReady
	}
	return fmt.Sprintf("supply-evidence-%s-%s-%s-%s-a%d", in.TenantID, in.PartyID, in.LicenseID, in.FileID, in.ScanAttempt), nil
}

func evidenceEventInput(evt events.Event) (supplycontract.EvidenceScanInput, error) {
	if evt.Type != supplycontract.SubjectEvidenceScanRequested || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 1024 {
		return supplycontract.EvidenceScanInput{}, errors.New("evidence scan envelope invalid")
	}
	var data supplycontract.EvidenceScanRequestedData
	dec := json.NewDecoder(bytes.NewReader(evt.Data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		return supplycontract.EvidenceScanInput{}, errors.New("evidence scan payload invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return supplycontract.EvidenceScanInput{}, errors.New("evidence scan payload invalid")
	}
	in := supplycontract.EvidenceScanInput{TenantID: evt.TenantID, PartyID: data.PartyID, LicenseID: data.LicenseID, FileID: data.FileID, ScanAttempt: data.ScanAttempt}
	if !validEvidenceScanInput(in) {
		return supplycontract.EvidenceScanInput{}, errors.New("evidence scan scope invalid")
	}
	return in, nil
}

// EvidenceScanConsumer acknowledges only an accepted, matching durable execution.
func EvidenceScanConsumer(c client.Client) events.Consumer {
	return events.Consumer{
		Name: "supply-evidence-scan", Subject: supplycontract.SubjectEvidenceScanRequested,
		Handle: func(_ context.Context, _ *gorm.DB, evt events.Event) error {
			_, err := evidenceEventInput(evt)
			return err
		},
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			in, err := evidenceEventInput(evt)
			if err != nil {
				return err
			}
			if c == nil {
				return errors.New("evidence workflow client unavailable")
			}
			id, err := EvidenceWorkflowID(in)
			if err != nil {
				return err
			}
			_, err = c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
				ID: id, TaskQueue: EvidenceTaskQueue,
				WorkflowExecutionTimeout:                 45 * time.Minute,
				WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
				WorkflowIDConflictPolicy:                 enums.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
				WorkflowExecutionErrorWhenAlreadyStarted: true,
			}, EvidenceWorkflowName, in)
			if err == nil {
				return nil
			}
			var duplicate *serviceerror.WorkflowExecutionAlreadyStarted
			if !errors.As(err, &duplicate) || duplicate.RunId == "" {
				return errors.New("evidence workflow start unavailable")
			}
			it := c.GetWorkflowHistory(ctx, id, duplicate.RunId, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
			if !it.HasNext() {
				return errors.New("evidence workflow receipt unavailable")
			}
			first, err := it.Next()
			if err != nil {
				return errors.New("evidence workflow receipt unavailable")
			}
			started := first.GetWorkflowExecutionStartedEventAttributes()
			if started == nil || started.GetWorkflowType().GetName() != EvidenceWorkflowName || started.GetTaskQueue().GetName() != EvidenceTaskQueue {
				return errors.New("evidence workflow receipt mismatch")
			}
			var previous supplycontract.EvidenceScanInput
			if err := converter.GetDefaultDataConverter().FromPayloads(started.GetInput(), &previous); err != nil || previous != in {
				return errors.New("evidence workflow receipt mismatch")
			}
			return nil
		},
	}
}
