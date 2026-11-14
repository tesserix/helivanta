package customeronboarding

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

func TestBusinessProfilePassesThePublicReceiptBoundaryAndInvalidAddressIsRejected(t *testing.T) {
	var calls atomic.Int32
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/internal/v1/customer-signups", r.URL.Path)
		assert.Equal(t, "Bearer synthetic-workload", r.Header.Get("Authorization"))
		var in customercontract.Signup
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&in)) {
			w.WriteHeader(400)
			return
		}
		if !assert.NotNil(t, in.Profile) {
			w.WriteHeader(400)
			return
		}
		assert.Equal(t, "office@example.test", in.Profile.Contact.Email)
		assert.Equal(t, "10 Headquarters Road", in.Profile.HeadOffice.Line1)
		assert.Equal(t, "20 Branch Road", in.Profile.MainBranch.Address.Line1)
		assert.Equal(t, customercontract.Odisha, in.Profile.MainBranch.Address.StateCode)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"provisioning"}`))
	}))
	t.Cleanup(private.Close)
	path := t.TempDir() + "/token"
	require.NoError(t, os.WriteFile(path, []byte("synthetic-workload"), 0600))
	h := &Handlers{origin: "https://helivanta.app", base: private.URL, tokenFile: path, client: private.Client()}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/v1/auth/signup", h.Start)
	body := `{"name":"TEST Hospital","kind":"hospital","countryCode":"IN","stateCode":"MH","owner":{"givenName":"Test","familyName":"Owner","email":"owner@example.test"},"profile":{"contact":{"phone":"+919876543210","email":"office@example.test"},"headOffice":{"line1":"10 Headquarters Road","line2":"","city":"Pune","district":"Pune","postalCode":"411001","stateCode":"MH","countryCode":"IN"},"mainBranch":{"name":"Test Main Branch","address":{"line1":"20 Branch Road","line2":"","city":"Bhubaneswar","district":"Khordha","postalCode":"751001","stateCode":"OD","countryCode":"IN"}}}}`
	submit := func(raw string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", strings.NewReader(raw))
		req.Header.Set("Origin", "https://helivanta.app")
		req.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		return result.Code
	}
	require.Equal(t, 202, submit(body))
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, 400, submit(strings.Replace(body, "751001", "000000", 1)))
	require.Equal(t, int32(1), calls.Load(), "invalid branch address crossed into provisioning")
}
