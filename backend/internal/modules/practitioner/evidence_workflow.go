package practitioner

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"gorm.io/gorm"
)

const EvidenceWorkflowName = "practitioner-evidence-processing-v1"
const EvidenceActivityName = "practitioner-evidence-process-v1"
const EvidenceTaskQueue = "practitioner-evidence-v1"

// EvidenceWorkflow retries only the owning generation-bound activity.
func EvidenceWorkflow(ctx workflow.Context, in practitionercontract.EvidenceScanInput) (string, error) {
	if !validEvidenceScanInput(in) {
		return "", temporal.NewNonRetryableApplicationError("professional evidence input invalid", "evidence_invalid", nil)
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 6 * time.Minute, ScheduleToCloseTimeout: 40 * time.Minute, HeartbeatTimeout: 30 * time.Second, RetryPolicy: &temporal.RetryPolicy{InitialInterval: 5 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute, MaximumAttempts: 5}})
	var status string
	if err := workflow.ExecuteActivity(ctx, EvidenceActivityName, in).Get(ctx, &status); err != nil {
		return "", err
	}
	return status, nil
}

// ProcessActivity keeps protected storage/SQL details out of durable history.
func (p *EvidenceProcessor) ProcessActivity(ctx context.Context, in practitionercontract.EvidenceScanInput) (string, error) {
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
	var pg *pgconn.PgError
	if errors.Is(err, ErrEvidenceNotReady) || errors.Is(err, ErrEvidenceIntegrity) || errors.Is(err, gorm.ErrRecordNotFound) || errors.As(err, &pg) && (pg.Code == "23514" || pg.Code == "42501") {
		return "", temporal.NewNonRetryableApplicationError("professional evidence input or integrity unavailable", "evidence_not_ready", nil)
	}
	return "", temporal.NewApplicationError("professional evidence processing temporarily unavailable", "evidence_unavailable")
}
