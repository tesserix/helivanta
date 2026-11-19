package iam

import (
	"time"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
)

func credentialIsCurrent(observation loginclient.CredentialStatus, authenticated time.Time, recoveryGeneration string) (bool, error) {
	if !authenticated.After(time.Unix(0, 0)) || (observation.State != loginclient.UserStateActive && observation.State != loginclient.UserStateInactive) {
		return false, loginclient.ErrUnavailable
	}
	if observation.State == loginclient.UserStateInactive {
		if !observation.PasswordChangedAt.IsZero() || observation.MFARecovery != nil {
			return false, loginclient.ErrUnavailable
		}
		return false, nil
	}
	// Missing owner evidence cannot silently release an account-wide recovery fence.
	if observation.MFARecovery == nil || observation.MFARecovery.Validate() != nil {
		return false, loginclient.ErrUnavailable
	}
	switch observation.MFARecovery.State {
	case "none":
		if recoveryGeneration != "" {
			return false, nil
		}
	case "completed":
		if recoveryGeneration != observation.MFARecovery.GenerationID {
			return false, nil
		}
	default:
		return false, nil
	}
	if observation.PasswordChangedAt.IsZero() {
		return true, nil
	}
	if !observation.PasswordChangedAt.After(time.Unix(0, 0)) {
		return false, loginclient.ErrUnavailable
	}
	// OIDC may carry whole seconds; rounding a fractional native cutoff down
	// would admit authentication from before the password change.
	return authenticated.After(observation.PasswordChangedAt), nil
}
