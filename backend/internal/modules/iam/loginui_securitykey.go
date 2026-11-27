package iam

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
)

type securityKeyRequiredResponse struct {
	SecurityKey struct {
		URL string `json:"url"`
	} `json:"security_key_required"`
}

func (h *LoginUIHandlers) securityKeyContinuation(c *gin.Context, requestID string, s loginclient.Session, result loginclient.Result) bool {
	if h.nativeWebClientID == "" {
		return false
	}
	if result.Outcome != loginclient.OutcomeRefused || result.Reason != loginclient.RefusalFactorUnsupported {
		return false
	}
	key := false
	for _, method := range result.EnrolledMethods {
		switch method {
		case "AUTHENTICATION_METHOD_TYPE_U2F":
			key = true
		case "AUTHENTICATION_METHOD_TYPE_PASSWORD", "AUTHENTICATION_METHOD_TYPE_IDP":
		default:
			return false
		}
	}
	if !key {
		return false
	}
	target, err := h.client.SecurityKeyLoginURL(c.Request.Context(), requestID, s, h.nativeWebClientID)
	if err != nil {
		h.respondLoginClientError(c, err, "security_key_continuation")
		return true
	}
	body := securityKeyRequiredResponse{}
	body.SecurityKey.URL = target
	audit.Log(c.Request.Context(), requestid.Logger(c), slog.LevelInfo, "auth.login.security_key_required", "login requires native security key", "auth_request_id", requestID, "outcome", "security_key_required")
	respond.OK(c, body)
	return true
}

// SetNativeWebClientID binds the native-key action to this deployment's OIDC client.
func (h *LoginUIHandlers) SetNativeWebClientID(clientID string) { h.nativeWebClientID = clientID }

// keyRegistry is what the sufficiency decision consults to treat an enrolled
// U2F as collectible (loginclient.KeyRegistry). nil without a validated
// relying party: no key is ever offered, so none can count (fail closed).
func (h *LoginUIHandlers) keyRegistry() loginclient.KeyRegistry {
	if h.webauthnRPID == "" {
		return nil
	}
	return h.keys.Contains
}

// SetWebAuthnRPID binds the boot-validated relying-party ID
// (config.ValidatedWebAuthnRPID). Left empty, no security key is offered.
func (h *LoginUIHandlers) SetWebAuthnRPID(rpID string) { h.webauthnRPID = rpID }

const (
	defaultSecurityKeyName  = "Security key"
	maxSecurityKeyNameRunes = 64
)

// validate checks what each enrolment factor requires and returns the key
// name to record (security_key only). Any factor other than totp and
// security_key is a 400, never a credential refusal: it says nothing about a
// login, only that the caller sent a value this endpoint does not support.
func (r enrollRequest) validate() (string, error) {
	switch r.Factor {
	case "totp":
		if r.Code == "" {
			return "", errors.New("code is required for totp")
		}
		return "", nil
	case "security_key":
		if len(r.Credential) == 0 || string(r.Credential) == "null" {
			return "", errors.New("credential is required for security_key")
		}
		return securityKeyName(r.Name)
	default:
		return "", fmt.Errorf("unsupported factor %q", r.Factor)
	}
}

// securityKeyName is the label Zitadel stores for the key: trimmed, at most
// 64 characters, no control characters, and "Security key" when blank.
func securityKeyName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return defaultSecurityKeyName, nil
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxSecurityKeyNameRunes {
		return "", fmt.Errorf("name must be at most %d characters", maxSecurityKeyNameRunes)
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", errors.New("name must not contain control characters")
	}
	return name, nil
}

// validate checks what each factor requires: a code for totp, a non-null
// assertion for security_key. Any other factor is a 400.
func (r factorRequest) validate() error {
	switch r.Factor {
	case "totp":
		if r.Code == "" {
			return errors.New("code is required for totp")
		}
	case "security_key":
		if len(r.Assertion) == 0 || string(r.Assertion) == "null" {
			return errors.New("assertion is required for security_key")
		}
	default:
		return fmt.Errorf("unsupported factor %q", r.Factor)
	}
	return nil
}

// checkSecurityKey is /factor's security-key branch (#1898): Zitadel checks
// the browser's assertion against the session's WebAuthn challenge, and a
// success finishes through exactly the TOTP tail (completeVerifiedFactor).
// Only an attempt a key challenge was issued to may assert: one from /enroll
// (SecurityKeyID is the just-registered key, Enrolling still true) or a
// sign-in with a Helivanta-registered key (securityKeySignIn). Any other attempt is
// refused before any round trip, indistinguishably from a wrong code (spec
// D4's rule for the TOTP pair). A refused or cancelled assertion spends one
// of the five guesses, as a wrong code does.
func (h *LoginUIHandlers) checkSecurityKey(c *gin.Context, start time.Time, authRequestID string, attempt loginAttempt, assertion json.RawMessage) {
	if attempt.SecurityKeyID == "" || h.webauthnRPID == "" {
		h.bumpFactorAttempt(c, authRequestID, errAttemptInOtherFlow, start)
		return
	}
	verified, err := h.client.CheckSecurityKey(c.Request.Context(),
		loginclient.Session{ID: attempt.SessionID, Token: attempt.SessionToken}, assertion)
	if err != nil {
		if loginclient.IsCredentialRefusal(err) {
			h.bumpFactorAttempt(c, authRequestID, err, start)
			return
		}
		h.respondLoginClientError(c, err, "check_security_key")
		return
	}
	h.completeVerifiedFactor(c, authRequestID, verified)
}

