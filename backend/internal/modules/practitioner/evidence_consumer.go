package practitioner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract" //nolint:depguard // own module and data-only contract
	"github.com/tesserix/helivanta/pkg/events"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"gorm.io/gorm"
)

// EvidenceWorkflowID includes the immutable tenant, revision and evidence scope.
func EvidenceWorkflowID(in practitionercontract.EvidenceScanInput) (string, error) {
	if !validEvidenceScanInput(in) {
		return "", ErrEvidenceNotReady
	}
	kind, credential, _ := scanCredential(in)
	prefix := "practitioner-evidence"
	if kind == "qualification" {
		prefix = "practitioner-qualification-evidence"
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s-%s-a%d", prefix, in.TenantID, in.PractitionerID, credential, in.RevisionID, in.FileID, in.ScanAttempt), nil
}

func evidenceEventInput(evt events.Event) (practitionercontract.EvidenceScanInput, error) {
	if evt.Type != practitionercontract.EvidenceScanRequested || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 1024 {
		return practitionercontract.EvidenceScanInput{}, errors.New("evidence scan envelope invalid")
	}
	var data practitionercontract.EvidenceScanRequestedData
	dec := json.NewDecoder(bytes.NewReader(evt.Data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		return practitionercontract.EvidenceScanInput{}, errors.New("evidence scan payload invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return practitionercontract.EvidenceScanInput{}, errors.New("evidence scan payload invalid")
	}
	in := practitionercontract.EvidenceScanInput{TenantID: evt.TenantID, PractitionerID: data.PractitionerID, RegistrationID: data.RegistrationID, QualificationID: data.QualificationID, RevisionID: data.RevisionID, FileID: data.FileID, ScanAttempt: data.ScanAttempt}
	if !validEvidenceScanInput(in) {
		return practitionercontract.EvidenceScanInput{}, errors.New("evidence scan scope invalid")
	}
	return in, nil
}

// EvidenceScanConsumer acknowledges only an accepted, matching durable execution.
func EvidenceScanConsumer(c client.Client) events.Consumer {
	return events.Consumer{
		Name: "practitioner-evidence-scan", Subject: practitionercontract.EvidenceScanRequested,
		Handle: func(_ context.Context, _ *gorm.DB, evt events.Event) error {
			_, err := evidenceEventInput(evt)
			return err
		},
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
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
			var previous practitionercontract.EvidenceScanInput
			if err := converter.GetDefaultDataConverter().FromPayloads(started.GetInput(), &previous); err != nil || previous != in {
				return errors.New("evidence workflow receipt mismatch")
			}
			return nil
		},
	}
}
