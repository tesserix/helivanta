package emergency_test

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
	"strings"
	"sync"
	"testing"
)

func TestCaseBaseIsolationAndRevocationAreStructural(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	createBase(t, f)
	id := uuid.Must(uuid.NewV7()).String()
	path := f.path() + "/cases/" + id
	w := testutil.Do(f.r, "PUT", path, "dispatcher", intakeBody())
	require.Equal(t, 202, w.Code, w.Body.String())
	for _, who := range []string{"other", "admin"} {
		w = testutil.Do(f.r, "GET", path, who, "")
		require.Equal(t, 404, w.Code, w.Body.String())
	}
	otherBase := uuid.Must(uuid.NewV7()).String()
	otherPath := "/v1/emergency/bases/" + otherBase
	w = testutil.Do(f.r, "PUT", otherPath, "admin", baseBody())
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", otherPath+"/access/user-dispatcher", "admin", `{"level":"dispatcher"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "GET", otherPath+"/cases/"+id, "dispatcher", "")
	require.Equal(t, 404, w.Code)
	var count int64
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return tx.Raw(`SELECT count(*) FROM emergency_cases`).Scan(&count).Error }))
	require.Zero(t, count, "tenant scope alone must not expose case data")
	w = testutil.Do(f.r, "DELETE", f.path()+"/access/user-dispatcher", "admin", "")
	require.Equal(t, 204, w.Code)
	w = testutil.Do(f.r, "GET", path, "dispatcher", "")
	require.Equal(t, 404, w.Code)
	w = testutil.Do(f.r, "PUT", f.path()+"/cases/"+uuid.Must(uuid.NewV7()).String(), "dispatcher", intakeBody())
	require.Equal(t, 404, w.Code)
}
func TestConcurrentIntakeRetriesCommitOnlyOneTimelineAndEvent(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	createBase(t, f)
	id := uuid.Must(uuid.NewV7()).String()
	path := f.path() + "/cases/" + id
	const n = 8
	results := make(chan int, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() { results <- testutil.Do(f.r, "PUT", path, "dispatcher", intakeBody()).Code })
	}
	wg.Wait()
	close(results)
	for code := range results {
		require.Equal(t, 202, code)
	}
	var cases, history, outbox int64
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-dispatcher',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT count(*) FROM emergency_cases`).Scan(&cases).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT count(*) FROM emergency_case_timeline`).Scan(&history).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.emergency.case_received.v1'`).Scan(&outbox).Error
	}))
	require.EqualValues(t, 1, cases)
	require.EqualValues(t, 1, history)
	require.EqualValues(t, 1, outbox)
}

func TestClientCaseIDCollisionDoesNotRevealAnotherBase(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	createBase(t, f)
	id := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(f.r, "PUT", f.path()+"/cases/"+id, "dispatcher", intakeBody())
	require.Equal(t, 202, w.Code, w.Body.String())
	other := "/v1/emergency/bases/" + uuid.Must(uuid.NewV7()).String()
	w = testutil.Do(f.r, "PUT", other, "admin", baseBody())
	require.Equal(t, 200, w.Code)
	w = testutil.Do(f.r, "PUT", other+"/access/user-supervisor", "admin", `{"level":"supervisor"}`)
	require.Equal(t, 200, w.Code)
	w = testutil.Do(f.r, "DELETE", f.path()+"/access/user-supervisor", "admin", "")
	require.Equal(t, 204, w.Code)
	w = testutil.Do(f.r, "GET", f.path()+"/cases/"+id, "supervisor", "")
	require.Equal(t, 404, w.Code)
	w = testutil.Do(f.r, "PUT", other+"/cases/"+id, "supervisor", strings.Replace(intakeBody(), "Synthetic Caller", "Synthetic Other Caller", 1))
	require.Equal(t, 202, w.Code, w.Body.String())
	w = testutil.Do(f.r, "GET", other+"/cases/"+id, "supervisor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "Synthetic Other Caller")
	w = testutil.Do(f.r, "GET", f.path()+"/cases/"+id, "dispatcher", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "Synthetic Caller")
	require.NotContains(t, w.Body.String(), "Synthetic Other Caller")
}
func TestIntakeRefusesUnknownFieldsAndFailsClosedWithoutTransit(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	path := f.path() + "/cases/" + uuid.Must(uuid.NewV7()).String()
	for _, body := range []string{`{"tenant_id":"foreign"}`, intakeBody() + intakeBody(), `{"channel":"phone","requested_service":"unknown","processing_basis":"medical_emergency","incident":{"patient_description":"` + string(make([]byte, 9000)) + `"}}`} {
		w := testutil.Do(f.r, "PUT", path, "dispatcher", body)
		require.Equal(t, 400, w.Code)
	}
	w := testutil.Do(f.r, "PUT", path, "dispatcher", intakeBody())
	require.Equal(t, 503, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "Synthetic")
	w = testutil.Do(f.r, "PUT", path, "admin", intakeBody())
	require.Equal(t, 404, w.Code, "authorize object before probing encryption")
	w = testutil.Do(f.r, "PUT", path, "reader", intakeBody())
	require.Equal(t, 403, w.Code)
}
func TestCaseHistoryCannotBeRewrittenOrRemoved(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	createBase(t, f)
	id := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(f.r, "PUT", f.path()+"/cases/"+id, "dispatcher", intakeBody())
	require.Equal(t, 202, w.Code, w.Body.String())
	for _, sql := range []string{`INSERT INTO emergency_case_timeline(tenant_id,base_id,case_id,id,version,state,actor_subject) SELECT tenant_id,base_id,id,'01990000-0000-7000-8000-000000000099'::uuid,2,'closed','user-dispatcher' FROM emergency_cases`, `UPDATE emergency_case_timeline SET state='closed'`, `DELETE FROM emergency_case_timeline`, `TRUNCATE emergency_case_timeline`, `DELETE FROM emergency_cases`, `TRUNCATE emergency_cases`, `UPDATE emergency_cases SET incident_ciphertext='vault:v1:forged'`, `UPDATE emergency_audit SET action='forged'`} {
		err := f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-dispatcher',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
				return err
			}
			return tx.Exec(sql).Error
		})
		require.Error(t, err, sql)
	}
}