// securityKeyRequestWire relays Zitadel's WebAuthn request options verbatim.
type securityKeyRequestWire struct {
	RequestOptions json.RawMessage `json:"request_options"`
}

type securityKeyChallengeResponse struct {
	SecurityKey securityKeyRequestWire `json:"security_key"`
}

// securityKeySignIn is the SecurityKeyID a sign-in attempt carries when the
// password step challenged the user's Helivanta-registered keys (#1898). It
// is not a Zitadel u2fId (those are numeric): it marks that a challenge was
// issued, so checkSecurityKey admits the assertion; which key answers is
// Zitadel's to verify. /enroll refuses it (Enrolling is false).
const securityKeySignIn = "registered"

// enrollSecurityKey confirms the key the password step registered (#1898):
// Zitadel verifies the browser's attestation, the key is recorded in the
// registry, and the session gets a WebAuthn challenge the page asserts next
// on /factor. The order is the recovery argument: the registry row is
// written before any challenge exists, and a retry that finds the row skips
// the verify (Zitadel would refuse a second verify of the same key) and only
// issues a fresh challenge. A retry after a failed registry write finds the
// key READY in Zitadel and records it without verifying again. Refusals and the five-guess budget are exactly
// the TOTP enrolment's.
func (h *LoginUIHandlers) enrollSecurityKey(c *gin.Context, start time.Time, authRequestID string, attempt loginAttempt, credential json.RawMessage, name string) {
	ctx := c.Request.Context()
	if !attempt.Enrolling || attempt.SecurityKeyID == "" || h.webauthnRPID == "" {
		// No key was offered to this attempt: refused like a wrong
		// credential, before any round trip (spec D4's rule for the TOTP pair).
		h.bumpFactorAttempt(c, authRequestID, errAttemptInOtherFlow, start)
		return
	}
	subject, err := h.client.SessionUserID(ctx, attempt.SessionID)
	if err != nil {
		h.respondLoginClientError(c, err, "security_key_subject")
		return
	}
	recorded, err := h.keys.Contains(ctx, subject, []string{attempt.SecurityKeyID})
	if err != nil {
		// Fail closed as retryable: an unreadable registry must not become a
		// second verify (refused by Zitadel, so a spent guess) or a success.
		h.respondLoginClientError(c, fmt.Errorf("%w: %w", err, loginclient.ErrUnavailable), "security_key_registry")
		return
	}
	if !recorded {
		// Recovery: a verify that succeeded before an earlier registry write
		// failed leaves the key READY in Zitadel. Verifying it again would be
		// refused and spend a guess, so record it without a second verify.
		// Safe: this attempt's own password-checked session registered the id.
		ready, err := h.client.ReadySecurityKeyIDs(ctx, attempt.SessionID)
		if err != nil {
			h.respondLoginClientError(c, err, "security_key_ready")
			return
		}
		if !slices.Contains(ready, attempt.SecurityKeyID) {
			if err := h.client.VerifySecurityKey(ctx, attempt.SessionID, attempt.SecurityKeyID, credential, name); err != nil {
				if loginclient.IsCredentialRefusal(err) {
					h.bumpFactorAttempt(c, authRequestID, err, start)
					return
				}
				h.respondLoginClientError(c, err, "verify_security_key")
				return
			}
		}
		if err := h.keys.Add(ctx, subject, attempt.SecurityKeyID, h.webauthnRPID); err != nil {
			// Zitadel holds a verified key Helivanta has not recorded; a key
			// absent from the registry is never accepted later, so this
			// fails closed. Never a success.
			h.respondLoginClientError(c, fmt.Errorf("%w: %w", err, loginclient.ErrUnavailable), "security_key_registry")
			return
		}
	}
	challenged, options, err := h.client.ChallengeSecurityKey(ctx,
		loginclient.Session{ID: attempt.SessionID, Token: attempt.SessionToken}, h.webauthnRPID)
	if err != nil {
		h.respondLoginClientError(c, err, "challenge_security_key")
		return
	}
	// The challenge rotated the token; the stored one must be the one that
	// works for the assertion, as on the TOTP path.
	if err := h.store.UpdateToken(ctx, authRequestID, challenged.Token); err != nil {
		respond.InternalErr(c, err, "sign-in could not be completed")
		return
	}
	audit.Log(ctx, requestid.Logger(c), slog.LevelInfo, "auth.login.security_key_enrolled", "login security key recorded",
		"auth_request_id", authRequestID, "outcome", "security_key_challenge")
	body := securityKeyChallengeResponse{}
	body.SecurityKey.RequestOptions = options
	respond.OK(c, body)
}
