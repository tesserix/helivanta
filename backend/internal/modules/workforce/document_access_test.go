package workforce_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports its own module
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"gorm.io/gorm"
)

func TestDocumentDownloadRequiresVerifiedReleaseAndCurrentOwnerAuthorization(t *testing.T) {
	s := newDocumentScenario(t)
	response := testutil.Do(s.router, "GET", s.path+"/content", "manager", "")
	require.Equal(t, 409, response.Code, response.Body.String())
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	response = testutil.Do(s.router, "GET", s.path+"/content", "manager", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, s.content, response.Body.Bytes())
	require.Equal(t, "private, no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.Contains(t, response.Header().Get("Content-Disposition"), "attachment;")
	require.Equal(t, "image/png", response.Header().Get("Content-Type"))
	response = testutil.Do(s.router, "GET", s.path+"/content", "foreign", "")
	require.Equal(t, 404, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "evidence.png")
	base := "/v1/workforce/hospitals/" + s.input.HospitalID
	require.Equal(t, 204, testutil.Do(s.router, "DELETE", base+"/access/user-manager", "manager", "").Code)
	response = testutil.Do(s.router, "GET", s.path+"/content", "manager", "")
	require.Equal(t, 404, response.Code, response.Body.String())
}

func TestDocumentRevocationIsIdempotentAndStopsDownloads(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	_, err = processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	for range 2 {
		response := testutil.Do(s.router, "PUT", s.path+"/revocation", "manager", "")
		require.Equal(t, 200, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), `"status":"revoked"`)
	}
	response := testutil.Do(s.router, "GET", s.path+"/content", "manager", "")
	require.Equal(t, 409, response.Code, response.Body.String())
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentChanged).Count(&count).Error)
		require.EqualValues(t, 2, count, "availability and exactly one revocation event")
		return nil
	}))
	response = testutil.Do(s.router, "PUT", s.path+"/revocation", "foreign", "")
	require.Equal(t, 404, response.Code, response.Body.String())
}

func TestDocumentManualRetryUsesNewAttemptAndReturnsSameReceipt(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: ERROR\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.Error(t, err)
	require.Equal(t, "scan_error", status)
	for i, expected := range []int{202, 200} {
		response := testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "")
		require.Equal(t, expected, response.Code, response.Body.String())
		var receipt map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
		require.Equal(t, float64(2), receipt["scan_attempt"])
		require.Equal(t, "scan_pending", receipt["status"])
		require.Nil(t, receipt["failure_code"], "retry clears the old verdict, delivery %d", i)
	}
	status, err = processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "superseded", status)
	response := testutil.Do(s.router, "PUT", s.path+"/retries/3", "manager", "")
	require.Equal(t, 409, response.Code, response.Body.String())
	response = testutil.Do(s.router, "PUT", s.path+"/retries/2", "foreign", "")
	require.Equal(t, 404, response.Code, response.Body.String())
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentScanRequested).Count(&count).Error)
		require.EqualValues(t, 2, count, "initial request and one manual retry")
		return nil
	}))
}

func TestDocumentDownloadReauthorizesAfterStorageRead(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	_, err = processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	s.cloud.AfterReleasedRead(func() {
		base := "/v1/workforce/hospitals/" + s.input.HospitalID
		require.Equal(t, 204, testutil.Do(s.router, "DELETE", base+"/access/user-manager", "manager", "").Code)
	})
	response := testutil.Do(s.router, "GET", s.path+"/content", "manager", "")
	require.Equal(t, 404, response.Code, response.Body.String())
	require.NotEqual(t, s.content, response.Body.Bytes())
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("workforce_audit").Where("action='workforce.document.downloaded'").Count(&count).Error)
		require.Zero(t, count, "no successful download audit when the final authorization denies")
		return nil
	}))
}
