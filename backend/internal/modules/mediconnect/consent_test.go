package mediconnect_test

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/mediconnect" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTenantReviewedNoticeIsImmutableAndScoped(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"publisher": testutil.TenantA, "patient": testutil.TenantA, "other": testutil.TenantB}, Perms: map[string][]authz.Permission{"publisher": {"mediconnect.notice.manage"}}, Modules: []platform.Module{mediconnect.New()}})
	path := "/v1/mediconnect/notices/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"purpose":"communications","locale":"hi-IN","version_label":"test-v1","title":"Synthetic notice","text":"केवल परीक्षण — no real legal policy.","review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 403, testutil.Do(r, "PUT", path, "patient", body).Code)
	created := testutil.Do(r, "PUT", path, "publisher", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	var notice map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &notice))
	require.Len(t, notice["content_hash"], 64)
	require.Equal(t, "communications", notice["purpose"])
	require.Equal(t, "hi-IN", notice["locale"])
	require.Equal(t, "केवल परीक्षण — no real legal policy.", notice["text"])
	require.Equal(t, "published", notice["status"])
	require.NotContains(t, created.Body.String(), "review_reference")
	require.NotContains(t, created.Body.String(), "publisher_subject")
	require.JSONEq(t, created.Body.String(), testutil.Do(r, "PUT", path, "publisher", body).Body.String())
	require.JSONEq(t, created.Body.String(), testutil.Do(r, "GET", path, "patient", "").Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", path, "other", "").Code)
	require.Equal(t, "no-store", created.Header().Get("Cache-Control"))
}

func TestAccountConsentPinsNoticeAndWithdrawalIsTerminal(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"publisher": testutil.TenantA, "patient": testutil.TenantA, "peer": testutil.TenantA, "other": testutil.TenantB}, Perms: map[string][]authz.Permission{"publisher": {"mediconnect.notice.manage"}}, Modules: []platform.Module{mediconnect.New()}})
	upstream := httptest.NewServer(r)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"mediconnect": upstream.URL}})
	require.NoError(t, err)
	forwarded := gin.New()
	forwarded.NoRoute(func(c *gin.Context) {
		c.Request.Header.Set("X-Tenant-ID", testutil.TenantB)
		c.Request.Header.Set("X-User-ID", "forged")
		gateway.ServeHTTP(c.Writer, c.Request)
	})
	r = forwarded

	noticeID := uuid.Must(uuid.NewV7()).String()
	noticePath := "/v1/mediconnect/notices/" + noticeID
	content := fmt.Sprintf(`{"purpose":"communications","locale":"hi-IN","version_label":"test-v1","title":"Synthetic notice","text":"केवल परीक्षण — no real legal policy.","review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	published := testutil.Do(r, "PUT", noticePath, "publisher", content)
	require.Equal(t, 201, published.Code, published.Body.String())
	var notice map[string]any
	require.NoError(t, json.Unmarshal(published.Body.Bytes(), &notice))
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "patient", "{}").Code)
	path := "/v1/mediconnect/account/consents/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"notice_id":%q,"content_hash":%q,"accepted":true}`, noticeID, notice["content_hash"])
	created := testutil.Do(r, "PUT", path, "patient", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	require.Contains(t, created.Body.String(), `"status":"active"`)
	require.Contains(t, created.Body.String(), `"locale":"hi-IN"`)
	require.Contains(t, created.Body.String(), notice["content_hash"])
	require.NotContains(t, created.Body.String(), "user-patient")
	for _, token := range []string{"publisher", "peer", "other"} {
		require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", token, "{}").Code)
		require.Equal(t, 404, testutil.Do(r, "GET", path, token, "").Code)
	}
	replay := testutil.Do(r, "PUT", path, "patient", body)
	require.Equal(t, 200, replay.Code)
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	withdrawn := testutil.Do(r, "POST", path+"/withdraw", "patient", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	require.Contains(t, withdrawn.Body.String(), `"status":"withdrawn"`)
	require.JSONEq(t, withdrawn.Body.String(), testutil.Do(r, "POST", path+"/withdraw", "patient", `{"expected_version":1}`).Body.String())
	require.JSONEq(t, withdrawn.Body.String(), testutil.Do(r, "PUT", path, "patient", body).Body.String())
	require.Contains(t, testutil.Do(r, "GET", "/v1/mediconnect/account/consents", "patient", "").Body.String(), `"status":"withdrawn"`)
}

func TestConsentResponseAuditFailureRollsBackReceiptAndEvent(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"publisher": testutil.TenantA, "patient": testutil.TenantA}, Perms: map[string][]authz.Permission{"publisher": {"mediconnect.notice.manage"}}, Modules: []platform.Module{mediconnect.New()}})
	id := uuid.Must(uuid.NewV7()).String()
	published := testutil.Do(r, "PUT", "/v1/mediconnect/notices/"+id, "publisher", fmt.Sprintf(`{"purpose":"communications","locale":"en-IN","version_label":"test-v1","title":"Synthetic notice","text":"Only a test.","review_reference":%q}`, uuid.Must(uuid.NewV7()).String()))
	require.Equal(t, 201, published.Code)
	var notice map[string]any
	require.NoError(t, json.Unmarshal(published.Body.Bytes(), &notice))
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "patient", "{}").Code)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_consent_read_failure", SQL: `CREATE FUNCTION reject_consent_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_type='mediconnect_account_consents' AND NEW.action='read' THEN RAISE EXCEPTION 'synthetic read audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_consent_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_consent_read();`}}))
	failed := testutil.Do(r, "PUT", "/v1/mediconnect/account/consents/"+uuid.Must(uuid.NewV7()).String(), "patient", fmt.Sprintf(`{"notice_id":%q,"content_hash":%q,"accepted":true}`, id, notice["content_hash"]))
	require.Equal(t, 500, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "synthetic")
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-patient',true)`).Error; err != nil {
			return err
		}
		var receipts, events int64
		if err := tx.Table("mediconnect_account_consents").Count(&receipts).Error; err != nil {
			return err
		}
		if err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.mediconnect.account_consent_recorded.v1").Count(&events).Error; err != nil {
			return err
		}
		require.Zero(t, receipts)
		require.Zero(t, events)
		return nil
	}))
}

