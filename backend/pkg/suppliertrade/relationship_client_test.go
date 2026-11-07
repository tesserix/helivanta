package suppliertrade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func relationshipCheckFixture() RelationshipCheckInput {
	return RelationshipCheckInput{OwnerTenantID: uuid.New(), PeerTenantID: uuid.New(), RelationshipID: uuid.Must(uuid.NewV7()), TermsSha256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Purpose: "new_business", ExpectedAuthorityEpoch: 2}
}

func relationshipReply(input RelationshipCheckInput) RelationshipCheckResult {
	return RelationshipCheckResult{RequestID: input.RequestID, OwnerTenantID: input.OwnerTenantID, PeerTenantID: input.PeerTenantID, RelationshipID: input.RelationshipID, TermsSha256: input.TermsSha256, Purpose: input.Purpose, Eligible: true, AuthorityEpoch: 2, RemoteEpoch: 3, ObservedAt: time.Now()}
}

func TestRelationshipClientBindsFreshNonceAndNeverCachesConsent(t *testing.T) {
	input := relationshipCheckFixture()
	calls := 0
	last := uuid.Nil
	client, err := NewCurrentSourceClient("https://source.example.invalid"+SourceCheckPath, responseTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, RelationshipCheckPath, r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		require.Empty(t, r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("Cookie"))
		require.Equal(t, "Bearer TEST-WORKLOAD", r.Header.Get(SourceCallerHeader))
		var request RelationshipCheckInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.True(t, recordID(request.RequestID))
		require.NotEqual(t, last, request.RequestID)
		last = request.RequestID
		request.RequestID = uuid.Nil
		require.Equal(t, input, request)
		request.RequestID = last
		result := relationshipReply(request)
		result.Eligible = calls == 1
		raw, e := json.Marshal(result)
		require.NoError(t, e)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw))}, nil
	}), func(context.Context) (string, error) { return "TEST-WORKLOAD", nil })
	require.NoError(t, err)
	result, err := client.CheckRelationship(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, int64(3), result.RemoteEpoch)
	_, err = client.CheckRelationship(t.Context(), input)
	require.ErrorIs(t, err, ErrIneligible)
	require.Equal(t, 2, calls)
}

func TestRelationshipClientRejectsMismatchedStaleAndMalformedConsent(t *testing.T) {
	for name, corrupt := range map[string]func(map[string]any){
		"nonce":                  func(v map[string]any) { v["request_id"] = uuid.Must(uuid.NewV7()) },
		"owner":                  func(v map[string]any) { v["owner_tenant_id"] = uuid.New() },
		"peer":                   func(v map[string]any) { v["peer_tenant_id"] = uuid.New() },
		"relationship":           func(v map[string]any) { v["relationship_id"] = uuid.Must(uuid.NewV7()) },
		"terms":                  func(v map[string]any) { v["terms_sha256"] = "different" },
		"purpose":                func(v map[string]any) { v["purpose"] = "retained_transaction" },
		"stale":                  func(v map[string]any) { v["observed_at"] = time.Now().Add(-time.Minute) },
		"future":                 func(v map[string]any) { v["observed_at"] = time.Now().Add(time.Minute) },
		"missing eligibility":    func(v map[string]any) { delete(v, "eligible") },
		"null eligibility":       func(v map[string]any) { v["eligible"] = nil },
		"unknown field":          func(v map[string]any) { v["grant_all"] = true },
		"changed epoch":          func(v map[string]any) { v["authority_epoch"] = 3 },
		"zero remote epoch":      func(v map[string]any) { v["remote_epoch"] = 0 },
		"oversized remote epoch": func(v map[string]any) { v["remote_epoch"] = int64(9007199254740992) },
	} {
		t.Run(name, func(t *testing.T) {
			client, err := NewCurrentSourceClient("https://source.example.invalid"+SourceCheckPath, responseTransport(func(r *http.Request) (*http.Response, error) {
				var input RelationshipCheckInput
				require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
				raw, e := json.Marshal(relationshipReply(input))
				require.NoError(t, e)
				var value map[string]any
				require.NoError(t, json.Unmarshal(raw, &value))
				corrupt(value)
				raw, e = json.Marshal(value)
				require.NoError(t, e)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw))}, nil
			}), func(context.Context) (string, error) { return "TEST-WORKLOAD", nil })
			require.NoError(t, err)
			_, err = client.CheckRelationship(t.Context(), relationshipCheckFixture())
			require.ErrorIs(t, err, ErrUnavailable)
		})
	}
}

func TestRelationshipClientRetainedPurposeDoesNotRequireCurrentNewBusinessEpoch(t *testing.T) {
	input := relationshipCheckFixture()
	input.Purpose, input.ExpectedAuthorityEpoch = "retained_transaction", 0
	client, err := NewCurrentSourceClient("https://source.example.invalid"+SourceCheckPath, responseTransport(func(r *http.Request) (*http.Response, error) {
		var request RelationshipCheckInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		raw, e := json.Marshal(relationshipReply(request))
		require.NoError(t, e)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw))}, nil
	}), func(context.Context) (string, error) { return "TEST-WORKLOAD", nil })
	require.NoError(t, err)
	result, err := client.CheckRelationship(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, "retained_transaction", result.Purpose)
	require.Equal(t, int64(2), result.AuthorityEpoch)
}

func TestRelationshipClientRejectsBadIntentAndUnavailableCredentialsBeforeTransport(t *testing.T) {
	transport := responseTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid intent or credentials must not reach the network")
		return nil, errors.New("unexpected transport")
	})
	for name, mutate := range map[string]func(*RelationshipCheckInput){
		"same parties":     func(v *RelationshipCheckInput) { v.PeerTenantID = v.OwnerTenantID },
		"missing owner":    func(v *RelationshipCheckInput) { v.OwnerTenantID = uuid.Nil },
		"legacy record ID": func(v *RelationshipCheckInput) { v.RelationshipID = uuid.New() },
		"invalid digest":   func(v *RelationshipCheckInput) { v.TermsSha256 = "invalid" },
		"unknown purpose":  func(v *RelationshipCheckInput) { v.Purpose = "all" },
		"missing epoch":    func(v *RelationshipCheckInput) { v.ExpectedAuthorityEpoch = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			client, err := NewCurrentSourceClient("https://source.example.invalid"+SourceCheckPath, transport, func(context.Context) (string, error) { return "TEST-WORKLOAD", nil })
			require.NoError(t, err)
			input := relationshipCheckFixture()
			mutate(&input)
			_, err = client.CheckRelationship(t.Context(), input)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
	for _, credential := range []string{"", "bad\ncredential"} {
		client, err := NewCurrentSourceClient("https://source.example.invalid"+SourceCheckPath, transport, func(context.Context) (string, error) { return credential, nil })
		require.NoError(t, err)
		_, err = client.CheckRelationship(t.Context(), relationshipCheckFixture())
		require.ErrorIs(t, err, ErrUnavailable)
	}
}
