package pharmacy

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
)

type stocktakeScopeVerifier struct{}

func (stocktakeScopeVerifier) Verify(context.Context, string) (authn.Principal, error) {
	return authn.Principal{Subject: "TEST-actor", TenantID: "0199e000-0000-7000-8000-000000000001", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}, nil
}

type stocktakeScopeRevocation struct{}

func (stocktakeScopeRevocation) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}

func TestStocktakeSubjectPreconditionPrecedesResourceAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		subjects []string
		status   int
	}{
		{"legacy missing precondition", nil, 404},
		{"current actor", []string{"TEST-actor"}, 404},
		{"other account in same tenant", []string{"TEST-other-actor"}, 409},
		{"blank precondition", []string{""}, 409},
		{"ambiguous precondition", []string{"TEST-actor", "TEST-actor"}, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(authn.Middleware(stocktakeScopeVerifier{}, stocktakeScopeRevocation{}))
			h := catalogueHandlers{}
			r.GET("/pharmacies/:pharmacy/outlets/:outlet/stocktakes/:stocktake", h.getStocktake)
			request := httptest.NewRequest("GET", "/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000002/stocktakes/not-a-count", nil)
			request.Header.Set("Authorization", "Bearer TEST")
			for _, subject := range tc.subjects {
				request.Header.Add("X-Helivanta-Expected-Subject", subject)
			}
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
		})
	}
}
