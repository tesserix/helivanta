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

// TenantIdentitySnapshot projects only the requested identity-owner purpose.
func (h *Handlers) TenantIdentitySnapshot(ctx context.Context, tenant, subject string, purpose loginclient.IdentityReadPurpose) (loginclient.IdentitySnapshot, error) {
	var empty loginclient.IdentitySnapshot
	in := customercontract.IdentityProjectionInput{TenantID: tenant, Subject: subject, RequestID: uuid.NewString()}
	switch purpose {
	case loginclient.IdentityReadStatus:
		in.Purpose = "status"
	case loginclient.IdentityReadMembership:
		in.Purpose = "membership"
	case loginclient.IdentityReadProfile:
		in.Purpose = "profile"
		in.IncludeTenantName = true
	default:
		return empty, loginclient.ErrUnavailable
	}
	if h == nil || h.client == nil || in.Validate() != nil {
		return empty, loginclient.ErrUnavailable
	}
	token, err := h.identityCredential()
	if err != nil || len(token) > 8192 || strings.TrimSpace(string(token)) == "" {
		return empty, loginclient.ErrUnavailable
	}
	body, err := json.Marshal(in)
	if err != nil {
		return empty, loginclient.ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, h.base+"/internal/v1/customer-signups/identity-projection", bytes.NewReader(body))
	if err != nil {
		return empty, loginclient.ErrUnavailable
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	request.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(request)
	if err != nil {
		return empty, loginclient.ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return empty, loginclient.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return empty, loginclient.ErrUnavailable
	}
	view, err := customercontract.DecodeIdentityProjection(raw)
	if err != nil || view.Purpose != in.Purpose || view.TenantID != tenant || view.Subject != subject || view.OrganizationID != "helivanta-"+tenant {
		return empty, loginclient.ErrUnavailable
	}
	snapshot := loginclient.IdentitySnapshot{}
	switch view.State {
	case "active":
		snapshot.State = loginclient.UserStateActive
	case "inactive":
		snapshot.State = loginclient.UserStateInactive
	default:
		return empty, loginclient.ErrUnavailable
	}
	if purpose == loginclient.IdentityReadMembership {
		snapshot.MembershipEligible = view.Admission == "eligible"
	}
	if view.Profile != nil {
		if view.TenantName == "" {
			return empty, loginclient.ErrUnavailable
		}
		snapshot.Profile = &loginclient.UserProfile{DisplayName: view.Profile.DisplayName, Email: view.Profile.Email, TenantName: view.TenantName}
	}
	return snapshot, nil
}
