package loginclient

import (
	"context"
	"strings"
)

func (c *Client) recoveryRequiresFactor(ctx context.Context, subject sessionSubject) (bool, error) {
	tenant, managed := strings.CutPrefix(subject.OrgID, "helivanta-")
	if !managed {
		return false, nil
	}
	credential, err := c.UserCredentialState(ctx, tenant, subject.UserID)
	if err != nil {
		return false, err
	}
	if credential.State != UserStateActive {
		return false, ErrBadCredentials
	}
	switch credential.MFARecovery.State {
	case "none":
		return false, nil
	case "blocked":
		return false, ErrBadCredentials
	case "enrollment_required", "completed":
		return true, nil
	default:
		return false, ErrUnavailable
	}
}
