package config

import "fmt"

const (
	webAuthnRPIDProd = "helivanta.app"
	webAuthnRPIDDev  = "helivanta.localhost"
)

// ValidatedWebAuthnRPID returns the WebAuthn relying-party ID, or an error
// unless it is exactly the one value allowed for this environment.
func (c Config) ValidatedWebAuthnRPID() (string, error) {
	want := webAuthnRPIDProd
	if c.IsDev() {
		want = webAuthnRPIDDev
	}
	// Fail closed: Zitadel accepts any RP ID a caller sends, so a wrong or
	// missing value would mint credentials bound to the wrong origin. An
	// exact match, never parsed or normalised, leaves no near-miss to accept.
	if c.WebAuthnRPID != want {
		return "", fmt.Errorf("WEBAUTHN_RP_ID must be exactly %q in this environment, got %q", want, c.WebAuthnRPID)
	}
	return want, nil
}
