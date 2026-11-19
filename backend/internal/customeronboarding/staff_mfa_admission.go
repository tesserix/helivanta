package customeronboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

// AdmitStaffMFASession exchanges only server-verified native evidence for a generation.
func (h *Handlers) AdmitStaffMFASession(ctx context.Context, in customercontract.StaffMFAAdmissionInput) (customercontract.StaffMFAAdmissionReceipt, error) {
	var empty customercontract.StaffMFAAdmissionReceipt
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
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, h.base+"/internal/v1/customer-signups/staff-mfa-recovery/admit", bytes.NewReader(body))
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
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict:
		return empty, loginclient.ErrBadCredentials
	default:
		return empty, loginclient.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return empty, loginclient.ErrUnavailable
	}
	var receipt customercontract.StaffMFAAdmissionReceipt
	err = json.Unmarshal(raw, &receipt)
	if err != nil || receipt.GenerationID != in.Scope.GenerationID {
		return empty, loginclient.ErrUnavailable
	}
	return receipt, nil
}
