package customeronboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var ErrSecurityDenied = errors.New("owner security denied")
var ErrSecurityConflict = errors.New("owner security changed")
var ErrSecurityUnavailable = errors.New("owner security unavailable")

type SecurityClient struct{ client *http.Client }

func NewSecurityClient() *SecurityClient {
	return &SecurityClient{client: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (s *SecurityClient) OwnerSecurity(ctx context.Context, in customercontract.SecurityRequest, apply bool) (customercontract.SecurityView, error) {
	var empty customercontract.SecurityView
	if in.Validate() != nil || apply && in.ExpectedRevision == "" {
		return empty, ErrSecurityConflict
	}
	token, err := os.ReadFile("/var/run/owner-security/token")
	if err != nil || len(token) == 0 || len(token) > 16384 {
		return empty, ErrSecurityUnavailable
	}
	body, err := json.Marshal(in)
	if err != nil {
		return empty, ErrSecurityUnavailable
	}
	path := "read"
	if apply {
		path = "reconcile"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://onboarding-api.onboarding.svc.cluster.local:8080/internal/v1/owner-security/"+path, bytes.NewReader(body))
	if err != nil {
		return empty, ErrSecurityUnavailable
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return empty, ErrSecurityUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	switch response.StatusCode {
	case 403, 404:
		return empty, ErrSecurityDenied
	case 409:
		return empty, ErrSecurityConflict
	}
	if response.StatusCode != 200 && response.StatusCode != 202 {
		return empty, ErrSecurityUnavailable
	}
	var view customercontract.SecurityView
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&view) != nil || decoder.Decode(new(any)) != io.EOF || view.Validate() != nil || view.TenantID != in.TenantID || view.Subject != in.Subject {
		return empty, ErrSecurityUnavailable
	}
	return view, nil
}
