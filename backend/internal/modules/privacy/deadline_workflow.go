package privacy

import (
	"github.com/google/uuid"
	privacycontract "github.com/tesserix/helivanta/internal/modules/privacy/contract"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"time"
)

const (
	DeadlineWorkflowName    = "privacy-case-deadline-v1"
	DeadlineTaskQueue       = "privacy-case-deadline-v1"
	DeadlineChangedSignal   = "privacy-case-deadline-changed"
	DeadlineRefreshActivity = "privacy-case-deadline-refresh-v1"
	DeadlineExpireActivity  = "privacy-case-deadline-expire-v1"
)

// DeadlineSnapshot contains authoritative clock metadata, never private case text.
type DeadlineSnapshot struct {
	Version                  int64
	ObservedAt               time.Time
	AcknowledgeBy            time.Time
	ResolveBy                time.Time
	Acknowledged             bool
	Paused                   bool
	Terminal                 bool
	AcknowledgementEscalated bool
	ResolutionEscalated      bool
}

// DeadlineExpiryInput uses tenant/request/kind as its durable idempotency key.
type DeadlineExpiryInput struct {
	privacycontract.DeadlineWorkflowInput
	Kind string
}

func validDeadlineInput(in privacycontract.DeadlineWorkflowInput) bool {
	tenant, err := uuid.Parse(in.TenantID)
	return err == nil && tenant != uuid.Nil && tenant.String() == in.TenantID && validID(in.RequestID)
}

// DeadlineWorkflow owns waits only; activities recheck state and database time.
func DeadlineWorkflow(ctx workflow.Context, in privacycontract.DeadlineWorkflowInput) (string, error) {
	if !validDeadlineInput(in) {
		return "", temporal.NewNonRetryableApplicationError("invalid privacy deadline scope", "privacy_deadline_input", nil)
	}
	_ = workflow.GetVersion(ctx, "privacy-case-deadline-lifecycle", workflow.DefaultVersion, 1)
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 15 * time.Second, RetryPolicy: &temporal.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute}})
	signals := workflow.GetSignalChannel(ctx, DeadlineChangedSignal)
	notifications := 0
	consumed := 0
	workflow.Go(ctx, func(ctx workflow.Context) {
		for {
			var version int64
			signals.Receive(ctx, &version)
			notifications++
		}
	})
	var current DeadlineSnapshot
	if err := workflow.ExecuteActivity(ctx, DeadlineRefreshActivity, in).Get(ctx, &current); err != nil {
		return "", err
	}
	observed := workflow.Now(ctx)
	for {
		if current.Terminal {
			return "terminal", nil
		}
		if current.Version == 0 {
			return "unmonitored", nil
		}
		if current.ObservedAt.IsZero() {
			return "", temporal.NewNonRetryableApplicationError("privacy clock observation missing", "privacy_deadline_input", nil)
		}
		if current.ResolutionEscalated {
			return "escalated", nil
		}
		if notifications >= 256 {
			return "", workflow.NewContinueAsNewError(ctx, DeadlineWorkflowName, in)
		}
		kind := ""
		var due time.Time
		if !current.Acknowledged && !current.AcknowledgementEscalated && !current.AcknowledgeBy.IsZero() {
			kind = "acknowledgement"
			due = current.AcknowledgeBy
		}
		if !current.Paused && !current.ResolveBy.IsZero() && (due.IsZero() || current.ResolveBy.Before(due)) {
			kind = "resolution"
			due = current.ResolveBy
		}
		changed := func() bool { return notifications > consumed }
		if kind == "" {
			if err := workflow.Await(ctx, changed); err != nil {
				return "", err
			}
		} else {
			// Relative database time avoids spinning when worker and database clocks differ.
			remaining := due.Sub(current.ObservedAt) - workflow.Now(ctx).Sub(observed)
			if remaining < 0 {
				remaining = 0
			}
			signaled, err := workflow.AwaitWithTimeout(ctx, remaining, changed)
			if err != nil {
				return "", err
			}
			if !signaled {
				if err := workflow.ExecuteActivity(ctx, DeadlineExpireActivity, DeadlineExpiryInput{DeadlineWorkflowInput: in, Kind: kind}).Get(ctx, &current); err != nil {
					return "", err
				}
				observed = workflow.Now(ctx)
				continue
			}
		}
		consumed = notifications
		if err := workflow.ExecuteActivity(ctx, DeadlineRefreshActivity, in).Get(ctx, &current); err != nil {
			return "", err
		}
		observed = workflow.Now(ctx)
	}
}
