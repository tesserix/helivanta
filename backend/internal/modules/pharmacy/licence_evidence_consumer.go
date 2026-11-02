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

// LicenceEvidenceWorkflowID includes exact origin scope and a committed retry attempt.
func LicenceEvidenceWorkflowID(in pharmacycontract.LicenceEvidenceScanInput) (string, error) {
	if !validLicenceEvidenceScanInput(in) {
		return "", ErrLicenceEvidenceNotReady
	}
	return fmt.Sprintf("pharmacy-licence-evidence-%s-%s-%s-%s-a%d", in.TenantID, in.OutletID, in.LicenceID, in.FileID, in.ScanAttempt), nil
}

func licenceEvidenceEventInput(evt events.Event) (pharmacycontract.LicenceEvidenceScanInput, error) {
	if evt.Type != pharmacycontract.SubjectLicenceEvidenceScanRequested || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 1024 {
		return pharmacycontract.LicenceEvidenceScanInput{}, errors.New("evidence scan envelope invalid")
	}
	var data pharmacycontract.LicenceEvidenceScanRequestedData
	dec := json.NewDecoder(bytes.NewReader(evt.Data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		return pharmacycontract.LicenceEvidenceScanInput{}, errors.New("evidence scan payload invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return pharmacycontract.LicenceEvidenceScanInput{}, errors.New("evidence scan payload invalid")
	}
	in := pharmacycontract.LicenceEvidenceScanInput{TenantID: evt.TenantID, OutletID: data.OutletID, LicenceID: data.LicenceID, FileID: data.FileID, ScanAttempt: data.ScanAttempt}
	if !validLicenceEvidenceScanInput(in) {
		return pharmacycontract.LicenceEvidenceScanInput{}, errors.New("evidence scan scope invalid")
	}
	return in, nil
}

// LicenceEvidenceScanConsumer acknowledges only an accepted, matching durable execution.
func LicenceEvidenceScanConsumer(c client.Client) events.Consumer {
	return events.Consumer{
		Name: "pharmacy-licence-evidence-scan", Subject: pharmacycontract.SubjectLicenceEvidenceScanRequested,
		Handle: func(_ context.Context, _ *gorm.DB, evt events.Event) error {
			_, err := licenceEvidenceEventInput(evt)
			return err
		},
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			in, err := licenceEvidenceEventInput(evt)
			if err != nil {
				return err
			}
			if c == nil {
				return errors.New("evidence workflow client unavailable")
			}
			id, err := LicenceEvidenceWorkflowID(in)
			if err != nil {
				return err
			}
			_, err = c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
				ID: id, TaskQueue: LicenceEvidenceTaskQueue,
				WorkflowExecutionTimeout:                 45 * time.Minute,
				WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
				WorkflowIDConflictPolicy:                 enums.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
				WorkflowExecutionErrorWhenAlreadyStarted: true,
			}, LicenceEvidenceWorkflowName, in)
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
			if started == nil || started.GetWorkflowType().GetName() != LicenceEvidenceWorkflowName || started.GetTaskQueue().GetName() != LicenceEvidenceTaskQueue {
				return errors.New("evidence workflow receipt mismatch")
			}
			var previous pharmacycontract.LicenceEvidenceScanInput
			if err := converter.GetDefaultDataConverter().FromPayloads(started.GetInput(), &previous); err != nil || previous != in {
				return errors.New("evidence workflow receipt mismatch")
			}
			return nil
		},
	}
}
