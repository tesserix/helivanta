package loginclient

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// This file is #856: a password Zitadel requires to change — because an
// administrator set human.passwordChangeRequired, or because it is older than
// the org's password expiry policy — must be changed before the login
// completes. Zitadel signals neither condition to a login client anywhere on
// the session or finalize wire (verified live, spike §5); its own login UI
// enforces both client-side (apps/login verify-helper.ts,
// checkPasswordChangeRequired). Helivanta replaced that UI (#947), so the
// check lives here. See
// docs/superpowers/specs/2026-10-08-password-change-required-design.md.

// PasswordChangeReason says why a password must change before the login can
// complete. Its zero value is not a reason; PasswordChange.Reason is set only
// alongside OutcomePasswordChangeRequired.
type PasswordChangeReason int

const (
	passwordChangeReasonUnset PasswordChangeReason = iota
	// PasswordChangeRequired means an administrator set
	// human.passwordChangeRequired (e.g. an imported or reset account).
	PasswordChangeRequired
	// PasswordChangeExpired means the password is older than the org's
	// password expiry policy (maxAgeDays).
	PasswordChangeExpired
)

// String is the stable wire and log name of the reason.
func (r PasswordChangeReason) String() string {
	switch r {
	case PasswordChangeRequired:
		return "required"
	case PasswordChangeExpired:
		return "expired"
	default:
		return fmt.Sprintf("PasswordChangeReason(%d)", int(r))
	}
}

// PasswordComplexity is the org's password complexity policy, read so the
// change form can show what a new password must satisfy. It is advisory to the
// browser only: Zitadel checks the new password against the same policy when
// it is set, and its answer is the one Helivanta enforces (ChangePassword).
type PasswordComplexity struct {
	MinLength         uint64
	RequiresUppercase bool
	RequiresLowercase bool
	RequiresNumber    bool
	RequiresSymbol    bool
}

// PasswordChange is what OutcomePasswordChangeRequired carries: why the
// password must change, and the rules a new one must satisfy.
type PasswordChange struct {
	Reason PasswordChangeReason
	Policy PasswordComplexity
}

// ErrPasswordPolicy means Zitadel (or this package, before ever calling it)
// refused a NEW password because it does not satisfy the password policy.
// It is not a credential refusal — the user has already proven every factor
// — so the handler shows which rule failed and lets them try again.
// PasswordPolicyRule names the rule.
var ErrPasswordPolicy = errors.New("loginclient: rejected by the complexity policy")

// ErrPasswordUnchanged means the new password equals the current one. Refused here,
// BEFORE Zitadel is called, and that ordering is the point: Zitadel's
// ChangePassword hands both to passwap's VerifyAndUpdate, which returns
// ErrPasswordNoChange; zitadel's convertLoginPasswapErr turns that into an
// INTERNAL error (500, COMMAND-CahN2) after pushing a password-check-FAILED
// event that counts toward the user's lockout (verifyPasswordWithLockoutPolicy,
// internal/command/user_human_password.go at 8a54a2a). Sending it would answer
// an honest mistake with an outage and a lockout strike.
var ErrPasswordUnchanged = errors.New("loginclient: new value equals the current one")

// maxPasswordLength is Zitadel's own bound on SetPasswordRequest's password
// fields (validate.rules max_len: 200, proto/zitadel/user/v2/password.proto).
// Checked here so an over-long password is a named policy refusal rather than
// a validation 400 with no id.
const maxPasswordLength = 200

// Password policy rule names, as PasswordPolicyRule reports them.
const (
	PasswordRuleMinLength = "min_length"
	PasswordRuleMaxLength = "max_length"
	PasswordRuleLowercase = "lowercase"
	PasswordRuleUppercase = "uppercase"
	PasswordRuleNumber    = "number"
	PasswordRuleSymbol    = "symbol"
)

