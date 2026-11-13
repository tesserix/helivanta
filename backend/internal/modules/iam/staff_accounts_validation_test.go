package iam

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
)

func TestStaffAccountTransportRefusesAuthorityFieldsAndChangedWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := uuid.NewString()
	owner := "TEST-owner"
	id := uuid.Must(uuid.NewV7()).String()
	valid := "{\"email\":\"synthetic@example.invalid\",\"given_name\":\"Synthetic\",\"family_name\":\"Staff\",\"reason\":\"Authorized account\"}"
	h := &staffAccountHandlers{}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{TenantID: tenant, Subject: owner, AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)})
	})
	router.PUT("/commands/:id", h.create)
	for _, tc := range []struct {
		name, body, expectedTenant, expectedSubject string
		want                                        int
	}{
		{"valid unavailable", valid, tenant, owner, 503},
		{"forged actor", strings.Replace(valid, "{", "{\"actor_subject\":\"forged\",", 1), tenant, owner, 400},
		{"duplicate email", strings.Replace(valid, "{", "{\"email\":\"other@example.invalid\",", 1), tenant, owner, 400},
		{"case alias", strings.Replace(valid, "email", "Email", 1), tenant, owner, 400},
		{"foreign tenant", valid, uuid.NewString(), owner, 409},
		{"changed owner", valid, tenant, "other-owner", 409},
		{"missing account assertion", valid, tenant, "", 409},
		{"oversize", strings.Repeat(" ", 4097) + valid, tenant, owner, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/commands/"+id, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Helivanta-Expected-Tenant", tc.expectedTenant)
			if tc.expectedSubject != "" {
				request.Header.Set("X-Helivanta-Expected-Subject", tc.expectedSubject)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", response.Code, tc.want, response.Body)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("account response is cacheable")
			}
		})
	}
}
