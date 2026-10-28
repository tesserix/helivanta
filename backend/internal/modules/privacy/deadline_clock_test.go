package privacy_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/privacy" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestCaseWithoutReviewedPolicyReportsUnmonitoredDeadline(t *testing.T) {
	r, _, _, _, path := caseFixture(t)
	for _, token := range []string{"patient", "officer"} {
		result := testutil.Do(r, "GET", path+"/deadline", token, "")
		require.Equal(t, 200, result.Code, result.Body.String())
		require.JSONEq(t, `{"monitoring":"unmonitored"}`, result.Body.String())
		require.Equal(t, "no-store", result.Header().Get("Cache-Control"))
	}
	for _, token := range []string{"admin", "peer", "other"} {
		require.Equal(t, 404, testutil.Do(r, "GET", path+"/deadline", token, "").Code)
	}
}

func deadlineFixture(t *testing.T, pause bool) (*gin.Engine, *tenantdb.DB, context.Context, string) {
	t.Helper()
	workflowClient := new(mocks.Client)
	workflowClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("synthetic Temporal outage")).Maybe()
	return deadlineFixtureWithClient(t, pause, workflowClient)
}
func deadlineFixtureWithClient(t *testing.T, pause bool, workflowClient client.Client) (*gin.Engine, *tenantdb.DB, context.Context, string) {
	t.Helper()
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{PrivacyDeadlinesEnabled: true, Workflows: workflowClient, Tokens: map[string]string{"admin": testutil.TenantA, "officer": testutil.TenantA, "dpo": testutil.TenantA, "patient": testutil.TenantA, "peer": testutil.TenantA, "other": testutil.TenantB}, Perms: map[string][]authz.Permission{"admin": {"privacy.officer.manage", "privacy.deadline.manage"}}, Modules: []platform.Module{privacy.New()}})
	for _, role := range []string{"primary", "escalation"} {
		user := "officer"
		if role == "escalation" {
			user = "dpo"
		}
		path := "/v1/privacy/officers/" + uuid.Must(uuid.NewV7()).String()
		body := fmt.Sprintf(`{"role":%q,"subject":%q,"display_name":"Synthetic Officer","contact_email":"privacy@example.invalid","review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"}`, role, "user-"+user, uuid.Must(uuid.NewV7()).String())
		require.Equal(t, 201, testutil.Do(r, "PUT", path, "admin", body).Code)
		require.Equal(t, 200, testutil.Do(r, "POST", path+"/accept", user, `{}`).Code)
	}
	policy := uuid.Must(uuid.NewV7()).String()
	path := "/v1/privacy/deadline-policies/" + policy
	body := fmt.Sprintf(`{"version_label":"synthetic-v1","acknowledgement_seconds":3600,"resolution_seconds":7200,"pause_while_waiting":%t,"review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"}`, pause, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "admin", body).Code)
	approved := testutil.Do(r, "POST", path+"/approve", "dpo", `{}`)
	require.Equal(t, 200, approved.Code, approved.Body.String())
	return r, db, ctx, policy
}