// passwordPolicyRules maps Zitadel's complexity error ids
// (PasswordComplexityPolicy.Check, internal/domain/policy_password_complexity.go
// at 8a54a2a) to the rule each one names. badRequestKinds classifies every one
// of them as ErrPasswordPolicy.
var passwordPolicyRules = map[string]string{
	"DOMAIN-HuJf6": PasswordRuleMinLength,
	"DOMAIN-co3Xw": PasswordRuleLowercase,
	"DOMAIN-VoaRj": PasswordRuleUppercase,
	"DOMAIN-ZBv4H": PasswordRuleNumber,
	"DOMAIN-ZDLwA": PasswordRuleSymbol,
}

// passwordPolicyError is ErrPasswordPolicy with the rule that failed. It
// unwraps to ErrPasswordPolicy and, when Zitadel produced it, to the
// ZitadelError too, so the id and status still reach the log line.
type passwordPolicyError struct {
	rule  string
	cause error
}

func (e *passwordPolicyError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%v (rule %s): %v", ErrPasswordPolicy, e.rule, e.cause)
	}
	return fmt.Sprintf("%v (rule %s)", ErrPasswordPolicy, e.rule)
}

func (e *passwordPolicyError) Unwrap() []error {
	if e.cause != nil {
		return []error{ErrPasswordPolicy, e.cause}
	}
	return []error{ErrPasswordPolicy}
}

// PasswordPolicyRule names the rule an ErrPasswordPolicy error failed
// (PasswordRule*), or "" when err is not one or the rule is unknown.
func PasswordPolicyRule(err error) string {
	var pe *passwordPolicyError
	if errors.As(err, &pe) {
		return pe.rule
	}
	return ""
}

// userHumanWhere names the object passwordState guards, for error text.
const userHumanWhere = "GET /v2/users/{id}: user.human"

// passwordState is what GET /v2/users/{id} says about the user's password.
// changed is zero when Zitadel reported no passwordChanged.
type passwordState struct {
	changeRequired bool
	changed        time.Time
}

// passwordState reads GET /v2/users/{id} — the same read UserState makes,
// with the same login-client PAT — for human.passwordChangeRequired and
// human.passwordChanged. Zitadel's v2 GetUserByID queries with
// shouldTriggerBulk=true, so a read made right after a password change sees
// it (spec D1).
//
// Fails closed: every answer this cannot interpret is an ErrUnavailable
// error, never "no change due". A 404 (the user vanished mid-login) is one
// of them — unlike UserState, where a deleted user is a definite "inactive",
// here there is nothing safe to conclude about a password, and the login
// must not complete. passwordChangeRequired is a proto3 bool, elided when
// false, so absent means false; a key that merely differs by case or "_" is
// refused rather than read as false (refuseIfKeyRenamedOrRecased).
func (c *Client) passwordState(ctx context.Context, userID string) (passwordState, error) {
	var wire struct {
		User struct {
			Human map[string]any `json:"human"`
		} `json:"user"`
	}
	path := "/v2/users/" + url.PathEscape(userID)
	if err := c.do(ctx, http.MethodGet, path, nil, &wire, ErrUnavailable); err != nil {
		return passwordState{}, err
	}
	human := wire.User.Human
	if human == nil {
		return passwordState{}, fmt.Errorf("GET %s: 200 without a user.human object: %w", path, ErrUnavailable)
	}
	for _, key := range []string{"passwordChangeRequired", "passwordChanged"} {
		if err := refuseIfKeyRenamedOrRecased(userHumanWhere, human, key); err != nil {
			return passwordState{}, err
		}
	}
	required, err := readOptionalBool(userHumanWhere, human, "passwordChangeRequired")
	if err != nil {
		return passwordState{}, err
	}
	state := passwordState{changeRequired: required}
	if raw, present := human["passwordChanged"]; present {
		text, isString := raw.(string)
		if !isString {
			return passwordState{}, fmt.Errorf("%s.passwordChanged is %T, not a timestamp: %w", userHumanWhere, raw, ErrUnavailable)
		}
		changed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return passwordState{}, fmt.Errorf("%s.passwordChanged %q is not RFC 3339: %w", userHumanWhere, text, ErrUnavailable)
		}
		state.changed = changed
	}
	return state, nil
}

