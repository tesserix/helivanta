package pharmacy

import (
	"context"
	"errors"
	"time"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own data-only contract
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"gorm.io/gorm"
)

const PrescriptionImageWorkflowName = "pharmacy-prescription-image-processing-v1"
const PrescriptionImageActivityName = "pharmacy-prescription-image-process-v1"
const PrescriptionImageTaskQueue = "pharmacy-prescription-image-v1"

// PrescriptionImageWorkflow retries only the generation-bound, idempotent owning activity.
func PrescriptionImageWorkflow(ctx workflow.Context, in pharmacycontract.PrescriptionImageScanInput) (string, error) {
	if !validImageScanInput(in) {
		return "", temporal.NewNonRetryableApplicationError("evidence processing input invalid", "evidence_invalid", nil)
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
	if err := workflow.ExecuteActivity(ctx, PrescriptionImageActivityName, in).Get(ctx, &status); err != nil {
		return "", err
	}
	return status, nil
}

// ProcessActivity confines Temporal history to opaque input and bounded outcomes.
func (p *PrescriptionImageProcessor) ProcessActivity(ctx context.Context, in pharmacycontract.PrescriptionImageScanInput) (string, error) {
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
	if errors.Is(err, ErrPrescriptionImageNotReady) || errors.Is(err, ErrPrescriptionImageIntegrity) || errors.Is(err, gorm.ErrRecordNotFound) {
		return "", temporal.NewNonRetryableApplicationError("evidence processing input unavailable", "evidence_invalid", nil)
	}
	return "", temporal.NewApplicationError("evidence processing unavailable", "evidence_unavailable")
}
