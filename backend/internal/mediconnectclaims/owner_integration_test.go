package mediconnectclaims_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/mediconnectclaims"
	"github.com/tesserix/helivanta/internal/modules/mediconnect" //nolint:depguard // real owner boundary under test
	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/workloadidentity"
	authentication "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestOwnerClientObservesCommittedWithdrawalAcrossActualOwnerHTTP(t *testing.T) {
	users, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	require.Equal(t, 201, testutil.Do(users, "POST", "/v1/mediconnect/account", "owner", "{}").Code)
	requestID, hospital, patient := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	path := "/v1/mediconnect/account/link-requests/" + requestID
	proposed := testutil.Do(users, "PUT", path, "owner", fmt.Sprintf(`{"hospital_id":%q,"patient_id":%q,"relationship":"self"}`, hospital, patient))
	require.Equal(t, 202, proposed.Code, proposed.Body.String())
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var review authentication.TokenReview
		if json.NewDecoder(r.Body).Decode(&review) != nil {
			w.WriteHeader(400)
			return
		}
		allowed := review.Spec.Token == "projected-owner-token" && len(review.Spec.Audiences) == 1 && review.Spec.Audiences[0] == mediconnectcontract.ClaimWorkloadAudience
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(authentication.TokenReview{TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "TokenReview"}, Status: authentication.TokenReviewStatus{Authenticated: allowed, Audiences: []string{mediconnectcontract.ClaimWorkloadAudience}, User: authentication.UserInfo{Username: "system:serviceaccount:helivanta:patientmaster-api", UID: "synthetic-service-account"}}})
	}))
	t.Cleanup(kube.Close)
	caller, err := workloadidentity.New(&rest.Config{Host: kube.URL}, mediconnectcontract.ClaimWorkloadAudience, "system:serviceaccount:helivanta:patientmaster-api")
	require.NoError(t, err)
	owner := gin.New()
	owner.POST(mediconnectcontract.CurrentClaimPath, mediconnect.NewClaimCheckHandler(platform.Deps{DB: db}, caller))
	server := httptest.NewTLSServer(owner)
	t.Cleanup(server.Close)
	token := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(token, []byte("projected-owner-token"), 0600))
	client, err := mediconnectclaims.New(server.URL, token, "", server.Client())
	require.NoError(t, err)
	input := mediconnectcontract.ClaimCheckRequest{TenantID: testutil.TenantA, RequestID: requestID, HospitalID: hospital, PatientID: patient, Subject: "user-owner"}
	current, err := client.Current(t.Context(), input)
	require.NoError(t, err)
	require.True(t, current)
	withdrawn := testutil.Do(users, "POST", path+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	current, err = client.Current(t.Context(), input)
	require.NoError(t, err)
	require.False(t, current)
	input.TenantID = testutil.TenantB
	current, err = client.Current(t.Context(), input)
	require.NoError(t, err)
	require.False(t, current)
}