func TestRetiredNoticeRejectsNewConsentAndPreservesHistory(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"publisher": testutil.TenantA, "patient": testutil.TenantA}, Perms: map[string][]authz.Permission{"publisher": {"mediconnect.notice.manage"}}, Modules: []platform.Module{mediconnect.New()}})
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/mediconnect/notices/" + id
	content := fmt.Sprintf(`{"purpose":"analytics","locale":"en-IN","version_label":"test-v1","title":"Synthetic notice","text":"Only a test.","review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	published := testutil.Do(r, "PUT", path, "publisher", content)
	require.Equal(t, 201, published.Code)
	var notice map[string]any
	require.NoError(t, json.Unmarshal(published.Body.Bytes(), &notice))
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "patient", "{}").Code)
	receiptPath := "/v1/mediconnect/account/consents/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"notice_id":%q,"content_hash":%q,"accepted":true}`, id, notice["content_hash"])
	require.Equal(t, 201, testutil.Do(r, "PUT", receiptPath, "patient", body).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "publisher", strings.Replace(content, "Only a test.", "Changed text.", 1)).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", "/v1/mediconnect/notices/"+uuid.Must(uuid.NewV7()).String(), "publisher", content).Code)
	require.Equal(t, 403, testutil.Do(r, "POST", path+"/retire", "patient", `{"expected_version":1}`).Code)
	require.Equal(t, 409, testutil.Do(r, "POST", path+"/retire", "publisher", `{"expected_version":2}`).Code)
	retired := testutil.Do(r, "POST", path+"/retire", "publisher", `{"expected_version":1}`)
	require.Equal(t, 200, retired.Code, retired.Body.String())
	require.Contains(t, retired.Body.String(), `"status":"retired"`)
	require.JSONEq(t, retired.Body.String(), testutil.Do(r, "POST", path+"/retire", "publisher", `{"expected_version":1}`).Body.String())
	require.JSONEq(t, retired.Body.String(), testutil.Do(r, "PUT", path, "publisher", content).Body.String())
	current := testutil.Do(r, "PUT", receiptPath, "patient", body)
	require.Equal(t, 200, current.Code, current.Body.String())
	require.Contains(t, current.Body.String(), `"notice_status":"retired"`)
	require.Equal(t, 409, testutil.Do(r, "PUT", "/v1/mediconnect/account/consents/"+uuid.Must(uuid.NewV7()).String(), "patient", body).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", receiptPath+"/withdraw", "patient", `{"expected_version":1}`).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", "/v1/mediconnect/account/consents/"+uuid.Must(uuid.NewV7()).String(), "patient", body).Code)
	require.NotContains(t, testutil.Do(r, "GET", "/v1/mediconnect/notices", "patient", "").Body.String(), id)
	require.Contains(t, testutil.Do(r, "GET", "/v1/mediconnect/notices?status=retired&purpose=analytics&locale=en-IN", "patient", "").Body.String(), id)
}