// orgSettings reads one of Zitadel's v2 org-scoped settings objects
// (GET /v2/settings/password/expiry, /complexity) for orgID. The org is sent
// BOTH as the request's ctx.orgId and as the x-zitadel-orgid header, so if
// either is ever ignored the other still scopes the read — an unscoped read
// would judge the user by the login-client PAT's own org, which is #913's bug
// class. An empty orgID refuses before any request, like LoginPolicyForOrg.
// A 200 without a settings object is unreadable, not "all defaults".
func (c *Client) orgSettings(ctx context.Context, path, orgID string) (map[string]any, error) {
	if orgID == "" {
		return nil, fmt.Errorf("loginclient: %s needs the user's org id, refusing an unscoped read: %w", path, ErrUnavailable)
	}
	var wire struct {
		Settings map[string]any `json:"settings"`
	}
	scoped := path + "?ctx.orgId=" + url.QueryEscape(orgID)
	if err := c.do(ctx, http.MethodGet, scoped, nil, &wire, ErrUnavailable, withOrgID(orgID)); err != nil {
		return nil, err
	}
	if wire.Settings == nil {
		return nil, fmt.Errorf("GET %s: 200 without a settings object: %w", path, ErrUnavailable)
	}
	return wire.Settings, nil
}

// readOptionalUint reads a proto3 uint64 from a JSON object: absent is 0
// (proto3 elides a zero), and protojson renders a uint64 as a decimal STRING,
// though a plain JSON number is accepted too. Anything else — a negative or
// fractional number, a non-numeric string, a renamed key — is unreadable.
func readOptionalUint(where string, obj map[string]any, key string) (uint64, error) {
	if err := refuseIfKeyRenamedOrRecased(where, obj, key); err != nil {
		return 0, err
	}
	raw, present := obj[key]
	if !present {
		return 0, nil
	}
	switch v := raw.(type) {
	case string:
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s.%s %q is not an unsigned integer: %w", where, key, v, ErrUnavailable)
		}
		return n, nil
	case float64:
		if v < 0 || v != math.Trunc(v) || v > float64(1<<53) {
			return 0, fmt.Errorf("%s.%s %v is not an unsigned integer: %w", where, key, v, ErrUnavailable)
		}
		return uint64(v), nil
	default:
		return 0, fmt.Errorf("%s.%s is %T, not an unsigned integer: %w", where, key, raw, ErrUnavailable)
	}
}

const (
	expirySettingsPath     = "/v2/settings/password/expiry"
	complexitySettingsPath = "/v2/settings/password/complexity"
)

// passwordMaxAgeDays reads orgID's password expiry policy: how many days a
// password lives (0: forever).
func (c *Client) passwordMaxAgeDays(ctx context.Context, orgID string) (uint64, error) {
	settings, err := c.orgSettings(ctx, expirySettingsPath, orgID)
	if err != nil {
		return 0, err
	}
	return readOptionalUint("GET "+expirySettingsPath+": settings", settings, "maxAgeDays")
}

// passwordComplexity reads orgID's password complexity policy.
func (c *Client) passwordComplexity(ctx context.Context, orgID string) (PasswordComplexity, error) {
	settings, err := c.orgSettings(ctx, complexitySettingsPath, orgID)
	if err != nil {
		return PasswordComplexity{}, err
	}
	where := "GET " + complexitySettingsPath + ": settings"
	var policy PasswordComplexity
	if policy.MinLength, err = readOptionalUint(where, settings, "minLength"); err != nil {
		return PasswordComplexity{}, err
	}
	for key, dst := range map[string]*bool{
		"requiresUppercase": &policy.RequiresUppercase,
		"requiresLowercase": &policy.RequiresLowercase,
		"requiresNumber":    &policy.RequiresNumber,
		"requiresSymbol":    &policy.RequiresSymbol,
	} {
		if err := refuseIfKeyRenamedOrRecased(where, settings, key); err != nil {
			return PasswordComplexity{}, err
		}
		if *dst, err = readOptionalBool(where, settings, key); err != nil {
			return PasswordComplexity{}, err
		}
	}
	return policy, nil
}

