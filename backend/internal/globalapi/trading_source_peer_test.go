package globalapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/workloadidentity"
)

type tradingWorkloadFixture struct{}

func (tradingWorkloadFixture) Verify(_ context.Context, credential string) error {
	if credential != "Bearer TEST-CURRENT-OWNER" {
		return workloadidentity.ErrRejected
	}
	return nil
}
func (tradingWorkloadFixture) Subject() string {
	return "system:serviceaccount:helivanta:helivanta-api"
}

type tradingOwnerTransport struct{ owner http.Handler }

func (transport tradingOwnerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response := httptest.NewRecorder()
	transport.owner.ServeHTTP(response, request)
	return response.Result(), nil
}

type observedTradingOwnerTransport struct {
	tradingOwnerTransport
	t *testing.T
}

func (transport observedTradingOwnerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.tradingOwnerTransport.RoundTrip(request)
	if err != nil {
		transport.t.Logf("current-owner protocol: path=%s error=%v", request.URL.Path, err)
	} else {
		transport.t.Logf("current-owner protocol: path=%s status=%d", request.URL.Path, response.StatusCode)
	}
	return response, err
}

func tradingSourcePeerForTest(t *testing.T, deps platform.Deps) *suppliertrade.CurrentSourceClient {
	t.Helper()
	owner := gin.New()
	sourceDeps := deps
	sourceDeps.TradingEndpoints = observedTradingEndpoints{CurrentEndpointGuard: deps.TradingEndpoints, t: t}
	owner.POST(suppliertrade.SourceCheckPath, supply.NewTradingSourceCheckHandler(sourceDeps, tradingWorkloadFixture{}))
	owner.POST(suppliertrade.RelationshipCheckPath, supply.NewRelationshipCheckHandler(deps, tradingWorkloadFixture{}))
	client, err := suppliertrade.NewCurrentSourceClient("https://current-owner.example.invalid"+suppliertrade.SourceCheckPath, observedTradingOwnerTransport{tradingOwnerTransport: tradingOwnerTransport{owner}, t: t}, func(context.Context) (string, error) { return "TEST-CURRENT-OWNER", nil })
	require.NoError(t, err)
	return client
}
