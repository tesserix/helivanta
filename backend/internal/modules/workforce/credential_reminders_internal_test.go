package workforce

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/events"
)

func reminderDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	require.NoError(t, err)
	return d
}

func TestPlanReminderWritesOnlyTheSmallestDueThreshold(t *testing.T) {
	until := reminderDay(t, "2026-12-31")
	cases := []struct {
		name, today string
		thresholds  []int
		write       *int
		next        string
	}{
		{"nothing due yet", "2026-10-01", []int{60, 30, 0}, nil, "2026-11-01"},
		{"first threshold on its date", "2026-11-01", []int{60, 30, 0}, new(60), "2026-12-01"},
		{"late approval with several due", "2026-12-15", []int{60, 30, 7, 0}, new(30), "2026-12-24"},
		{"expiry day", "2026-12-31", []int{60, 30, 0}, new(0), ""},
		{"after expiry", "2027-01-05", []int{60, 30, 0}, new(0), ""},
		{"policy without zero", "2027-01-05", []int{30, 7}, new(7), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan := planReminder(c.thresholds, until, reminderDay(t, c.today))
			require.Equal(t, c.write, plan.write)
			if c.next == "" {
				require.Nil(t, plan.next)
			} else {
				require.Equal(t, reminderDay(t, c.next), *plan.next)
			}
		})
	}
	leap := planReminder([]int{365, 1}, reminderDay(t, "2028-02-29"), reminderDay(t, "2027-01-01"))
	require.Nil(t, leap.write)
	require.Equal(t, reminderDay(t, "2027-03-01"), *leap.next, "a leap-day expiry counts whole days back")
}

func TestReminderWakeIsTheLocalMidnightCappedAtADay(t *testing.T) {
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	auckland, err := time.LoadLocation("Pacific/Auckland")
	require.NoError(t, err)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	tomorrow := reminderDay(t, "2026-10-19")
	require.Equal(t, time.Date(2026, 10, 18, 18, 30, 0, 0, time.UTC), reminderWake(&tomorrow, kolkata, now).UTC(), "Kolkata midnight is 18:30 UTC")
	sameDay := reminderDay(t, "2026-10-19")
	require.Equal(t, time.Date(2026, 10, 18, 11, 0, 0, 0, time.UTC), reminderWake(&sameDay, auckland, now.Add(-2*time.Hour)).UTC(), "Auckland midnight in daylight time is 11:00 UTC the day before")
	far := reminderDay(t, "2026-11-30")
	require.Equal(t, now.Add(24*time.Hour), reminderWake(&far, kolkata, now), "a distant threshold still wakes within a day")
	require.Equal(t, now.Add(24*time.Hour), reminderWake(nil, kolkata, now))
}

func TestReminderThresholdArraysRoundTrip(t *testing.T) {
	values, err := parseIntArray(intArrayLiteral([]int{365, 30, 0}))
	require.NoError(t, err)
	require.Equal(t, []int{365, 30, 0}, values)
	empty, err := parseIntArray("{}")
	require.NoError(t, err)
	require.Empty(t, empty)
	_, err = parseIntArray("{1,x}")
	require.Error(t, err)
}

func reminderInput() workforcecontract.CredentialReminderInput {
	return workforcecontract.CredentialReminderInput{TenantID: uuid.NewString(), HospitalID: uuid.NewString(), StaffID: uuid.NewString(), CredentialID: uuid.NewString(), Version: 1}
}

