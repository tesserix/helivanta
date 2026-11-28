package customeronboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
	case loginclient.IdentityReadCredentials:
		in.Purpose = "status"
		in.IncludeCredentialState = true
		in.IncludeMFARecovery = true
	case loginclient.IdentityReadMembership:
		in.Purpose = "membership"
	case loginclient.IdentityReadAssignmentCandidate:
		in.Purpose = "assignment_candidate"
	case loginclient.IdentityReadProfile:
		in.Purpose = "profile"
		in.IncludeTenantName = true
	default:
		return empty, loginclient.ErrUnavailable
	}
	if h == nil || h.client == nil || in.Validate() != nil {
		return empty, loginclient.ErrUnavailable
	}
	// Fixed fields only: never the subject, tenant, token or the owner's response body.
	unavailable := func(stage string, attrs ...any) (loginclient.IdentitySnapshot, error) {
		slog.WarnContext(ctx, "identity owner read unavailable", append([]any{"purpose", in.Purpose, "stage", stage}, attrs...)...)
		return empty, loginclient.ErrUnavailable
	}
	token, err := h.identityCredential()
	if err != nil || len(token) > 8192 || strings.TrimSpace(string(token)) == "" {
		return unavailable("credential")
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
		return unavailable("transport", "deadline", errors.Is(err, context.DeadlineExceeded), "cancelled", errors.Is(err, context.Canceled))
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return unavailable("status", "status", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return unavailable("body")
	}
	view, err := customercontract.DecodeIdentityProjection(raw)
	if err != nil || view.Purpose != in.Purpose || view.TenantID != tenant || view.Subject != subject || view.OrganizationID != "helivanta-"+tenant {
		return unavailable("projection")
	}
	if view.Credentials != nil && !in.IncludeCredentialState || in.IncludeCredentialState && view.State == "active" && view.Credentials == nil {
		return unavailable("projection")
	}
	if view.MFARecovery != nil && !in.IncludeMFARecovery || in.IncludeMFARecovery && view.State == "active" && view.MFARecovery == nil {
		return unavailable("projection")
	}
	snapshot := loginclient.IdentitySnapshot{Candidate: view.Candidate, Credentials: view.Credentials, MFARecovery: view.MFARecovery}
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
