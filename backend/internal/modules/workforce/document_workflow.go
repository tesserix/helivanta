package workforce

import (
	"context"
	"errors"
	"time"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"gorm.io/gorm"
)

const DocumentWorkflowName = "workforce-document-processing-v1"
const DocumentActivityName = "workforce-document-process-v1"
const DocumentTaskQueue = "workforce-documents-v1"

// DocumentWorkflow retries only the generation-bound, idempotent owning activity.
func DocumentWorkflow(ctx workflow.Context, in workforcecontract.DocumentScanInput) (string, error) {
	if !validScanInput(in) {
		return "", temporal.NewNonRetryableApplicationError("document processing input invalid", "document_invalid", nil)
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    6 * time.Minute,
		ScheduleToCloseTimeout: 40 * time.Minute,
		HeartbeatTimeout:       30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	})
	var status string
	if err := workflow.ExecuteActivity(ctx, DocumentActivityName, in).Get(ctx, &status); err != nil {
		return "", err
	}
	return status, nil
}

// ProcessActivity confines Temporal history to opaque input and bounded outcomes.
func (p *DocumentProcessor) ProcessActivity(ctx context.Context, in workforcecontract.DocumentScanInput) (string, error) {
	activity.RecordHeartbeat(ctx)
	stop, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				activity.RecordHeartbeat(ctx)
			case <-ctx.Done():
				return
			case <-stop:
				return
			}
		}
	}()
	defer func() { close(stop); <-finished }()
	status, err := p.Process(ctx, in)
	if err == nil {
		return status, nil
	}
	// Raw storage, SQL and scanner errors must never enter durable history.
	if errors.Is(err, ErrDocumentNotReady) || errors.Is(err, ErrDocumentIntegrity) || errors.Is(err, gorm.ErrRecordNotFound) {
		return "", temporal.NewNonRetryableApplicationError("document processing input unavailable", "document_invalid", nil)
	}
	return "", temporal.NewApplicationError("document processing unavailable", "document_unavailable")
}
