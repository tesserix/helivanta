package customeronboarding

import (
	"net/http"
	"net/http/httptest"
	"time"
)

// NewOwnerIntegrationHandlers is compiled only for tests; production retains
// the fixed private endpoint and token path enforced by New.
func NewOwnerIntegrationHandlers(tokenFile string, owner http.Handler) *Handlers {
	return &Handlers{origin: "https://helivanta.app", base: "http://onboarding-api.onboarding.svc.cluster.local:8080", tokenFile: tokenFile, client: &http.Client{Timeout: 30 * time.Second, Transport: ownerIntegrationTransport{owner}}}
}

type ownerIntegrationTransport struct{ owner http.Handler }

func (t ownerIntegrationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response := httptest.NewRecorder()
	t.owner.ServeHTTP(response, request)
	return response.Result(), nil
}