func TestConsentInvalidAndConcurrentCommandsCannotMultiplyAuthority(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"publisher": testutil.TenantA, "patient": testutil.TenantA}, Perms: map[string][]authz.Permission{"publisher": {"mediconnect.notice.manage"}}, Modules: []platform.Module{mediconnect.New()}})
	id := uuid.Must(uuid.NewV7()).String()
	noticePath := "/v1/mediconnect/notices/" + id
	content := fmt.Sprintf(`{"purpose":"account_processing","locale":"en-IN","version_label":"test-v1","title":"Synthetic notice","text":"Only a test.","review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	for _, bad := range []string{`null`, `{}`, content + ` {}`, strings.Replace(content, "account_processing", "clinical_disclosure", 1), strings.Replace(content, "en-IN", "not-a-locale-$$$$", 1), strings.Replace(content, "Only a test.", strings.Repeat("x", 8193), 1), strings.TrimSuffix(content, "}") + `,"tenant_id":"forged"}`} {
		response := testutil.Do(r, "PUT", noticePath, "publisher", bad)
		require.Equal(t, 400, response.Code, response.Body.String())
	}
	published := testutil.Do(r, "PUT", noticePath, "publisher", content)
	require.Equal(t, 201, published.Code)
	var notice map[string]any
	require.NoError(t, json.Unmarshal(published.Body.Bytes(), &notice))
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "patient", "{}").Code)
	path := "/v1/mediconnect/account/consents/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"notice_id":%q,"content_hash":%q,"accepted":true}`, id, notice["content_hash"])
	for _, bad := range []string{`null`, `{}`, strings.Replace(body, "true", "false", 1), body + `{}`, strings.TrimSuffix(body, "}") + `,"account_id":"forged"}`} {
		response := testutil.Do(r, "PUT", path, "patient", bad)
		require.Equal(t, 400, response.Code, response.Body.String())
	}
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "patient", strings.Replace(body, notice["content_hash"].(string), strings.Repeat("0", 64), 1)).Code)
	results := make(chan int, 8)
	for range 8 {
		go func() { results <- testutil.Do(r, "PUT", path, "patient", body).Code }()
	}
	created := 0
	for range 8 {
		code := <-results
		if code == 201 {
			created++
		} else {
			require.Equal(t, 200, code)
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, 409, testutil.Do(r, "PUT", "/v1/mediconnect/account/consents/"+uuid.Must(uuid.NewV7()).String(), "patient", body).Code)
	for range 8 {
		go func() { results <- testutil.Do(r, "POST", path+"/withdraw", "patient", `{"expected_version":1}`).Code }()
	}
	for range 8 {
		require.Equal(t, 200, <-results)
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		for _, subject := range []string{"helivanta.in.mediconnect.account_consent_recorded.v1", "helivanta.in.mediconnect.account_consent_withdrawn.v1"} {
			var count int64
			if err := tx.Table("outbox_events").Where("subject=?", subject).Count(&count).Error; err != nil {
				return err
			}
			require.EqualValues(t, 1, count)
		}
		return nil
	}))
	require.Equal(t, 201, testutil.Do(r, "PUT", "/v1/mediconnect/account/consents/"+uuid.Must(uuid.NewV7()).String(), "patient", body).Code)
}

func TestConsentOutboxFailureDoesNotLoseWithdrawalOrInventReceipt(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"publisher": testutil.TenantA, "patient": testutil.TenantA}, Perms: map[string][]authz.Permission{"publisher": {"mediconnect.notice.manage"}}, Modules: []platform.Module{mediconnect.New()}})
	noticeID := uuid.Must(uuid.NewV7()).String()
	notice := testutil.Do(r, "PUT", "/v1/mediconnect/notices/"+noticeID, "publisher", fmt.Sprintf(`{"purpose":"communications","locale":"en-IN","version_label":"test-v1","title":"Synthetic notice","text":"Only a test.","review_reference":%q}`, uuid.Must(uuid.NewV7()).String()))
	require.Equal(t, 201, notice.Code)
	var published map[string]any
	require.NoError(t, json.Unmarshal(notice.Body.Bytes(), &published))
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "patient", "{}").Code)
	body := fmt.Sprintf(`{"notice_id":%q,"content_hash":%q,"accepted":true}`, noticeID, published["content_hash"])
	path := "/v1/mediconnect/account/consents/" + uuid.Must(uuid.NewV7()).String()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_consent_publish_failure", SQL: `CREATE FUNCTION reject_consent_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject LIKE 'helivanta.in.mediconnect.account_consent_%' THEN RAISE EXCEPTION 'synthetic consent event outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_consent_event BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_consent_event();`}}))
	failed := testutil.Do(r, "PUT", path, "patient", body)
	require.Equal(t, 500, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "synthetic")
	require.Equal(t, 404, testutil.Do(r, "GET", path, "patient", "").Code)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_consent_restore_publish", SQL: `DROP TRIGGER reject_consent_event ON outbox_events;`}}))
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "patient", body).Code)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_consent_withdraw_publish_failure", SQL: `CREATE TRIGGER reject_consent_event BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_consent_event();`}}))
	require.Equal(t, 500, testutil.Do(r, "POST", path+"/withdraw", "patient", `{"expected_version":1}`).Code)
	require.Contains(t, testutil.Do(r, "GET", path, "patient", "").Body.String(), `"status":"active"`)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_consent_restore_withdraw", SQL: `DROP TRIGGER reject_consent_event ON outbox_events;`}}))
	require.Equal(t, 200, testutil.Do(r, "POST", path+"/withdraw", "patient", `{"expected_version":1}`).Code)
	require.Contains(t, testutil.Do(r, "GET", path, "patient", "").Body.String(), `"status":"withdrawn"`)
}
