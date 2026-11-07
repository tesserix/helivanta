package suppliertrade

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"
)

const RelationshipCheckPath = "/v1/supply/internal/relationship/check"

type RelationshipCheckInput = SupplierRelationshipCheckInput
type RelationshipCheckResult = SupplierRelationshipCheckResult
type RelationshipChecker interface {
	CheckRelationship(context.Context, RelationshipCheckInput) (RelationshipCheckResult, error)
}

func ValidRelationshipCheck(input RelationshipCheckInput) bool {
	digest, err := hex.DecodeString(input.TermsSha256)
	return err == nil && len(digest) == 32 && input.OwnerTenantID != uuid.Nil && input.PeerTenantID != uuid.Nil && input.OwnerTenantID != input.PeerTenantID && recordID(input.RelationshipID) && (input.Purpose == "new_business" || input.Purpose == "retained_transaction") && input.ExpectedAuthorityEpoch >= 0 && input.ExpectedAuthorityEpoch <= 9007199254740991 && (input.Purpose != "new_business" || input.ExpectedAuthorityEpoch > 0)
}
func (c *CurrentSourceClient) CheckRelationship(ctx context.Context, input RelationshipCheckInput) (RelationshipCheckResult, error) {
	empty := RelationshipCheckResult{}
	if c == nil || c.token == nil {
		return empty, ErrUnavailable
	}
	if !ValidRelationshipCheck(input) {
		return empty, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	credential, err := c.token(ctx)
	if err != nil || !validPeerCredential(credential) {
		return empty, ErrUnavailable
	}
	input.RequestID, err = uuid.NewV7()
	if err != nil {
		return empty, ErrUnavailable
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return empty, ErrInvalid
	}
	endpoint, err := url.Parse(c.endpoint)
	if err != nil {
		return empty, ErrUnavailable
	}
	endpoint.Path = RelationshipCheckPath
	request, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return empty, ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set(SourceCallerHeader, "Bearer "+credential)
	client := http.Client{Transport: c.transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return empty, ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	media, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != 200 || mediaErr != nil || media != "application/json" {
		return empty, ErrUnavailable
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, 16<<10+1))
	if err != nil {
		return empty, ErrUnavailable
	}
	var result RelationshipCheckResult
	var fields map[string]json.RawMessage
	if DecodeObject(raw, &result) != nil || DecodeObject(raw, &fields) != nil || (string(fields["eligible"]) != "true" && string(fields["eligible"]) != "false") || result.RequestID != input.RequestID || result.OwnerTenantID != input.OwnerTenantID || result.PeerTenantID != input.PeerTenantID || result.RelationshipID != input.RelationshipID || result.TermsSha256 != input.TermsSha256 || result.Purpose != input.Purpose || result.ObservedAt.Before(started.Add(-5*time.Second)) || result.ObservedAt.After(time.Now().Add(5*time.Second)) {
		return empty, ErrUnavailable
	}
	if !result.Eligible {
		return empty, ErrIneligible
	}
	if result.AuthorityEpoch < 1 || result.AuthorityEpoch > 9007199254740991 || result.RemoteEpoch < 1 || result.RemoteEpoch > 9007199254740991 || (input.Purpose == "new_business" && result.AuthorityEpoch != input.ExpectedAuthorityEpoch) {
		return empty, ErrUnavailable
	}
	return result, nil
}
