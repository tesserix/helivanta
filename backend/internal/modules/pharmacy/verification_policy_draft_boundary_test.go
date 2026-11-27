package pharmacy

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
)

const draftTestPath = "/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000001/verification-policy-drafts/0199e000-0000-7000-8000-000000000002/revisions/0199e000-0000-7000-8000-000000000003"

func TestVerificationPolicyDraftRejectsChangedAccountBeforeStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &catalogueHandlers{}
	for _, endpoint := range []struct {
		name, method, suffix string
		handler              gin.HandlerFunc
	}{
		{"write", "PUT", "/revisions/:revision", h.putVerificationPolicyDraft},
		{"recover", "GET", "/revisions/:revision", h.getVerificationPolicyDraft},
		{"history", "GET", "/revisions", func(c *gin.Context) { _, _ = h.listVerificationPolicyDraftHistory(c, pagination.Params{}) }},
		{"list", "GET", "", func(c *gin.Context) { _, _ = h.listVerificationPolicyDrafts(c, pagination.Params{}) }},
	} {
		for _, expected := range [][]string{{"another-person"}, {""}, {"verified-person", "verified-person"}, {"verified-person,another-person"}, {" verified-person"}} {
			t.Run(endpoint.name+"/"+strings.Join(expected, ":"), func(t *testing.T) {
				r := gin.New()
				r.Use(gin.Recovery(), func(c *gin.Context) {
					c.Set("authn.principal", authn.Principal{TenantID: "11111111-1111-4111-8111-111111111111", Subject: "verified-person"})
				})
				r.Handle(endpoint.method, "/pharmacies/:pharmacy/outlets/:outlet/verification-policy-drafts/:policy"+endpoint.suffix, endpoint.handler)
				path := draftTestPath
				if endpoint.name == "history" {
					path = path[:strings.LastIndex(path, "/")]
				}
				if endpoint.name == "list" {
					path = strings.Split(path, "/revisions/")[0]
				}
				req := httptest.NewRequest(endpoint.method, path, strings.NewReader(`{}`))
				req.Header["X-Helivanta-Expected-Subject"] = expected
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				require.Equal(t, 409, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), `"session_changed"`)
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			})
		}
	}
}

func TestVerificationPolicyDraftRejectsClientAuthorityBeforeStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const valid = `"expected_version":0,"label":"Synthetic policy","second_check_required":true,"review_valid_for_seconds":600,"query_response_target_seconds":1200,"reason":"Synthetic proposal"`
	for _, extra := range []string{`"clinical_authority":"approved"`, `"approver_subject":"someone"`, `"verified_person_id":"someone"`, `"emergency_supply":true`, `"state":"active"`, `"tenant_id":"11111111-1111-4111-8111-111111111111"`} {
		t.Run(extra, func(t *testing.T) {
			r := gin.New()
			r.Use(gin.Recovery(), func(c *gin.Context) {
				c.Set("authn.principal", authn.Principal{TenantID: "11111111-1111-4111-8111-111111111111", Subject: "verified-person"})
			})
			r.PUT("/pharmacies/:pharmacy/outlets/:outlet/verification-policy-drafts/:policy/revisions/:revision", (&catalogueHandlers{}).putVerificationPolicyDraft)
			req := httptest.NewRequest("PUT", draftTestPath, strings.NewReader("{"+valid+","+extra+"}"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
}
