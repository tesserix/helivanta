package config

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoZitadelLoginClientToken is returned by RequireZitadelLoginClientToken
// when ZITADEL_LOGIN_CLIENT_TOKEN is unset. See the function's doc comment
// for why this refuses to boot rather than running with login disabled.
var ErrNoZitadelLoginClientToken = errors.New("config: ZITADEL_LOGIN_CLIENT_TOKEN is not set")

// RequireZitadelLoginClientToken returns the trimmed login-client PAT, or
// refuses with ErrNoZitadelLoginClientToken when it is empty.
//
// This mirrors SessionSigningKeySeed's shape and reasoning
// (signingkey.go), applied to a different secret: Helivanta's own login form
// (internal/modules/iam/loginui.go) cannot check a single credential or
// finalize a single sign-in without a working loginclient.Client, so an
// API that boots without this token would not be "login degraded" — it
// would be "every one of these three routes 500s, forever, on every
// request", per docs/standards/engineering-principles.md §3's framing:
// a half-working login is worse than a process that refuses to start,
// because the failure is immediate, loud, and at the one point an
// operator is already looking (the deploy), rather than discovered by
// the first clinician who cannot sign in.
//
// Unlike SessionSigningKey, there is no well-known dev value this
// function has to guard against being reused in production — the local
// dev stack's PAT (dev/zitadel/secrets/login-client.pat) is generated
// per-clone by Zitadel's own first-instance provisioning
// (docker-compose.dev.yml), not committed to source control the way
// DevSessionSigningKey is, so it carries no cross-environment forgery
// risk to check for here. What IS checked, by callers of this function
// rather than here, is that the value itself never reaches a log line or
// an HTTP response — see ZitadelLoginClientToken's doc comment on why
// that property matters more for this credential than almost any other
// Helivanta holds.
func (c Config) RequireZitadelLoginClientToken() (string, error) {
	token := strings.TrimSpace(c.ZitadelLoginClientToken)
	if token == "" {
		return "", fmt.Errorf(
			"%w: refusing to boot without one — Helivanta's own login form "+
				"(POST /v1/auth/login/password and its two sibling routes) "+
				"cannot check a credential or finalize a sign-in without it, so "+
				"every login attempt would 500 rather than one of them being "+
				"individually degraded; set ZITADEL_LOGIN_CLIENT_TOKEN to the "+
				"IAM_LOGIN_CLIENT machine user's PAT (locally: the contents of "+
				"dev/zitadel/secrets/login-client.pat, written by Zitadel's own "+
				"first-instance provisioning — see docker-compose.dev.yml's "+
				"zitadel-pat-ready service)",
			ErrNoZitadelLoginClientToken,
		)
	}
	return token, nil
}
