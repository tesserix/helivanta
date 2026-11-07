package supply_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // external test exercises its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/workloadidentity"
)

type relationshipProtocolCaller struct{}

func (relationshipProtocolCaller) Verify(_ context.Context, value string) error {
	if value != "Bearer TEST-WORKLOAD" {
		return workloadidentity.ErrRejected
	}
	return nil
}
func (relationshipProtocolCaller) Subject() string {
	return "system:serviceaccount:helivanta:helivanta-api"
}

type relationshipProtocolTransport struct{ handler http.Handler }

func (r relationshipProtocolTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	r.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
}

func relationshipCheckerForTest(t *testing.T, db *tenantdb.DB) *suppliertrade.CurrentSourceClient {
	t.Helper()
	router := gin.New()
	router.POST(suppliertrade.RelationshipCheckPath, supply.NewRelationshipCheckHandler(platform.Deps{DB: db}, relationshipProtocolCaller{}))
	client, err := suppliertrade.NewCurrentSourceClient("https://source.example.invalid"+suppliertrade.SourceCheckPath, relationshipProtocolTransport{router}, func(context.Context) (string, error) { return "TEST-WORKLOAD", nil })
	require.NoError(t, err)
	return client
}

func relationshipCheckForTest(state suppliertrade.Relationship, purpose string) suppliertrade.RelationshipCheckInput {
	peer := state.Terms.Buyer.TenantID
	if state.Side == "buyer" {
		peer = state.Terms.Seller.TenantID
	}
	epoch := state.AuthorityEpoch
	if purpose == "retained_transaction" {
		epoch = 0
	}
	return suppliertrade.RelationshipCheckInput{OwnerTenantID: state.OwnerTenantID, PeerTenantID: peer, RelationshipID: state.ID, TermsSha256: state.TermsSha256, Purpose: purpose, ExpectedAuthorityEpoch: epoch}
}
