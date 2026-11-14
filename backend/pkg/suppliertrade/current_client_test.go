package suppliertrade

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"testing"
	"time"
)

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCurrentSourceClientBindsNonceContextAndDoesNotCacheEligibility(t *testing.T) {
	in := SourceCheckInput{Endpoint: testTerms().Seller, Category: "prescription", Operation: "order"}
	requests := 0
	last := uuid.Nil
	client, err := NewCurrentSourceClient("https://source.example.invalid"+SourceCheckPath, responseTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		require.Empty(t, r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("Cookie"))
		require.Equal(t, "Bearer TEST-WORKLOAD", r.Header.Get(SourceCallerHeader))
		raw, e := io.ReadAll(r.Body)
		require.NoError(t, e)
		var input SourceCheckInput
		require.NoError(t, DecodeObject(raw, &input))
		require.NotEqual(t, uuid.Nil, input.RequestID)
		require.NotEqual(t, last, input.RequestID)
		last = input.RequestID
		result := SourceCheckResult{RequestID: input.RequestID, VerificationKind: sourceVerificationKind, Eligible: requests == 1, ObservedAt: time.Now()}
		if result.Eligible {
			result.Reference = &EndpointReference{Endpoint: in.Endpoint, Category: in.Category, Operation: in.Operation, LicenceReviewID: uuid.Must(uuid.NewV7()), LicenceReviewVersion: 2, PolicyVersion: uuid.Must(uuid.NewV7())}
		}
		encoded, e := json.Marshal(result)
		require.NoError(t, e)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(encoded))}, nil
	}), func(context.Context) (string, error) { return "TEST-WORKLOAD", nil })
	require.NoError(t, err)
	_, err = client.Check(t.Context(), in)
	require.NoError(t, err)
	_, err = client.Check(t.Context(), in)
	require.ErrorIs(t, err, ErrIneligible)
	require.Equal(t, 2, requests)
}
func TestCurrentSourceClientRejectsCorruptAndReplayedPeerProofs(t *testing.T) {
	input := SourceCheckInput{Endpoint: testTerms().Seller, Category: "prescription", Operation: "order"}
	for name, corrupt := range map[string]func(map[string]any){
		"nonce":               func(v map[string]any) { v["request_id"] = uuid.Must(uuid.NewV7()) },
		"missing eligibility": func(v map[string]any) { delete(v, "eligible") },
		"null eligibility":    func(v map[string]any) { v["eligible"] = nil },
		"old proof":           func(v map[string]any) { v["observed_at"] = time.Now().Add(-time.Minute) },
		"unknown fields":      func(v map[string]any) { v["permission_bypass"] = true },
		"mismatched purpose":  func(v map[string]any) { v["reference"].(map[string]any)["operation"] = "dispatch" },
		"mismatched owner": func(v map[string]any) {
			v["reference"].(map[string]any)["endpoint"].(map[string]any)["tenant_id"] = uuid.NewString()
		},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := NewCurrentSourceClient("https://source.example.invalid"+SourceCheckPath, responseTransport(func(r *http.Request) (*http.Response, error) {
				var request SourceCheckInput
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				result := SourceCheckResult{RequestID: request.RequestID, VerificationKind: sourceVerificationKind, Eligible: true, ObservedAt: time.Now(), Reference: &EndpointReference{Endpoint: request.Endpoint, Category: request.Category, Operation: request.Operation, LicenceReviewID: uuid.Must(uuid.NewV7()), LicenceReviewVersion: 1, PolicyVersion: uuid.Must(uuid.NewV7())}}
				raw, e := json.Marshal(result)
				require.NoError(t, e)
				var value map[string]any
				require.NoError(t, json.Unmarshal(raw, &value))
				corrupt(value)
				raw, e = json.Marshal(value)
				require.NoError(t, e)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw))}, nil
			}), func(context.Context) (string, error) { return "TEST-WORKLOAD", nil })
			require.NoError(t, err)
			_, err = client.Check(t.Context(), input)
			require.ErrorIs(t, err, ErrUnavailable)
		})
	}
}
func TestCurrentSourceClientDoesNotFollowRedirectsOrAcceptBrowserOrigins(t *testing.T) {
	for _, endpoint := range []string{"http://source.example.invalid" + SourceCheckPath, "https://user:credential@source.example.invalid" + SourceCheckPath, "https://source.example.invalid" + SourceCheckPath + "?tenant=other"} {
		_, err := NewCurrentSourceClient(endpoint, nil, func(context.Context) (string, error) { return "TEST-WORKLOAD", nil })
		require.ErrorIs(t, err, ErrInvalid)
	}
	calls := 0
	client, err := NewCurrentSourceClient("https://source.example.invalid"+SourceCheckPath, responseTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://unrelated.example.invalid/"}}, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}), func(context.Context) (string, error) { return "TEST-WORKLOAD", nil })
	require.NoError(t, err)
	_, err = client.Check(t.Context(), SourceCheckInput{Endpoint: testTerms().Seller, Category: "prescription", Operation: "order"})
	require.ErrorIs(t, err, ErrUnavailable)
	require.Equal(t, 1, calls)
}
