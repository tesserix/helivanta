package iam

import (
	"time"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
)

func credentialIsCurrent(observation loginclient.CredentialStatus, authenticated time.Time) (bool, error) {
	if !authenticated.After(time.Unix(0, 0)) || (observation.State != loginclient.UserStateActive && observation.State != loginclient.UserStateInactive) {
		return false, loginclient.ErrUnavailable
	}
	if observation.State == loginclient.UserStateInactive {
		if !observation.PasswordChangedAt.IsZero() {
			return false, loginclient.ErrUnavailable
		}
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
