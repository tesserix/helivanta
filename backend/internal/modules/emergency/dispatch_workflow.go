package emergency

import (
	"context"
	"errors"
	"github.com/google/uuid"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"gorm.io/gorm"
	"time"
)

const (
	DispatchWorkflowName            = "emergency-dispatch-v1"
	DispatchTaskQueue               = "emergency-dispatch-v1"
	DispatchVersionSignal           = "dispatch-version-changed"
	DispatchOfferActivity           = "emergency-dispatch-offer-v1"
	DispatchRefreshActivity         = "emergency-dispatch-refresh-v1"
	DispatchExpireActivity          = "emergency-dispatch-expire-v1"
	DispatchReceivingExpireActivity = "emergency-dispatch-receiving-expire-v1"
)

// DispatchExpiryInput carries only committed identifiers into the timeout activity.
type DispatchExpiryInput struct {
	emergencycontract.DispatchWorkflowInput
	CommandID string `json:"command_id"`
}

// DispatchWorkflow owns durable waits; Postgres remains authoritative for every action.
func DispatchWorkflow(ctx workflow.Context, in emergencycontract.DispatchWorkflowInput) (string, error) {
	if !validDispatchWorkflowInput(in) {
		return "", temporal.NewNonRetryableApplicationError("invalid dispatch identifiers", "dispatch_input_invalid", nil)
	}
	_ = workflow.GetVersion(ctx, "emergency-dispatch-lifecycle", workflow.DefaultVersion, 1)
	receivingVersion := workflow.GetVersion(ctx, "emergency-receiving-waits", workflow.DefaultVersion, 1)
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second, ScheduleToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 5 * time.Second, MaximumAttempts: 8},
	})
	var notified int64
	notifications := 0
	consumedNotifications := 0
	signals := workflow.GetSignalChannel(ctx, DispatchVersionSignal)
	workflow.Go(ctx, func(ctx workflow.Context) {
		for {
			var v int64
			signals.Receive(ctx, &v)
			notifications++
			if v > notified {
				notified = v
			}
		}
	})
	var current DispatchSnapshot
	if err := workflow.ExecuteActivity(ctx, DispatchOfferActivity, in).Get(ctx, &current); err != nil {
		return "", err
	}
	for {
		if current.State == "cleared" || current.State == "cancelled" {
			return current.State, nil
		}
		if notifications >= 256 {
			return "", workflow.NewContinueAsNewError(ctx, DispatchWorkflowName, in)
		}
		changed := func() bool {
			if receivingVersion != workflow.DefaultVersion {
				return notifications > consumedNotifications || notifications >= 256
			}
			return notified > current.Version || notifications >= 256
		}
		if current.State == "offered" {
			if current.AcknowledgementDeadline == nil {
				return "", temporal.NewNonRetryableApplicationError("dispatch deadline missing", "dispatch_input_invalid", nil)
			}
			remaining := current.AcknowledgementDeadline.Sub(workflow.Now(ctx))
			if remaining < 0 {
				remaining = 0
			}
			received, err := workflow.AwaitWithTimeout(ctx, remaining, changed)
			if err != nil {
				return "", err
			}
			if !received {
				var command string
				encoded := workflow.SideEffect(ctx, func(workflow.Context) any {
					id, err := uuid.NewV7()
					if err != nil {
						return ""
					}
					return id.String()
				})
				if err := encoded.Get(&command); err != nil {
					return "", err
				}
				if err := workflow.ExecuteActivity(ctx, DispatchExpireActivity, DispatchExpiryInput{DispatchWorkflowInput: in, CommandID: command}).Get(ctx, &current); err != nil {
					return "", err
				}
				continue
			}
		} else if receivingVersion != workflow.DefaultVersion && current.ReceivingDeadline != nil && !current.ReceivingTimedOut {
			if current.ReceivingRequestID == nil || (current.ReceivingDeadlineKind != "response" && current.ReceivingDeadlineKind != "acceptance") {
				return "", temporal.NewNonRetryableApplicationError("receiving deadline missing", "dispatch_input_invalid", nil)
			}
			remaining := current.ReceivingDeadline.Sub(workflow.Now(ctx))
			if remaining < 0 {
				remaining = 0
			}
			received, err := workflow.AwaitWithTimeout(ctx, remaining, changed)
			if err != nil {
				return "", err
			}
			if !received {
				input := DispatchReceivingExpiryInput{DispatchWorkflowInput: in, RequestID: current.ReceivingRequestID.String(), Kind: current.ReceivingDeadlineKind}
				if err := workflow.ExecuteActivity(ctx, DispatchReceivingExpireActivity, input).Get(ctx, &current); err != nil {
					return "", err
				}
				continue
			}
		} else if err := workflow.Await(ctx, changed); err != nil {
			return "", err
		}
		if notifications >= 256 {
			return "", workflow.NewContinueAsNewError(ctx, DispatchWorkflowName, in)
		}
		consumedNotifications = notifications
		if err := workflow.ExecuteActivity(ctx, DispatchRefreshActivity, in).Get(ctx, &current); err != nil {
			return "", err
		}
	}
}
func dispatchActivityResult(out DispatchSnapshot, err error) (DispatchSnapshot, error) {
	if err == nil {
		return out, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DispatchSnapshot{}, temporal.NewNonRetryableApplicationError("dispatch input unavailable", "dispatch_input_invalid", nil)
	}
	return DispatchSnapshot{}, temporal.NewApplicationError("dispatch storage unavailable", "dispatch_unavailable")
}

