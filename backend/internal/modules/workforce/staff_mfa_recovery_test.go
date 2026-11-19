package workforce

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
)

type mfaTransportVerifier struct{ p authn.Principal }

func (v mfaTransportVerifier) Verify(context.Context, string) (authn.Principal, error) {
	return v.p, nil
}

type mfaTransportRevocation struct{}

func (mfaTransportRevocation) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}

func TestStaffMFARecoveryTransportBoundsIntentAndAccountSelection(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	const command = "019f39c2-1738-7998-b5de-6d4214f614e0"
	const valid = `{"commandId":"` + command + `","expectedLinkId":"019f39c2-1738-7998-b5de-6d4214f614e1","expectedLinkVersion":2,"expectedStaffVersion":1,"reason":"Confirmed lost authenticator"}`
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(authn.Middleware(mfaTransportVerifier{authn.Principal{TenantID: tenant, Subject: "admin-user", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}}, mfaTransportRevocation{}))
	h := &handlers{}
	base := "/workforce/hospitals/:hospital/staff/:id/mfa-recoveries/:command"
	router.PUT(base, h.resetStaffMFA)
	router.GET(base, h.readStaffMFA)
	router.POST(base+"/cancel", h.cancelStaffMFA)
	path := "/workforce/hospitals/11111111-1111-4111-8111-111111111112/staff/11111111-1111-4111-8111-111111111113/mfa-recoveries/" + command
	for _, tc := range []struct {
		name, method, body, suffix, media, subject string
		want                                       int
	}{
		{"valid_unavailable", "PUT", valid, "", "application/json", "admin-user", 503},
		{"foreign_subject_field", "PUT", strings.TrimSuffix(valid, "}") + `,"subject":"other-user"}`, "", "application/json", "admin-user", 400},
		{"duplicate_version", "PUT", strings.Replace(valid, `"expectedLinkVersion":2`, `"expectedLinkVersion":1,"expectedLinkVersion":2`, 1), "", "application/json", "admin-user", 400},
		{"changed_command", "PUT", strings.Replace(valid, command, "019f39c2-1738-7998-b5de-6d4214f614e2", 1), "", "application/json", "admin-user", 400},
		{"oversize", "PUT", valid + strings.Repeat(" ", 4097), "", "application/json", "admin-user", 400},
		{"query", "PUT", valid, "?target=other", "application/json", "admin-user", 400},
		{"empty_query", "PUT", valid, "?", "application/json", "admin-user", 400},
		{"changed_actor", "PUT", valid, "", "application/json", "other-user", 409},
		{"missing_actor", "PUT", valid, "", "application/json", "", 409},
		{"non_json", "PUT", valid, "", "text/plain", "admin-user", 415},
		{"status_body", "GET", `{}`, "", "", "admin-user", 400},
		{"cancel_null", "POST", `null`, "/cancel", "application/json", "admin-user", 400},
		{"cancel_target", "POST", `{"subject":"other-user"}`, "/cancel", "application/json", "admin-user", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, path+tc.suffix, strings.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer synthetic-session")
			request.Header.Set("X-Helivanta-Expected-Tenant", tenant)
			if tc.subject != "" {
				request.Header.Set("X-Helivanta-Expected-Subject", tc.subject)
			}
			if tc.media != "" {
				request.Header.Set("Content-Type", tc.media)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tc.want, response.Code, response.Body.String())
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.Empty(t, response.Result().Cookies())
		})
	}
}

func TestStaffMFAErrorRequiresNewAuthenticationWhenTheActingSessionExpires(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("authn.principal", authn.Principal{TenantID: "11111111-1111-4111-8111-111111111111", Subject: "admin-user", IdleDeadline: time.Now().Add(-time.Second)})
	staffMFAError(c, context.DeadlineExceeded)
	require.Equal(t, 401, c.Writer.Status())
}
