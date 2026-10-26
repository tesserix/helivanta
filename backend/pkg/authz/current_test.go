package authz

import (
	"encoding/json"
	fgaclient "github.com/openfga/go-sdk/client"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCurrentAuthorizationRequestsHigherConsistencyForMembershipAndPermissions(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		requests = append(requests, body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/stores/01ARZ3NDEKTSV4RRFFQ69G5FAV/check" {
			_, _ = w.Write([]byte(`{"allowed":true}`))
		} else {
			_, _ = w.Write([]byte(`{"objects":["perm:tenant/private.evidence.read"]}`))
		}
	}))
	t.Cleanup(server.Close)
	api, err := fgaclient.NewSdkClient(&fgaclient.ClientConfiguration{ApiUrl: server.URL, StoreId: "01ARZ3NDEKTSV4RRFFQ69G5FAV"})
	require.NoError(t, err)
	c := &Client{api: api}
	ctx := WithCurrentAuthorization(t.Context())
	member, err := c.IsMember(ctx, "alice", "tenant")
	require.NoError(t, err)
	require.True(t, member)
	set, err := c.Resolve(ctx, "alice", "tenant")
	require.NoError(t, err)
	require.True(t, set.Has("private.evidence.read"))
	require.Len(t, requests, 2)
	for _, body := range requests {
		require.Equal(t, "HIGHER_CONSISTENCY", body["consistency"])
	}
	requests = nil
	_, err = c.IsMember(t.Context(), "alice", "tenant")
	require.NoError(t, err)
	_, err = c.Resolve(t.Context(), "alice", "tenant")
	require.NoError(t, err)
	for _, body := range requests {
		require.NotContains(t, body, "consistency", "ordinary routes keep their configured default behavior")
	}
}
