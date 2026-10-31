package workforce_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // External test imports its own module.
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"gorm.io/gorm"
)

func TestStaffDocumentRequestsRejectChangedExpectedAccount(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	var reads atomic.Int32
	s.cloud.AfterReleasedRead(func() { reads.Add(1) })
	staffPath := strings.TrimSuffix(s.path, "/documents/"+s.input.FileID)
	routes := []struct{ method, path string }{
		{"GET", staffPath + "/document-records"},
		{"GET", staffPath + "/documents"},
		{"PUT", s.path},
		{"PUT", s.path + "/content"},
		{"GET", s.path + "/content"},
		{"PUT", s.path + "/revocation"},
		{"PUT", s.path + "/retries/2"},
		{"GET", s.path + "/classification"},
		{"PUT", s.path + "/classification"},
		{"GET", s.path + "/classification/history"},
	}
	snapshot := func() string {
		t.Helper()
		var result string
		require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, s.input.HospitalID).Error; err != nil {
				return err
			}
			return tx.Raw(`SELECT jsonb_build_object('file',(SELECT to_jsonb(d) FROM workforce_documents d WHERE id=?),'classifications',(SELECT count(*) FROM workforce_document_classification_revisions),'audit',(SELECT count(*) FROM workforce_audit),'outbox',(SELECT count(*) FROM outbox_events))::text`, s.input.FileID).Scan(&result).Error
		}))
		require.Contains(t, result, `"status": "available"`)
		return result
	}
	before := snapshot()
	for _, tc := range []struct {
		name, header string
		values       []string
	}{
		{"changed_account", "X-Helivanta-Expected-Subject", []string{"synthetic-previous-account"}},
		{"empty_account", "X-Helivanta-Expected-Subject", []string{""}},
		{"duplicate_account", "X-Helivanta-Expected-Subject", []string{"user-manager", "user-manager"}},
		{"joined_account", "X-Helivanta-Expected-Subject", []string{"user-manager,user-manager"}},
		{"changed_tenant", "X-Helivanta-Expected-Tenant", []string{testutil.TenantB}},
		{"empty_tenant", "X-Helivanta-Expected-Tenant", []string{""}},
		{"malformed_tenant", "X-Helivanta-Expected-Tenant", []string{"invalid"}},
		{"zero_tenant", "X-Helivanta-Expected-Tenant", []string{uuid.Nil.String()}},
		{"duplicate_tenant", "X-Helivanta-Expected-Tenant", []string{testutil.TenantA, testutil.TenantA}},
		{"joined_tenant", "X-Helivanta-Expected-Tenant", []string{testutil.TenantA + "," + testutil.TenantA}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, route := range routes {
				request := httptest.NewRequest(route.method, route.path, strings.NewReader("invalid body must not be decoded"))
				request.Header.Set("Authorization", "Bearer manager")
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
				request.Header.Set("X-Helivanta-Expected-Subject", "user-manager")
				request.Header[tc.header] = tc.values
				response := httptest.NewRecorder()
				s.router.ServeHTTP(response, request)
				require.Equal(t, http.StatusConflict, response.Code, fmt.Sprintf("%s %s: %s", route.method, route.path, response.Body.String()))
				require.Contains(t, response.Body.String(), `"error":"session_changed"`)
				require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
				require.NotContains(t, response.Body.String(), "synthetic-evidence.png")
			}
		})
	}
	require.Equal(t, before, snapshot(), "workspace refusals must not change file, classification, audit or outbox")
	require.Zero(t, reads.Load(), "workspace refusal must precede released-byte retrieval")
	for _, headers := range []bool{false, true} {
		s.cloud.AfterReleasedRead(func() { reads.Add(1) })
		request := httptest.NewRequest("GET", s.path+"/content", nil)
		request.Header.Set("Authorization", "Bearer manager")
		if headers {
			request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
			request.Header.Set("X-Helivanta-Expected-Subject", "user-manager")
		}
		response := httptest.NewRecorder()
		s.router.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Equal(t, s.content, response.Body.Bytes())
	}
	require.EqualValues(t, 2, reads.Load())
	s.cloud.AfterReleasedRead(func() { reads.Add(1) })
	request := httptest.NewRequest("GET", s.path+"/content", nil)
	request.Header.Set("Authorization", "Bearer foreign")
	request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
	request.Header.Set("X-Helivanta-Expected-Subject", "user-foreign")
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
	require.EqualValues(t, 2, reads.Load())
}