func TestReminderEventInputAcceptsOnlyTheValidityContract(t *testing.T) {
	in := reminderInput()
	data := `{"hospital_id":"` + in.HospitalID + `","staff_id":"` + in.StaffID + `","credential_id":"` + in.CredentialID + `","review_id":"` + uuid.NewString() + `","version":1}`
	got, err := reminderEventInput(events.Event{Type: CredentialValidityChanged, Version: 1, TenantID: in.TenantID, Data: []byte(data)})
	require.NoError(t, err)
	require.Equal(t, in, got)
	id, err := CredentialReminderWorkflowID(got)
	require.NoError(t, err)
	require.Equal(t, "wf-cred-rem-"+in.TenantID+"-"+in.HospitalID+"-"+in.StaffID+"-"+in.CredentialID+"-v1", id)
	for name, evt := range map[string]events.Event{
		"wrong subject":   {Type: DocumentChanged, Version: 1, TenantID: in.TenantID, Data: []byte(data)},
		"wrong version":   {Type: CredentialValidityChanged, Version: 2, TenantID: in.TenantID, Data: []byte(data)},
		"cross tenant":    {Type: CredentialValidityChanged, Version: 1, TenantID: in.TenantID, DestinationTenantID: uuid.NewString(), Data: []byte(data)},
		"unknown field":   {Type: CredentialValidityChanged, Version: 1, TenantID: in.TenantID, Data: []byte(data[:len(data)-1] + `,"number":"x"}`)},
		"trailing object": {Type: CredentialValidityChanged, Version: 1, TenantID: in.TenantID, Data: []byte(data + `{}`)},
		"bad tenant":      {Type: CredentialValidityChanged, Version: 1, TenantID: "tenant", Data: []byte(data)},
		"zero version":    {Type: CredentialValidityChanged, Version: 1, TenantID: in.TenantID, Data: []byte(`{"hospital_id":"` + in.HospitalID + `","staff_id":"` + in.StaffID + `","credential_id":"` + in.CredentialID + `","review_id":"` + uuid.NewString() + `","version":0}`)},
	} {
		_, err := reminderEventInput(evt)
		require.Error(t, err, name)
	}
}

type reminderAdvance func(context.Context, workforcecontract.CredentialReminderInput) (workforcecontract.CredentialReminderStep, error)

// runReminderWorkflow hands the fake activity the environment's skipped clock.
func runReminderWorkflow(t *testing.T, advance func(now func() time.Time) reminderAdvance) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(CredentialReminderWorkflow, workflow.RegisterOptions{Name: CredentialReminderWorkflowName})
	env.RegisterActivityWithOptions(advance(env.Now), activity.RegisterOptions{Name: CredentialReminderActivityName})
	env.ExecuteWorkflow(CredentialReminderWorkflowName, reminderInput())
	require.True(t, env.IsWorkflowCompleted())
	return env
}

func TestReminderWorkflowSleepsUntilEachWakeAndStopsWhenDone(t *testing.T) {
	var calls []time.Time
	var started time.Time
	env := runReminderWorkflow(t, func(clock func() time.Time) reminderAdvance {
		return func(context.Context, workforcecontract.CredentialReminderInput) (workforcecontract.CredentialReminderStep, error) {
			now := clock()
			if started.IsZero() {
				started = now
			}
			calls = append(calls, now)
			switch len(calls) {
			case 1:
				return workforcecontract.CredentialReminderStep{WakeAt: now.Add(3 * time.Hour)}, nil
			case 2:
				return workforcecontract.CredentialReminderStep{WakeAt: now.Add(-time.Hour)}, nil
			default:
				return workforcecontract.CredentialReminderStep{Done: true}, nil
			}
		}
	})
	require.NoError(t, env.GetWorkflowError())
	require.Len(t, calls, 3)
	require.WithinDuration(t, started.Add(3*time.Hour), calls[1], time.Second, "sleeps until the wake instant")
	require.WithinDuration(t, calls[1].Add(time.Minute), calls[2], time.Second, "a past wake still waits a minute")
}

func TestReminderWorkflowContinuesAsNewAfterBoundedCycles(t *testing.T) {
	calls := 0
	env := runReminderWorkflow(t, func(clock func() time.Time) reminderAdvance {
		return func(context.Context, workforcecontract.CredentialReminderInput) (workforcecontract.CredentialReminderStep, error) {
			calls++
			return workforcecontract.CredentialReminderStep{WakeAt: clock().Add(24 * time.Hour)}, nil
		}
	})
	var next *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &next)
	require.Equal(t, CredentialReminderWorkflowName, next.WorkflowType.Name)
	require.Equal(t, credentialReminderCycles, calls)
}

func TestReminderWorkflowExhaustsBoundedRetriesVisibly(t *testing.T) {
	calls := 0
	env := runReminderWorkflow(t, func(func() time.Time) reminderAdvance {
		return func(context.Context, workforcecontract.CredentialReminderInput) (workforcecontract.CredentialReminderStep, error) {
			calls++
			return workforcecontract.CredentialReminderStep{}, errors.New("credential reminder unavailable")
		}
	})
	require.Error(t, env.GetWorkflowError())
	require.Contains(t, env.GetWorkflowError().Error(), "credential reminder unavailable")
	require.Equal(t, 10, calls)
}

func TestReminderWorkflowRejectsInvalidInput(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(CredentialReminderWorkflow, workforcecontract.CredentialReminderInput{TenantID: "x"})
	require.ErrorContains(t, env.GetWorkflowError(), "credential reminder input invalid")
}
