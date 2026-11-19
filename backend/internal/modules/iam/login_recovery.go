package iam

import (
	"context"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

func (h *LoginHandlers) loginCredential(ctx context.Context, p authn.Principal, tenant string) (string, loginclient.CredentialStatus, error) {
	credential, err := h.currentIdentity.UserCredentialState(ctx, tenant, p.Subject)
	if err != nil {
		return "", credential, err
	}
	if credential.State == loginclient.UserStateInactive {
		return "", credential, nil
	}
	if credential.State != loginclient.UserStateActive || credential.MFARecovery == nil || credential.MFARecovery.Validate() != nil {
		return "", credential, loginclient.ErrUnavailable
	}
	requirement := *credential.MFARecovery
	switch requirement.State {
	case "none":
		return "", credential, nil
	case "blocked":
		return "", credential, loginclient.ErrBadCredentials
	}
	if p.NativeSessionID == "" {
		return "", credential, loginclient.ErrBadCredentials
	}
	if h.recoveryAdmission == nil {
		return "", credential, loginclient.ErrUnavailable
	}
	input := customercontract.StaffMFAAdmissionInput{Scope: customercontract.StaffMFAAdmissionScope{
		TenantID: tenant, Subject: p.Subject, GenerationID: requirement.GenerationID, RequestID: uuid.NewString(),
	}, NativeSessionID: p.NativeSessionID}
	receipt, err := h.recoveryAdmission.AdmitStaffMFASession(ctx, input)
	if err != nil {
		return "", credential, err
	}
	if receipt.Validate() != nil || receipt.GenerationID != requirement.GenerationID {
		return "", credential, loginclient.ErrUnavailable
	}
	// A concurrent reset or password change must win over a successful remote read.
	current, err := h.currentIdentity.UserCredentialState(ctx, tenant, p.Subject)
	return receipt.GenerationID, current, err
}