// OfferActivity never writes private database errors to durable history.
func (p *DispatchProcessor) OfferActivity(ctx context.Context, in emergencycontract.DispatchWorkflowInput) (DispatchSnapshot, error) {
	if !validDispatchWorkflowInput(in) {
		return DispatchSnapshot{}, temporal.NewNonRetryableApplicationError("invalid dispatch identifiers", "dispatch_input_invalid", nil)
	}
	out, err := p.Offer(ctx, in)
	return dispatchActivityResult(out, err)
}

// RefreshActivity verifies signal hints against the committed dispatch state.
func (p *DispatchProcessor) RefreshActivity(ctx context.Context, in emergencycontract.DispatchWorkflowInput) (DispatchSnapshot, error) {
	if !validDispatchWorkflowInput(in) {
		return DispatchSnapshot{}, temporal.NewNonRetryableApplicationError("invalid dispatch identifiers", "dispatch_input_invalid", nil)
	}
	out, err := p.Snapshot(ctx, in)
	return dispatchActivityResult(out, err)
}

// ExpireActivity retains capacity until an authorized terminal reconciliation.
func (p *DispatchProcessor) ExpireActivity(ctx context.Context, in DispatchExpiryInput) (DispatchSnapshot, error) {
	command, err := uuid.Parse(in.CommandID)
	if !validDispatchWorkflowInput(in.DispatchWorkflowInput) || err != nil || command.Version() != 7 {
		return DispatchSnapshot{}, temporal.NewNonRetryableApplicationError("invalid dispatch identifiers", "dispatch_input_invalid", nil)
	}
	out, err := p.Expire(ctx, in.DispatchWorkflowInput, command)
	return dispatchActivityResult(out, err)
}

// ReceivingExpireActivity records a deadline escalation without exposing database diagnostics.
func (p *DispatchProcessor) ReceivingExpireActivity(ctx context.Context, in DispatchReceivingExpiryInput) (DispatchSnapshot, error) {
	id, err := uuid.Parse(in.RequestID)
	if !validDispatchWorkflowInput(in.DispatchWorkflowInput) || err != nil || id.Version() != 7 || (in.Kind != "response" && in.Kind != "acceptance") {
		return DispatchSnapshot{}, temporal.NewNonRetryableApplicationError("invalid receiving deadline identifiers", "dispatch_input_invalid", nil)
	}
	out, err := p.ExpireReceiving(ctx, in)
	return dispatchActivityResult(out, err)
}
