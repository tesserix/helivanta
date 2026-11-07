package supply

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/workloadidentity"
)

type tradeClaimCallerFunc func(context.Context, string) error

func (f tradeClaimCallerFunc) Verify(ctx context.Context, token string) error { return f(ctx, token) }
func (tradeClaimCallerFunc) Subject() string                                  { return "system:serviceaccount:helivanta:helivanta-api" }

func TestTradeClaimCallerRejectsMixedBrowserCredentialsBeforeTokenReview(t *testing.T) {
	for name, configure := range map[string]func(*http.Request){
		"missing caller":              func(r *http.Request) { r.Header.Del(suppliertrade.SourceCallerHeader) },
		"duplicate caller":            func(r *http.Request) { r.Header.Add(suppliertrade.SourceCallerHeader, "Bearer second") },
		"normal authorization":        func(r *http.Request) { r.Header.Set("Authorization", "Bearer browser") },
		"normal cookie":               func(r *http.Request) { r.Header.Set("Cookie", "session=browser") },
		"hidden second authorization": func(r *http.Request) { r.Header["Authorization"] = []string{"", "Bearer browser"} },
		"hidden second cookie":        func(r *http.Request) { r.Header["Cookie"] = []string{"", "session=browser"} },
		"query credentials":           func(r *http.Request) { r.URL.RawQuery = "access_token=browser" },
		"wrong content type":          func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, suppliertrade.RelationshipCheckPath, nil)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set(suppliertrade.SourceCallerHeader, "Bearer TEST-WORKLOAD")
			configure(c.Request)
			ctx, cancel, ok := tradeCallerContext(c, tradeClaimCallerFunc(func(context.Context, string) error {
				t.Error("mixed credentials must be rejected before TokenReview")
				return nil
			}))
			if cancel != nil {
				cancel()
			}
			require.False(t, ok)
			require.Nil(t, ctx)
			require.Equal(t, http.StatusUnauthorized, c.Writer.Status())
		})
	}
}

func TestTradeClaimCallerBoundsVerificationAndSeparatesDenialFromUnavailable(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
	}{
		"accepted":        {nil, http.StatusOK},
		"rejected":        {workloadidentity.ErrRejected, http.StatusUnauthorized},
		"dependency down": {errors.New("TokenReview unavailable"), http.StatusServiceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, suppliertrade.RelationshipCheckPath, nil)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set(suppliertrade.SourceCallerHeader, "Bearer TEST-WORKLOAD")
			ctx, cancel, ok := tradeCallerContext(c, tradeClaimCallerFunc(func(ctx context.Context, token string) error {
				require.Equal(t, "Bearer TEST-WORKLOAD", token)
				deadline, exists := ctx.Deadline()
				require.True(t, exists)
				require.WithinDuration(t, time.Now().Add(3*time.Second), deadline, 100*time.Millisecond)
				return tc.err
			}))
			require.Equal(t, tc.err == nil, ok)
			require.Equal(t, tc.status, c.Writer.Status())
			if ok {
				cancel()
				require.ErrorIs(t, ctx.Err(), context.Canceled)
			}
		})
	}
}
