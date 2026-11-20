package customeronboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	var target customercontract.LoginTarget
	status, err := h.postIdentity(ctx, "/internal/v1/customer-signups/login-target", in, &target)
	if status == 404 || status == 400 {
		return "", loginclient.ErrUserNotFound
	}
	if err != nil {
		return "", err
	}
	if target.Validate() != nil || target.TenantID != in.TenantID || target.OrganizationID != org {
		return "", fmt.Errorf("identity owner login-target: binding off contract: %w", loginclient.ErrUnavailable)
	}
	return target.Subject, nil
}

// TenantLoginTargets uses the identity owner's unscoped lookup (#1751 spec D2).
// No eligible account is an empty list; anything the contract does not allow
// is unavailable, never a guess.
func (h *Handlers) TenantLoginTargets(ctx context.Context, email string) ([]loginclient.LoginTarget, bool, error) {
	in := customercontract.LoginTargetsInput{Email: email, RequestID: uuid.NewString()}
	if in.Validate() != nil {
		return nil, false, nil
	}
	var answer customercontract.LoginTargets
	status, err := h.postIdentity(ctx, "/internal/v1/customer-signups/login-targets", in, &answer)
	if status == 400 {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if answer.Validate() != nil || len(answer.Targets) > loginclient.MaxEmailCandidates {
		return nil, false, fmt.Errorf("identity owner login-targets: answer off contract: %w", loginclient.ErrUnavailable)
	}
	targets := make([]loginclient.LoginTarget, 0, len(answer.Targets))
	for _, t := range answer.Targets {
		targets = append(targets, loginclient.LoginTarget{TenantID: t.TenantID, OrganizationID: t.OrganizationID, Subject: t.Subject, Name: t.Name, Account: t.Account})
	}
	return targets, answer.Truncated, nil
}

// postIdentity calls one private identity-owner operation with the workload
// credential and strictly decodes a 200 into out. Any other status is
// returned with ErrUnavailable so callers map the statuses they expect. Every
// failure names the operation and what went wrong, never a body or token, so
// a 503 at sign-in can be traced to its cause from the API log alone.
func (h *Handlers) postIdentity(ctx context.Context, path string, in, out any) (int, error) {
	fail := func(status int, why string) (int, error) {
		return status, fmt.Errorf("identity owner %s: %s: %w", path, why, loginclient.ErrUnavailable)
	}
	token, err := h.identityCredential()
	if err != nil || len(token) > 8192 || strings.TrimSpace(string(token)) == "" {
		return fail(0, "workload credential unreadable")
	}
	body, err := json.Marshal(in)
	if err != nil {
		return fail(0, "request not encodable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, h.base+path, bytes.NewReader(body))
	if err != nil {
		return fail(0, "request not buildable")
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	request.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(request)
	if err != nil {
		return fail(0, "transport: "+err.Error())
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		return fail(response.StatusCode, fmt.Sprintf("status %d", response.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return fail(response.StatusCode, "response unreadable or over 8192 bytes")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return fail(response.StatusCode, "response off contract")
	}
	return response.StatusCode, nil
}