// expired reports whether a password changed at changed has outlived
// maxAgeDays as of now — Zitadel's own rule (verify-helper.ts): calendar
// days added to passwordChanged, strictly after. maxAgeDays 0 never expires.
// A maxAgeDays too large for AddDate is treated as never expiring rather than
// overflowing into the past.
func expired(changed time.Time, maxAgeDays uint64, now time.Time) bool {
	if maxAgeDays == 0 {
		return false
	}
	if maxAgeDays > math.MaxInt32 {
		return false
	}
	return now.After(changed.AddDate(0, 0, int(maxAgeDays)))
}

// passwordChangeDue decides whether subject's password must change before the
// login completes (spec D1), and if so, with which rules. Called only from
// the finalizing paths in sufficiency.go, after every factor is proven (spec
// D2).
//
// The expiry policy is read only when the flag is not already set and Zitadel
// recorded a passwordChanged — matching Zitadel's own rule that an absent
// passwordChanged is not expired. The complexity policy is read only when a
// change is due. Every read fails closed (an ErrUnavailable error); the caller
// wraps it with unreadable.
func (c *Client) passwordChangeDue(ctx context.Context, subject sessionSubject) (PasswordChange, bool, error) {
	state, err := c.passwordState(ctx, subject.UserID)
	if err != nil {
		return PasswordChange{}, false, err
	}
	reason := passwordChangeReasonUnset
	switch {
	case state.changeRequired:
		reason = PasswordChangeRequired
	case !state.changed.IsZero():
		maxAgeDays, err := c.passwordMaxAgeDays(ctx, subject.OrgID)
		if err != nil {
			return PasswordChange{}, false, err
		}
		if expired(state.changed, maxAgeDays, c.now()) {
			reason = PasswordChangeExpired
		}
	}
	if reason == passwordChangeReasonUnset {
		return PasswordChange{}, false, nil
	}
	policy, err := c.passwordComplexity(ctx, subject.OrgID)
	if err != nil {
		return PasswordChange{}, false, err
	}
	return PasswordChange{Reason: reason, Policy: policy}, true, nil
}

// ChangePassword sets the password of the user behind session s, proving the
// current password to Zitadel (POST /v2/users/{id}/password with the
// currentPassword verification, spec D4).
//
// The user id comes from the session itself (GET /v2/sessions/{id}), never
// from the caller, so a held session can only ever change its own user's
// password. The currentPassword verification is deliberate: it makes Zitadel
// prove the caller knows the current password, under the user's lockout
// policy — the same proof Zitadel's own login takes before setting one
// (checkSessionAndSetPassword). The PAT's permission path (no verification)
// would let anyone holding the session reference set a password without
// knowing the old one.
//
// changeRequired is sent false: this change satisfies the requirement.
//
// Errors: ErrPasswordUnchanged and an over-long password are refused before
// any request (see ErrPasswordUnchanged). Zitadel's complexity ids are an
// ErrPasswordPolicy error naming the rule (PasswordPolicyRule). A wrong
// current password, a locked user and the rest are the credential sentinels
// classifyStatus already produces (IsCredentialRefusal), and an outage is
// ErrUnavailable.
//
// This changes the password and nothing else: it never finalizes. The caller
// must run CompleteAfterPasswordChange to complete the login.
func (c *Client) ChangePassword(ctx context.Context, s Session, currentPassword, newPassword string) error {
	if newPassword == currentPassword {
		return ErrPasswordUnchanged
	}
	if len([]rune(newPassword)) > maxPasswordLength {
		return &passwordPolicyError{rule: PasswordRuleMaxLength}
	}
	subject, err := c.sessionSubject(ctx, s.ID)
	if err != nil {
		return err
	}
	body := map[string]any{
		"newPassword":     map[string]any{"password": newPassword, "changeRequired": false},
		"currentPassword": currentPassword,
	}
	err = c.do(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(subject.UserID)+"/password", body, nil, ErrUserNotFound)
	if errors.Is(err, ErrPasswordPolicy) {
		return &passwordPolicyError{rule: passwordPolicyRules[ZitadelErrorID(err)], cause: err}
	}
	return err
}
