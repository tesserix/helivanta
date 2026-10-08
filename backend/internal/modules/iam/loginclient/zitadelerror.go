package loginclient

import (
	"errors"
	"fmt"
	"net/http"
)

// Credential-class sentinels added by #901. They sit alongside
// ErrBadCredentials and ErrUserNotFound (client.go) and, like them, are only
// ever distinguished in OUR logs: the login handlers answer all of them with
// spec D5's single equalised refusal (see IsCredentialRefusal).
var (
	// ErrAccountLocked: Zitadel refused because the user is locked —
	// COMMAND-JLK35 / COMMAND-SFA3t on the password path, COMMAND-SF3fg on
	// the TOTP path (zitadel internal/command, verifyPasswordWithLockoutPolicy
	// and checkTOTP).
	ErrAccountLocked = errors.New("loginclient: account locked")
	// ErrPasswordNotSet: the user exists but has no password to check
	// (COMMAND-3nJ4t, Errors.User.Password.NotSet).
	ErrPasswordNotSet = errors.New("loginclient: password not set")
	// ErrRejected: Zitadel answered 400 with an id this package does not
	// recognise (or none at all). It replaces the old rule that EVERY 400 was
	// ErrBadCredentials — a label asserting a cause nobody had observed. It
	// is still a refusal, never a success; only the name stopped lying.
	ErrRejected = errors.New("loginclient: rejected by zitadel")
)

// badRequestKinds maps the 400 ids a login client's password and TOTP checks
// are known to produce (#901 spec D2, read from zitadel's internal/command at
// 8a54a2a) to the sentinel each one actually means. Anything absent is
// ErrRejected — never assumed to be a wrong password.
//
// COMMAND-3M0fs is also Zitadel's id for Errors.IDMissing and
// Errors.User.Password.Empty; both mean the submitted credential was
// unusable, which is what ErrBadCredentials means to every caller.
var badRequestKinds = map[string]error{
	"COMMAND-3M0fs": ErrBadCredentials, // Errors.User.Password.Invalid
	"EVENT-8isk2":   ErrBadCredentials, // Errors.User.MFA.OTP.InvalidCode
	"TOTP-Auw0a":    ErrBadCredentials, // Errors.User.MFA.OTP.Reused
	"COMMAND-JLK35": ErrAccountLocked,  // Errors.User.Locked (before the check)
	"COMMAND-SFA3t": ErrAccountLocked,  // Errors.User.Locked (locked by this check)
	"COMMAND-SF3fg": ErrAccountLocked,  // Errors.User.Locked (TOTP path)
	"COMMAND-3nJ4t": ErrPasswordNotSet, // Errors.User.Password.NotSet
	"COMMAND-3n77z": ErrUserNotFound,   // Errors.User.NotFound (precondition form)
	// #856: the new password failed the org's complexity policy
	// (PasswordComplexityPolicy.Check). Only POST /v2/users/{id}/password
	// produces these; ChangePassword names the rule (passwordPolicyRules).
	"DOMAIN-HuJf6": ErrPasswordPolicy, // Errors.User.PasswordComplexityPolicy.MinLength
	"DOMAIN-co3Xw": ErrPasswordPolicy, // Errors.User.PasswordComplexityPolicy.HasLower
	"DOMAIN-VoaRj": ErrPasswordPolicy, // Errors.User.PasswordComplexityPolicy.HasUpper
	"DOMAIN-ZBv4H": ErrPasswordPolicy, // Errors.User.PasswordComplexityPolicy.HasNumber
	"DOMAIN-ZDLwA": ErrPasswordPolicy, // Errors.User.PasswordComplexityPolicy.HasSymbol
}

// ZitadelError is every non-2xx answer do() turns into an error (#901 spec
// D1). It keeps the one thing from Zitadel's error body this package ever
// keeps — the error id — alongside the status, so a handler can log WHAT
// Zitadel said without parsing an error string. Kind is the sentinel callers
// compare with errors.Is; Unwrap returns it, so every existing
// errors.Is(err, ErrBadCredentials) keeps working unchanged.
//
// Nothing else from the body is kept: failedAttempts and the raw message
// stay out of the error, exactly as readZitadelErrorID always ensured.
type ZitadelError struct {
	Method string
	Path   string
	Status int
	ID     string
	Kind   error
}

func (e *ZitadelError) Error() string {
	return fmt.Sprintf("%s %s: status %d id=%s: %v", e.Method, e.Path, e.Status, e.ID, e.Kind)
}

func (e *ZitadelError) Unwrap() error { return e.Kind }

// ZitadelErrorID returns the Zitadel error id carried by err (e.g.
// "COMMAND-3M0fs"), or "" when err is not, and does not wrap, a ZitadelError.
func ZitadelErrorID(err error) string {
	var ze *ZitadelError
	if errors.As(err, &ze) {
		return ze.ID
	}
	return ""
}

// ZitadelStatus returns the HTTP status carried by err, or 0 when err is not,
// and does not wrap, a ZitadelError (a transport failure has no status).
func ZitadelStatus(err error) int {
	var ze *ZitadelError
	if errors.As(err, &ze) {
		return ze.Status
	}
	return 0
}

// IsCredentialRefusal reports whether err is one of the credential-class
// refusals the login handlers must answer with spec D5's single equalised
// refusal (#901 spec D3). It is the ONE definition of that set: both the
// password and factor handlers use it, so a sentinel added here cannot be
// forgotten in one of them and leak a different answer (or a 500) for a
// condition that reveals whether an account exists.
func IsCredentialRefusal(err error) bool {
	return errors.Is(err, ErrBadCredentials) ||
		errors.Is(err, ErrUserNotFound) ||
		errors.Is(err, ErrAccountLocked) ||
		errors.Is(err, ErrPasswordNotSet) ||
		errors.Is(err, ErrRejected)
}

// FailureOutcome names which credential-class refusal err is, for the log
// line only (#901 spec D4) — never for the response.
func FailureOutcome(err error) string {
	switch {
	case errors.Is(err, ErrBadCredentials):
		return "bad_credentials"
	case errors.Is(err, ErrUserNotFound):
		return "user_not_found"
	case errors.Is(err, ErrAccountLocked):
		return "account_locked"
	case errors.Is(err, ErrPasswordNotSet):
		return "password_not_set"
	case errors.Is(err, ErrRejected):
		return "rejected"
	default:
		return "unknown"
	}
}

// classifyStatus picks the sentinel for a non-2xx answer: a 400 by its id
// (badRequestKinds, else ErrRejected), a 404 by the per-call notFound, and
// everything else as ErrUnavailable (a 5xx, or a 401/403 meaning the login
// client PAT itself is misconfigured — an operational failure Helivanta cannot
// resolve per request, not a refusal of the end user's credential).
func classifyStatus(status int, id string, notFound error) error {
	switch status {
	case http.StatusBadRequest:
		if kind, ok := badRequestKinds[id]; ok {
			return kind
		}
		return ErrRejected
	case http.StatusNotFound:
		return notFound
	default:
		return ErrUnavailable
	}
}
