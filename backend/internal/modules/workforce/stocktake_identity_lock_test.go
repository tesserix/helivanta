package workforce_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func TestHospitalStocktakeActorSerializesLegacyLoginMutation(t *testing.T) {
	f := newLoginLinkFixture(t)
	ctx := t.Context()
	hospital := strings.TrimPrefix(f.base, "/v1/workforce/hospitals/")
	ready, released, done := make(chan int, 1), make(chan struct{}), make(chan error, 1)
	unlock := sync.OnceFunc(func() { close(released) })
	go func() {
		done <- f.db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, testutil.TenantA, hospital).Error; err != nil {
				return err
			}
			var pid int
			if err := tx.Raw(`SELECT pg_backend_pid()`).Scan(&pid).Error; err != nil {
				return err
			}
			ready <- pid
			<-released
			return nil
		})
	}()
	t.Cleanup(unlock)
	var holder int
	select {
	case holder = <-ready:
	case err := <-done:
		t.Fatalf("source lock failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("source lock was not acquired")
	}
	type response struct {
		code int
		body string
	}
	result := make(chan response, 1)
	go func() {
		reply := testutil.Do(f.r, http.MethodPut, f.path+"/"+uuid.Must(uuid.NewV7()).String(), "manager", `{"subject":"user-candidate","reason":"Synthetic alias introduction must wait for retained count review"}`)
		result <- response{reply.Code, reply.Body.String()}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		require.NoError(t, f.db.WithSystem(ctx, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND ?=ANY(pg_blocking_pids(pid)) AND query LIKE '%workforce_lock_supervision%')`, holder).Scan(&waiting).Error
		}))
		return waiting
	}, 2*time.Second, 10*time.Millisecond, "legacy link writes must wait at the source lock, before acquiring Staff/link rows")
	select {
	case early := <-result:
		t.Fatalf("identity mutation completed while source proof was held: %d %s", early.code, early.body)
	default:
	}
	unlock()
	require.NoError(t, <-done)
	select {
	case reply := <-result:
		require.Equal(t, http.StatusOK, reply.code, reply.body)
	case <-time.After(5 * time.Second):
		t.Fatal("identity mutation did not finish after the source lock was released")
	}
}
