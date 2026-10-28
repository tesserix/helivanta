package privacy_test

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/privacy" //nolint:depguard // module under test
	privacycontract "github.com/tesserix/helivanta/internal/modules/privacy/contract"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestDeadlineProcessorRequiresRestrictedIdentityAndCannotReadCaseContent(t *testing.T) {
	r, db, _, _ := deadlineFixture(t, true)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/privacy/requests/" + id
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "patient", `{"kind":"access","description":"private synthetic content"}`).Code)
	_, err := privacy.NewDeadlineProcessor(t.Context(), db)
	require.Error(t, err)
	var database string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw("SELECT current_database()").Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.PrivacyWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	processor, err := privacy.NewDeadlineProcessor(t.Context(), worker)
	require.NoError(t, err)
	in := privacycontract.DeadlineWorkflowInput{TenantID: testutil.TenantA, RequestID: id}
	snapshot, err := processor.Refresh(t.Context(), in)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Version)
	require.False(t, snapshot.ObservedAt.IsZero())
	require.False(t, snapshot.Terminal)
	for _, query := range []string{"SELECT description FROM privacy_requests", "SELECT subject,contact_email FROM privacy_officers", "SET ROLE hms_app", "SET ROLE hms_owner"} {
		require.Error(t, worker.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec(query).Error }), query)
	}
	in.TenantID = testutil.TenantB
	_, err = processor.Refresh(t.Context(), in)
	require.Error(t, err)
}

func deadlineProcessor(t *testing.T, db *tenantdb.DB) *privacy.DeadlineProcessor {
	t.Helper()
	var database string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw("SELECT current_database()").Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.PrivacyWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	processor, err := privacy.NewDeadlineProcessor(t.Context(), worker)
	require.NoError(t, err)
	return processor
}

func TestDeadlineExpiryIsDurableIdempotentAndNeverResolvesCase(t *testing.T) {
	r, db, ctx, _ := deadlineFixture(t, true)
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	setDeadlineClock(t, db, start)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/privacy/requests/" + id
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "patient", `{"kind":"access","description":"Synthetic request"}`).Code)
	processor := deadlineProcessor(t, db)
	input := privacy.DeadlineExpiryInput{DeadlineWorkflowInput: privacycontract.DeadlineWorkflowInput{TenantID: testutil.TenantA, RequestID: id}, Kind: "acknowledgement"}
	early, err := processor.Expire(t.Context(), input)
	require.NoError(t, err)
	require.False(t, early.AcknowledgementEscalated)
	setDeadlineClock(t, db, start.Add(time.Hour))
	for range 2 {
		expired, err := processor.Expire(t.Context(), input)
		require.NoError(t, err)
		require.True(t, expired.AcknowledgementEscalated)
		require.False(t, expired.Terminal)
	}
	require.Contains(t, testutil.Do(r, "GET", path, "patient", "").Body.String(), `"status":"received"`)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.actor_subject','user-patient',true)").Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Table("privacy_deadline_escalations").Where("tenant_id=? AND request_id=? AND kind='acknowledgement'", testutil.TenantA, id).Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, count)
		return nil
	}))
}

func TestMissingEscalationOfficerRemainsVisibleWithoutResolvingCase(t *testing.T) {
	r, db, ctx, _ := deadlineFixture(t, true)
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	setDeadlineClock(t, db, start)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/privacy/requests/" + id
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "patient", `{"kind":"access","description":"Synthetic request"}`).Code)
	var officerID string
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw("SELECT id::text FROM privacy_officers WHERE tenant_id=? AND role='escalation' AND status='escalation_active'", testutil.TenantA).Scan(&officerID).Error
	}))
	revoked := testutil.Do(r, "POST", "/v1/privacy/officers/"+officerID+"/revoke", "admin", fmt.Sprintf(`{"expected_version":2,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String()))
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	setDeadlineClock(t, db, start.Add(time.Hour))
	_, err := deadlineProcessor(t, db).Expire(t.Context(), privacy.DeadlineExpiryInput{DeadlineWorkflowInput: privacycontract.DeadlineWorkflowInput{TenantID: testutil.TenantA, RequestID: id}, Kind: "acknowledgement"})
	require.NoError(t, err)
	result := testutil.Do(r, "GET", path+"/deadline", "patient", "")
	require.Equal(t, 200, result.Code, result.Body.String())
	require.Contains(t, result.Body.String(), `"routing":"unconfigured"`)
	require.Contains(t, result.Body.String(), `"kind":"acknowledgement"`)
	require.Contains(t, result.Body.String(), `"officer_id":null`)
	require.Contains(t, testutil.Do(r, "GET", path, "patient", "").Body.String(), `"status":"received"`)
}
