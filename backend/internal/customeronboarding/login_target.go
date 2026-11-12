package customeronboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

// TenantLoginSubject uses the identity owner's scoped lookup, never a public route.
func (h *Handlers) TenantLoginSubject(ctx context.Context, org, email string) (string, error) {
	in := customercontract.LoginTargetInput{TenantID: strings.TrimPrefix(org, "helivanta-"), Email: email, RequestID: uuid.NewString()}
	if in.Validate() != nil || org != "helivanta-"+in.TenantID {
		return "", loginclient.ErrUserNotFound
	}
	token, err := h.identityCredential()
	if err != nil || len(token) > 8192 || strings.TrimSpace(string(token)) == "" {
		return "", loginclient.ErrUnavailable
	}
	body, err := json.Marshal(in)
	if err != nil {
		return "", loginclient.ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, h.base+"/internal/v1/customer-signups/login-target", bytes.NewReader(body))
	if err != nil {
		return "", loginclient.ErrUnavailable
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	request.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(request)
	if err != nil {
		return "", loginclient.ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == 404 || response.StatusCode == 400 {
		return "", loginclient.ErrUserNotFound
	}
	if response.StatusCode != 200 {
		return "", loginclient.ErrUnavailable
	}
	var target customercontract.LoginTarget
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return "", loginclient.ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&target) != nil || decoder.Decode(new(any)) != io.EOF || target.Validate() != nil || target.TenantID != in.TenantID || target.OrganizationID != org {
		return "", loginclient.ErrUnavailable
	}
	return target.Subject, nil
}
