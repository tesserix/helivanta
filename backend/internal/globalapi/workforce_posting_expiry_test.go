package globalapi

import (
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertPostingSaveStopsAtReviewedCutoff(t *testing.T, db *tenantdb.DB, request func(string, string, string, []byte, string) *httptest.ResponseRecorder, command postingRaceRequest, until time.Time) {
	t.Helper()
	lockHospital := uuid.Must(uuid.NewV7()).String()
	sql := fmt.Sprintf(`CREATE FUNCTION wait_posting_expiry_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='workforce.posting_draft.saved' THEN PERFORM workforce_lock_supervision(NEW.tenant_id,'%s'::uuid); END IF; RETURN NEW; END $$; CREATE TRIGGER wait_posting_expiry_audit BEFORE INSERT ON workforce_audit FOR EACH ROW EXECUTE FUNCTION wait_posting_expiry_audit()`, lockHospital)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_posting_expiry_wait_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	clear := sync.OnceFunc(func() {
		require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_posting_expiry_clear_" + uuid.Must(uuid.NewV7()).String(), SQL: `DROP TRIGGER wait_posting_expiry_audit ON workforce_audit; DROP FUNCTION wait_posting_expiry_audit()`}}))
	})
	t.Cleanup(clear)
	// Observe the persisted business cutoff: a Go-only clock cannot advance PostgreSQL clock_timestamp().
	delay := time.Until(until.Add(-4 * time.Second))
	require.Positive(t, delay, "the approved source must still be effective before the expiry probe")
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal("source expiry fixture was cancelled")
	}
	blocker, unlock := holdActivationSupervision(t, db, lockHospital)
	saving := startActivationRequest(t, request, command.method, command.path, command.subject, command.input)
	awaitActivationBlock(t, db, blocker, "INSERT INTO workforce_audit")
	denied := finishActivationRequest(t, saving, 403)
	require.NotContains(t, denied, "id", "an expired command must disclose no saved receipt")
	var observed time.Time
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT clock_timestamp()`).Scan(&observed).Error }))
	require.False(t, observed.Before(until), "the request reached the real source boundary while its save audit was blocked")
	unlock()
	clear()
	var saved int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("workforce_audit").Where("action='workforce.posting_draft.saved' AND actor_subject=?", command.subject).Count(&saved).Error
	}))
	require.Zero(t, saved, "expired work must roll back its pending save audit")
}