func TestCaseIntakePinsReviewedClockAndQueuesOnlyMetadata(t *testing.T) {
	r, db, ctx, policy := deadlineFixture(t, true)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/privacy/requests/" + id
	body := `{"kind":"access","description":"private synthetic request"}`
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "patient", body).Code)
	result := testutil.Do(r, "GET", path+"/deadline", "patient", "")
	require.Equal(t, 200, result.Code, result.Body.String())
	var clock struct {
		Monitoring    string
		PolicyID      string    `json:"policy_id"`
		StartedAt     time.Time `json:"started_at"`
		AcknowledgeBy time.Time `json:"acknowledge_by"`
		ResolveBy     time.Time `json:"resolve_by"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &clock))
	require.Equal(t, "pending", clock.Monitoring)
	require.Equal(t, policy, clock.PolicyID)
	require.Equal(t, time.Hour, clock.AcknowledgeBy.Sub(clock.StartedAt))
	require.Equal(t, 2*time.Hour, clock.ResolveBy.Sub(clock.StartedAt))
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "patient", body).Code)
	require.JSONEq(t, result.Body.String(), testutil.Do(r, "GET", path+"/deadline", "patient", "").Body.String())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var payloads []string
		if err := tx.Raw(`SELECT payload::text FROM outbox_events WHERE tenant_id=? AND subject='helivanta.in.privacy.deadline_changed.v1'`, testutil.TenantA).Scan(&payloads).Error; err != nil {
			return err
		}
		require.Len(t, payloads, 1)
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(payloads[0]), &envelope))
		require.Equal(t, map[string]any{"request_id": id, "version": float64(1)}, envelope.Data)
		require.NotContains(t, payloads[0], "private synthetic")
		return nil
	}))
}

func setDeadlineClock(t *testing.T, db *tenantdb.DB, at time.Time) {
	t.Helper()
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_privacy_clock_" + uuid.Must(uuid.NewV7()).String(), SQL: fmt.Sprintf(`CREATE OR REPLACE FUNCTION privacy_deadline_clock() RETURNS timestamptz LANGUAGE sql VOLATILE AS $$ SELECT '%s'::timestamptz $$`, at.UTC().Format(time.RFC3339Nano))}}))
}

func TestCaseDeadlinePauseAndResumePreserveElapsedTimeAndReplay(t *testing.T) {
	r, db, _, _ := deadlineFixture(t, true)
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	setDeadlineClock(t, db, start)
	path := "/v1/privacy/requests/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "patient", `{"kind":"access","description":"Synthetic request"}`).Code)
	type deadline struct {
		Version            int64
		AcknowledgedAt     *time.Time `json:"acknowledged_at"`
		PausedAt           *time.Time `json:"paused_at"`
		PausedMicroseconds int64      `json:"paused_microseconds"`
		ResolveBy          time.Time  `json:"resolve_by"`
		Terminal           bool
	}
	read := func() deadline {
		t.Helper()
		result := testutil.Do(r, "GET", path+"/deadline", "patient", "")
		require.Equal(t, 200, result.Code, result.Body.String())
		var out deadline
		require.NoError(t, json.Unmarshal(result.Body.Bytes(), &out))
		return out
	}
	run := func(version int, action, actor string) string {
		t.Helper()
		url := path + "/commands/" + uuid.Must(uuid.NewV7()).String()
		body := fmt.Sprintf(`{"expected_version":%d,"action":%q,"message":"Synthetic case action"}`, version, action)
		response := testutil.Do(r, "PUT", url, actor, body)
		require.Equal(t, 201, response.Code, response.Body.String())
		require.Equal(t, 200, testutil.Do(r, "PUT", url, actor, body).Code)
		return url
	}
	setDeadlineClock(t, db, start.Add(10*time.Minute))
	run(1, "start_review", "officer")
	first := read()
	require.EqualValues(t, 2, first.Version)
	require.NotNil(t, first.AcknowledgedAt)
	require.True(t, first.AcknowledgedAt.Equal(start.Add(10*time.Minute)))
	setDeadlineClock(t, db, start.Add(20*time.Minute))
	run(2, "request_information", "officer")
	paused := read()
	require.NotNil(t, paused.PausedAt)
	require.True(t, paused.PausedAt.Equal(start.Add(20*time.Minute)))
	setDeadlineClock(t, db, start.Add(70*time.Minute))
	run(3, "reply", "patient")
	resumed := read()
	require.Nil(t, resumed.PausedAt)
	require.Equal(t, (50 * time.Minute).Microseconds(), resumed.PausedMicroseconds)
	require.True(t, resumed.ResolveBy.Equal(start.Add(170*time.Minute)))
	setDeadlineClock(t, db, start.Add(80*time.Minute))
	run(4, "respond", "officer")
	terminal := read()
	require.True(t, terminal.Terminal)
	require.EqualValues(t, 5, terminal.Version)
	require.Equal(t, resumed.PausedMicroseconds, terminal.PausedMicroseconds)
}

func TestDeadlineOutboxFailureRollsBackCaseAndClock(t *testing.T) {
	r, db, ctx, _ := deadlineFixture(t, true)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_privacy_outbox_failure", SQL: `CREATE FUNCTION test_privacy_outbox_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='helivanta.in.privacy.deadline_changed.v1' THEN RAISE EXCEPTION 'synthetic outbox failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_privacy_outbox_failure BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION test_privacy_outbox_failure();`}}))
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/privacy/requests/" + id
	failed := testutil.Do(r, "PUT", path, "patient", `{"kind":"access","description":"Synthetic request"}`)
	require.Equal(t, 500, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "synthetic outbox failure")
	require.Equal(t, 404, testutil.Do(r, "GET", path, "patient", "").Code)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.actor_subject','user-patient',true)").Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Table("privacy_case_deadlines").Where("tenant_id=? AND id=?", testutil.TenantA, id).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		if err := tx.Table("outbox_events").Where("tenant_id=? AND subject='helivanta.in.privacy.deadline_changed.v1'", testutil.TenantA).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		return nil
	}))
}
