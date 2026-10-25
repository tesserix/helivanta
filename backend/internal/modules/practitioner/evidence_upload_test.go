package practitioner_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"image"
	"image/png"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestEvidenceUploadQuarantinesExactBytesAndAtomicallyQueuesOneScan(t *testing.T) {
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	r, db, collection, _ := evidenceFixtureWithFiles(t, files)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	data := buf.Bytes()
	id := uuid.Must(uuid.NewV7()).String()
	path := collection + "/" + id
	body := fmt.Sprintf(`{"content_type":"image/png","size":%d,"sha256":"%x"}`, len(data), sha256.Sum256(data))
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", body).Code)
	upload := func(token string, content []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", path+"/content", bytes.NewReader(content))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "image/png")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	first := upload("evidence-only", data)
	require.Equal(t, 202, first.Code, first.Body.String())
	require.Contains(t, first.Body.String(), `"status":"scan_pending"`)
	require.NotContains(t, first.Body.String(), "source_generation")
	replay := upload("steward", data)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	require.Equal(t, 400, upload("steward", []byte("wrong bytes")).Code)
	require.Equal(t, 404, upload("foreign", data).Code)
	require.Zero(t, cloud.CopyCount(), "upload cannot publish an unscanned object")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var events []struct {
			Subject string
			Payload json.RawMessage
		}
		if err := tx.Table("outbox_events").Select("subject,payload").Where("subject=?", "helivanta.in.practitioner.evidence_scan_requested.v1").Find(&events).Error; err != nil {
			return err
		}
		require.Len(t, events, 1)
		require.Contains(t, string(events[0].Payload), id)
		require.NotContains(t, string(events[0].Payload), "sha256")
		require.NotContains(t, string(events[0].Payload), "image/png")
		return nil
	}))
}

type evidenceMembership struct{ denied, unavailable atomic.Bool }

func (m *evidenceMembership) IsMember(context.Context, string, string) (bool, error) {
	if m.unavailable.Load() {
		return false, errors.New("synthetic membership outage")
	}
	return !m.denied.Load(), nil
}
func TestEvidenceUploadRechecksAuthorityAfterQuarantineAndReconcilesRetainedBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail func(*evidenceMembership)
		want int
	}{{"revoked", func(m *evidenceMembership) { m.denied.Store(true) }, 403}, {"membership_outage", func(m *evidenceMembership) { m.unavailable.Store(true) }, 503}} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := testinfra.NewGCSProtocol(t)
			files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
			require.NoError(t, err)
			membership := &evidenceMembership{}
			r, db, collection, _ := evidenceFixtureWithAccess(t, files, membership)
			var buf bytes.Buffer
			require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))))
			data := buf.Bytes()
			path := collection + "/" + uuid.Must(uuid.NewV7()).String()
			require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", fmt.Sprintf(`{"content_type":"image/png","size":%d,"sha256":"%x"}`, len(data), sha256.Sum256(data))).Code)
			upload := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest("PUT", path+"/content", bytes.NewReader(data))
				req.Header.Set("Authorization", "Bearer steward")
				req.Header.Set("Content-Type", "image/png")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w
			}
			cloud.AfterQuarantineWrite(func() { tc.fail(membership) })
			failed := upload()
			require.Equal(t, tc.want, failed.Code, failed.Body.String())
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				var n int64
				err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.practitioner.evidence_scan_requested.v1").Count(&n).Error
				require.Zero(t, n)
				return err
			}))
			membership.denied.Store(false)
			membership.unavailable.Store(false)
			cloud.AfterQuarantineWrite(nil)
			pending := testutil.Do(r, "GET", path, "steward", "")
			require.Equal(t, 200, pending.Code)
			require.Contains(t, pending.Body.String(), `"status":"upload_pending"`)
			retry := upload()
			require.Equal(t, 202, retry.Code, retry.Body.String())
			require.Zero(t, cloud.CopyCount())
			var before, after struct {
				UploadedAt string `json:"uploaded_at"`
			}
			require.NoError(t, json.Unmarshal(pending.Body.Bytes(), &before))
			require.NoError(t, json.Unmarshal(retry.Body.Bytes(), &after))
			require.NotEmpty(t, before.UploadedAt)
			require.Equal(t, before.UploadedAt, after.UploadedAt)
		})
	}
}
func TestEvidenceUploadOutboxFailureRollsBackReceiptAndExpiredIntentsStayBlocked(t *testing.T) {
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	r, db, collection, _ := evidenceFixtureWithFiles(t, files)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	data := buf.Bytes()
	path := collection + "/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", fmt.Sprintf(`{"content_type":"image/png","size":%d,"sha256":"%x"}`, len(data), sha256.Sum256(data))).Code)
	upload := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", path+"/content", bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer steward")
		req.Header.Set("Content-Type", "image/png")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_evidence_outbox_failure", SQL: `CREATE FUNCTION reject_evidence_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='helivanta.in.practitioner.evidence_scan_requested.v1' THEN RAISE EXCEPTION 'synthetic outbox unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_evidence_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_evidence_outbox();`}}))
	failed := upload()
	require.Equal(t, 500, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "synthetic")
	pending := testutil.Do(r, "GET", path, "steward", "")
	require.Equal(t, 200, pending.Code)
	require.Contains(t, pending.Body.String(), `"status":"upload_pending"`)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_evidence_outbox_recovery", SQL: `DROP TRIGGER reject_evidence_outbox ON outbox_events;`}}))
	require.Equal(t, 202, upload().Code)
	expired := collection + "/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", expired, "steward", fmt.Sprintf(`{"content_type":"image/png","size":%d,"sha256":"%x"}`, len(data), sha256.Sum256(data))).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_evidence_expiry_clock", SQL: `CREATE OR REPLACE FUNCTION practitioner_registry_now() RETURNS timestamptz LANGUAGE sql VOLATILE AS $$ SELECT clock_timestamp()+interval '2 hours' $$;`}}))
	path = expired
	require.Equal(t, 409, upload().Code)
}
