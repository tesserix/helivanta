package suppliertrade

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const SourceCheckPath = "/v1/supply/internal/trading-source/check"
const SourceCallerHeader = "X-Helivanta-Supplier-Trade-Caller"
const SourceCheckAudience = "helivanta-supplier-trade-current"
const sourceVerificationKind = "local_source_eligibility"

type SourceCheckInput = SupplierTradeSourceCheckInput
type SourceCheckResult = SupplierTradeSourceCheckResult

type CurrentSourceClient struct {
	endpoint  string
	transport http.RoundTripper
	token     func(context.Context) (string, error)
}

// NewCurrentSourceClient accepts only an operator-configured peer; clients cannot supply a URL.
func NewCurrentSourceClient(endpoint string, transport http.RoundTripper, token func(context.Context) (string, error)) (*CurrentSourceClient, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != SourceCheckPath || u.RawPath != "" || token == nil || (u.Scheme != "https" && (u.Scheme != "http" || !strings.HasSuffix(u.Hostname(), ".svc.cluster.local"))) {
		return nil, ErrInvalid
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &CurrentSourceClient{endpoint: endpoint, transport: transport, token: token}, nil
}
func (c *CurrentSourceClient) Check(ctx context.Context, input SourceCheckInput) (EndpointReference, error) {
	if c == nil || c.token == nil {
		return EndpointReference{}, ErrUnavailable
	}
	if !ValidEndpointUse(input.Endpoint, input.Category, input.Operation) {
		return EndpointReference{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	credential, err := c.token(ctx)
	if err != nil || credential == "" || len(credential) > 8192 || strings.ContainsAny(credential, " \r\n\t") {
		return EndpointReference{}, ErrUnavailable
	}
	input.RequestID, err = uuid.NewV7()
	if err != nil {
		return EndpointReference{}, ErrUnavailable
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return EndpointReference{}, ErrInvalid
	}
	request, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return EndpointReference{}, ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set(SourceCallerHeader, "Bearer "+credential)
	started := time.Now()
	client := http.Client{Transport: c.transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return EndpointReference{}, ErrUnavailable
	}
	defer response.Body.Close()
	media, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || mediaErr != nil || media != "application/json" {
		return EndpointReference{}, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<10+1))
	if err != nil || len(raw) > 16<<10 || !utf8.Valid(raw) {
		return EndpointReference{}, ErrUnavailable
	}
	var result SourceCheckResult
	var fields map[string]json.RawMessage
	if DecodeObject(raw, &result) != nil || DecodeObject(raw, &fields) != nil || result.RequestID != input.RequestID || result.VerificationKind != sourceVerificationKind || result.ObservedAt.Before(started.Add(-5*time.Second)) || result.ObservedAt.After(time.Now().Add(5*time.Second)) {
		return EndpointReference{}, ErrUnavailable
	}
	if string(fields["eligible"]) != "true" && string(fields["eligible"]) != "false" {
		return EndpointReference{}, ErrUnavailable
	}
	if !result.Eligible && fields["reference"] != nil {
		return EndpointReference{}, ErrUnavailable
	}
	if !result.Eligible {
		if result.Reference != nil {
			return EndpointReference{}, ErrUnavailable
		}
		return EndpointReference{}, ErrIneligible
	}
	ref := result.Reference
	if ref == nil || ref.Category != input.Category || ref.Operation != input.Operation || !EndpointEqual(ref.Endpoint, input.Endpoint) || ref.LicenceReviewID == uuid.Nil || ref.LicenceReviewVersion < 1 || ref.PolicyVersion == uuid.Nil {
		return EndpointReference{}, ErrUnavailable
	}
	return *ref, nil
}

// SourceChecker reads fresh local-owner facts. Callers still need bilateral and recipient authority.
type SourceChecker interface {
	Check(context.Context, SourceCheckInput) (EndpointReference, error)
}
