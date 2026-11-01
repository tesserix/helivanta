package globalapi

import (
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func holdActivationSupervision(t *testing.T, db *tenantdb.DB, hospital string) (int, func()) {
	t.Helper()
	ready := make(chan int, 1)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, testutil.TenantA, hospital).Error; err != nil {
				return err
			}
			var pid int
			if err := tx.Raw(`SELECT pg_backend_pid()`).Scan(&pid).Error; err != nil {
				return err
			}
			ready <- pid
			select {
			case <-release:
				return nil
			case <-t.Context().Done():
				return t.Context().Err()
			}
		})
	}()
	unlock := sync.OnceFunc(func() {
		close(release)
		if err := <-done; err != nil && !t.Failed() {
			require.NoError(t, err)
		}
	})
	t.Cleanup(unlock)
	select {
	case pid := <-ready:
		return pid, unlock
	case err := <-done:
		// Retain the result for cleanup, including failures before lock acquisition.
		done <- err
		t.Fatalf("supervision lock holder: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("supervision lock holder did not become ready")
	}
	return 0, unlock
}

func awaitActivationBlock(t *testing.T, db *tenantdb.DB, blocker int, query string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var pid int
		err := db.WithSystem(t.Context(), func(tx *gorm.DB) error {
			return tx.Raw(`SELECT pid FROM pg_stat_activity WHERE ?=ANY(pg_blocking_pids(pid)) AND query LIKE ? LIMIT 1`, blocker, "%"+query+"%").Scan(&pid).Error
		})
		require.NoError(t, err)
		if pid != 0 {
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("request did not reach PostgreSQL lock boundary %q behind PID %d", query, blocker)
	return 0
}

func startActivationRequest(t *testing.T, request func(string, string, string, []byte, string) *httptest.ResponseRecorder, method, path, subject string, input any) <-chan *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	require.NoError(t, err)
	result := make(chan *httptest.ResponseRecorder, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result <- request(method, path, subject, body, "application/json")
	}()
	t.Cleanup(func() {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("request did not stop before race fixture cleanup")
		}
	})
	return result
}

func finishActivationRequest(t *testing.T, result <-chan *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	select {
	case response := <-result:
		require.Equal(t, status, response.Code, response.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &out))
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("request did not finish after releasing the lock")
	}
	return nil
}
