package mediconnect_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/mediconnect" //nolint:depguard // owner under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/workloadidentity"
	"gorm.io/gorm"
	authentication "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestCurrentClaimUsesLiveOwnerStateAfterWithdrawal(t *testing.T) {
	users, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	require.Equal(t, 201, testutil.Do(users, "POST", "/v1/mediconnect/account", "owner", "{}").Code)
	requestID := uuid.Must(uuid.NewV7()).String()
	hospital := uuid.Must(uuid.NewV7()).String()
	patient := uuid.Must(uuid.NewV7()).String()
	path := "/v1/mediconnect/account/link-requests/" + requestID
	input := fmt.Sprintf(`{"hospital_id":%q,"patient_id":%q,"relationship":"self"}`, hospital, patient)
	require.Equal(t, 202, testutil.Do(users, "PUT", path, "owner", input).Code)
	var kubeAvailable atomic.Bool
	kubeAvailable.Store(true)
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !kubeAvailable.Load() {
			w.WriteHeader(500)
			return
		}
		var review authentication.TokenReview
		require.NoError(t, json.NewDecoder(r.Body).Decode(&review))
		allowed := review.Spec.Token == "synthetic-projected-token" && len(review.Spec.Audiences) == 1 && review.Spec.Audiences[0] == "helivanta-mediconnect-claim"
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(authentication.TokenReview{TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "TokenReview"}, Status: authentication.TokenReviewStatus{Authenticated: allowed, Audiences: []string{"helivanta-mediconnect-claim"}, User: authentication.UserInfo{Username: "system:serviceaccount:helivanta:patientmaster-api", UID: "synthetic-source-pod"}}}))
	}))
	t.Cleanup(kube.Close)
	caller, err := workloadidentity.New(&rest.Config{Host: kube.URL}, "helivanta-mediconnect-claim", "system:serviceaccount:helivanta:patientmaster-api")
	require.NoError(t, err)
	owner := gin.New()
	owner.POST("/internal/v1/mediconnect/current-claim", mediconnect.NewClaimCheckHandler(platform.Deps{DB: db}, caller))
	body := fmt.Sprintf(`{"tenant_id":%q,"request_id":%q,"hospital_id":%q,"patient_id":%q,"subject":"user-owner"}`, testutil.TenantA, requestID, hospital, patient)

	send := func(payload string, headers map[string][]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/internal/v1/mediconnect/current-claim", strings.NewReader(payload))
		for name, values := range headers {
			for _, value := range values {
				req.Header.Add(name, value)
			}
		}
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		owner.ServeHTTP(response, req)
		return response
	}
	credential := map[string][]string{"X-MediConnect-Workload-Token": {"Bearer synthetic-projected-token"}}
	check := func() *httptest.ResponseRecorder { return send(body, credential) }
	current := check()
	require.Equal(t, 200, current.Code, current.Body.String())
	require.JSONEq(t, `{"current":true}`, current.Body.String())
	require.Equal(t, "no-store", current.Header().Get("Cache-Control"))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var actor string
		if err := tx.Raw(`SELECT actor_subject FROM audit_events WHERE tenant_id=? AND resource_id=? AND action='current_claim_checked'`, testutil.TenantA, requestID).Scan(&actor).Error; err != nil {
			return err
		}
		require.Equal(t, "system:serviceaccount:helivanta:patientmaster-api", actor)
		return nil
	}))
	for _, headers := range []map[string][]string{
		{},
		{"Authorization": {"Bearer synthetic-projected-token"}},
		{"X-MediConnect-Workload-Token": {"Bearer wrong-token"}},
		{"X-MediConnect-Workload-Token": {"Bearer synthetic-projected-token", "Bearer synthetic-projected-token"}},
		{"X-MediConnect-Workload-Token": {"Bearer synthetic-projected-token"}, "Authorization": {"Bearer arbitrary-user-token"}},
	} {
		refused := send(body, headers)
		require.Equal(t, 401, refused.Code, refused.Body.String())
		require.NotContains(t, refused.Body.String(), "user-owner")
	}
	for _, wrong := range []string{
		strings.Replace(body, testutil.TenantA, testutil.TenantB, 1),
		strings.Replace(body, requestID, uuid.Must(uuid.NewV7()).String(), 1),
		strings.Replace(body, hospital, uuid.Must(uuid.NewV7()).String(), 1),
		strings.Replace(body, patient, uuid.Must(uuid.NewV7()).String(), 1),
		strings.Replace(body, "user-owner", "user-peer", 1),
	} {
		missing := send(wrong, credential)
		require.Equal(t, 200, missing.Code, missing.Body.String())
		require.JSONEq(t, `{"current":false}`, missing.Body.String())
	}

	proxyID := uuid.Must(uuid.NewV7()).String()
	proxyInput := strings.Replace(input, `"self"`, `"child"`, 1)
	require.Equal(t, 202, testutil.Do(users, "PUT", "/v1/mediconnect/account/link-requests/"+proxyID, "owner", proxyInput).Code)
	proxy := send(strings.Replace(body, requestID, proxyID, 1), credential)
	require.Equal(t, 200, proxy.Code, proxy.Body.String())
	require.JSONEq(t, `{"current":false}`, proxy.Body.String())
	for _, invalid := range []string{
		"null", "{}", body + " {}",
		strings.TrimSuffix(body, "}") + `,"untrusted":true}`,
		strings.Replace(body, "user-owner", strings.Repeat("x", 513), 1),
		strings.Replace(body, "user-owner", string([]byte{0xff}), 1),
		strings.Replace(body, "user-owner", strings.Repeat("x", 5000), 1),
	} {
		require.Equal(t, 400, send(invalid, credential).Code)
	}

	withdrawn := testutil.Do(users, "POST", path+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	current = check()
	require.Equal(t, 200, current.Code, current.Body.String())
	require.JSONEq(t, `{"current":false}`, current.Body.String())

	activeID := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 202, testutil.Do(users, "PUT", "/v1/mediconnect/account/link-requests/"+activeID, "owner", input).Code)
	body = strings.Replace(body, requestID, activeID, 1)
	require.JSONEq(t, `{"current":true}`, check().Body.String())
	scope := func(tx *gorm.DB) error {
		return tx.Exec(`SELECT set_config('app.actor_subject','system:mediconnect-current-claim',true),set_config('app.mediconnect_claim_id',?,true),set_config('app.mediconnect_claim_subject','user-owner',true),set_config('app.mediconnect_claim_hospital',?,true),set_config('app.mediconnect_claim_patient',?,true)`, activeID, hospital, patient).Error
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := scope(tx); err != nil {
			return err
		}
		var count int64
		if err := tx.Table("mediconnect_link_requests").Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, count)
		return nil
	}))
	writeErr := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := scope(tx); err != nil {
			return err
		}
		return tx.Exec(`UPDATE mediconnect_link_requests SET status='withdrawn',version=2,withdrawn_at=clock_timestamp() WHERE tenant_id=? AND id=?`, testutil.TenantA, activeID).Error
	})
	var pgError *pgconn.PgError
	require.ErrorAs(t, writeErr, &pgError)
	require.Equal(t, "42501", pgError.Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_current_claim_audit_failure", SQL: `CREATE FUNCTION reject_current_claim_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='current_claim_checked' THEN RAISE EXCEPTION 'synthetic current claim audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_current_claim_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_current_claim_audit();`}}))
	failedAudit := check()
	require.Equal(t, 503, failedAudit.Code, failedAudit.Body.String())
	require.NotContains(t, failedAudit.Body.String(), `"current":true`)
	require.NotContains(t, failedAudit.Body.String(), "synthetic current claim audit outage")
	kubeAvailable.Store(false)
	unavailable := check()
	require.Equal(t, 503, unavailable.Code, unavailable.Body.String())
	require.NotContains(t, unavailable.Body.String(), `"current":true`)
}
