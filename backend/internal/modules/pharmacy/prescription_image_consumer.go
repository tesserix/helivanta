package pharmacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/pkg/events"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"gorm.io/gorm"
)

// PrescriptionImageWorkflowID includes exact origin scope and a committed retry attempt.
func PrescriptionImageWorkflowID(in pharmacycontract.PrescriptionImageScanInput) (string, error) {
	if !validImageScanInput(in) {
		return "", ErrPrescriptionImageNotReady
	}
	return fmt.Sprintf("pharmacy-prescription-image-%s-%s-%s-%s-a%d", in.TenantID, in.OutletID, in.PrescriptionID, in.FileID, in.ScanAttempt), nil
}

func imageEventInput(evt events.Event) (pharmacycontract.PrescriptionImageScanInput, error) {
	if evt.Type != pharmacycontract.SubjectPrescriptionImageScanRequested || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 1024 {
		return pharmacycontract.PrescriptionImageScanInput{}, errors.New("evidence scan envelope invalid")
	}
	var data pharmacycontract.PrescriptionImageScanRequestedData
	dec := json.NewDecoder(bytes.NewReader(evt.Data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		return pharmacycontract.PrescriptionImageScanInput{}, errors.New("evidence scan payload invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return pharmacycontract.PrescriptionImageScanInput{}, errors.New("evidence scan payload invalid")
	}
	in := pharmacycontract.PrescriptionImageScanInput{TenantID: evt.TenantID, OutletID: data.OutletID, PrescriptionID: data.PrescriptionID, FileID: data.FileID, ScanAttempt: data.ScanAttempt}
	if !validImageScanInput(in) {
		return pharmacycontract.PrescriptionImageScanInput{}, errors.New("evidence scan scope invalid")
	}
	return in, nil
}

// PrescriptionImageScanConsumer acknowledges only an accepted, matching durable execution.
func PrescriptionImageScanConsumer(c client.Client) events.Consumer {
	return events.Consumer{
		Name: "pharmacy-prescription-image-scan", Subject: pharmacycontract.SubjectPrescriptionImageScanRequested,
		Handle: func(_ context.Context, _ *gorm.DB, evt events.Event) error {
			_, err := imageEventInput(evt)
			return err
		},
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			in, err := imageEventInput(evt)
			if err != nil {
				return err
			}
			if c == nil {
				return errors.New("evidence workflow client unavailable")
			}
			id, err := PrescriptionImageWorkflowID(in)
			if err != nil {
				return err
			}
			_, err = c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
				ID: id, TaskQueue: PrescriptionImageTaskQueue,
				WorkflowExecutionTimeout:                 45 * time.Minute,
				WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
				WorkflowIDConflictPolicy:                 enums.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
				WorkflowExecutionErrorWhenAlreadyStarted: true,
			}, PrescriptionImageWorkflowName, in)
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
			if started == nil || started.GetWorkflowType().GetName() != PrescriptionImageWorkflowName || started.GetTaskQueue().GetName() != PrescriptionImageTaskQueue {
				return errors.New("evidence workflow receipt mismatch")
			}
			var previous pharmacycontract.PrescriptionImageScanInput
			if err := converter.GetDefaultDataConverter().FromPayloads(started.GetInput(), &previous); err != nil || previous != in {
				return errors.New("evidence workflow receipt mismatch")
			}
			return nil
		},
	}
}
