package loginclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests pin #856
// (docs/superpowers/specs/2026-10-08-password-change-required-design.md): a
// password that must change — flagged by an administrator, or older than the
// org's expiry policy — is never finalized past, every unreadable answer fails
// closed, the gate never runs before a factor, and ChangePassword sends exactly
// what Zitadel's set-password API expects.

const (
	pcOrgID  = "org-856-distinctive"
	pcUserID = "user-856"
)

// changedAt is the passwordChanged every expiry test is measured from.
var changedAt = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// pcZitadel is a fake Zitadel for the password-change paths, every answer
// under the test's control and every request the gate makes recorded. A field
// left empty answers 404, so a test that forgets a route fails closed rather
// than silently passing.
type pcZitadel struct {
	methods      string // authMethodTypes JSON array
	totpVerified bool
	loginPolicy  string
	orgID        string

	userStatus int
	user       string
	expiry     string
	complexity string

	setPasswordStatus int
	setPasswordBody   string

	mu              sync.Mutex
	paths           []string
	settingsQueries map[string]string
	settingsHeaders map[string]string
	setPasswordReq  map[string]any
	setPasswordPath string
	finalized       bool
}

func newPCZitadel() *pcZitadel {
	return &pcZitadel{
		methods:     `["AUTHENTICATION_METHOD_TYPE_PASSWORD"]`,
		loginPolicy: nonForceMFAPolicy,
		orgID:       pcOrgID,
		userStatus:  http.StatusOK,
		user:        userJSON(`"passwordChanged":"` + changedAt.Format(time.RFC3339) + `"`),
		expiry:      noPasswordExpiryJSON,
		complexity:  `{"settings":{"minLength":"12","requiresUppercase":true,"requiresLowercase":true,"requiresNumber":true}}`,
	}
}

// userJSON builds GET /v2/users/{id} with the given human fields.
func userJSON(humanFields string) string {
	return `{"user":{"userId":"` + pcUserID + `","state":"USER_STATE_ACTIVE","human":{"profile":{"givenName":"A"}` +
		map[bool]string{true: ",", false: ""}[humanFields != ""] + humanFields + `}}}`
}

func (z *pcZitadel) requested(path string) bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	for _, p := range z.paths {
		if p == path {
			return true
		}
	}
	return false
}

func (z *pcZitadel) wasFinalized() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.finalized
}

func (z *pcZitadel) client(t *testing.T, now time.Time) *Client {
	t.Helper()
	z.settingsQueries = map[string]string{}
	z.settingsHeaders = map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		z.mu.Lock()
		z.paths = append(z.paths, r.URL.Path)
		z.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			factors := `"user":{"id":"` + pcUserID + `","organizationId":"` + z.orgID + `"},"password":{"verifiedAt":"t"}`
			if z.totpVerified {
				factors += `,"totp":{"verifiedAt":"t"}`
			}
			_, _ = w.Write([]byte(`{"session":{"id":"s","factors":{` + factors + `}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/users/"+pcUserID+"/authentication_methods":
			_, _ = w.Write([]byte(`{"authMethodTypes":` + z.methods + `}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/users/"+pcUserID:
			w.WriteHeader(z.userStatus)
			_, _ = w.Write([]byte(z.user))
		case r.Method == http.MethodGet && r.URL.Path == "/management/v1/policies/login":
			_, _ = w.Write([]byte(z.loginPolicy))
		case r.Method == http.MethodGet && (r.URL.Path == expirySettingsPath || r.URL.Path == complexitySettingsPath):
			z.mu.Lock()
			z.settingsQueries[r.URL.Path] = r.URL.Query().Get("ctx.orgId")
			z.settingsHeaders[r.URL.Path] = r.Header.Get("x-zitadel-orgid")
			z.mu.Unlock()
			body := z.expiry
			if r.URL.Path == complexitySettingsPath {
				body = z.complexity
			}
			if body == "" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(body))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/users/"+pcUserID+"/password":
			raw, _ := io.ReadAll(r.Body)
			z.mu.Lock()
			z.setPasswordPath = r.URL.Path
			_ = json.Unmarshal(raw, &z.setPasswordReq)
			z.mu.Unlock()
			if z.setPasswordStatus != 0 {
				w.WriteHeader(z.setPasswordStatus)
			}
			body := z.setPasswordBody
			if body == "" {
				body = `{"details":{}}`
			}
			_, _ = w.Write([]byte(body))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v2/oidc/auth_requests/"):
			z.mu.Lock()
			z.finalized = true
			z.mu.Unlock()
			_, _ = w.Write([]byte(`{"callbackUrl":"https://hms.test/api/auth/callback?code=c&state=s"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "pat", srv.Client())
	c.now = func() time.Time { return now }
	return c
}

var pcSession = Session{ID: "s", Token: "t"}

// --- the gate on CompleteIfSufficient -------------------------------------

func TestPasswordChangeRequiredIsDueAndNeverFinalized(t *testing.T) {
	z := newPCZitadel()
	z.user = userJSON(`"passwordChangeRequired":true,"passwordChanged":"` + changedAt.Format(time.RFC3339) + `"`)
	c := z.client(t, changedAt.Add(time.Hour))

	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", pcSession)
	require.NoError(t, err)
	require.Equal(t, OutcomePasswordChangeRequired, res.Outcome)
	require.Equal(t, PasswordChangeRequired, res.PasswordChange.Reason)
	require.Equal(t, PasswordComplexity{MinLength: 12, RequiresUppercase: true, RequiresLowercase: true, RequiresNumber: true},
		res.PasswordChange.Policy)
	require.Empty(t, res.CallbackURL)
	require.False(t, z.wasFinalized(), "finalize was called for a user whose password must change: #856's bypass")
	require.False(t, z.requested(expirySettingsPath), "the flag alone decides; the expiry policy need not be read")
}

func TestPasswordNotRequiredAndNotExpiredCompletes(t *testing.T) {
	z := newPCZitadel()
	c := z.client(t, changedAt.Add(time.Hour))

	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", pcSession)
	require.NoError(t, err)
	require.Equal(t, OutcomeComplete, res.Outcome)
	require.True(t, z.wasFinalized())
	require.True(t, z.requested("/v2/users/"+pcUserID), "the password gate did not read the user")
	require.False(t, z.requested(complexitySettingsPath), "the complexity policy is read only when a change is due")
}

func TestPasswordExpiryElapsedIsDue(t *testing.T) {
	for name, expiry := range map[string]string{
		"maxAgeDays as a protojson string": `{"settings":{"maxAgeDays":"90"}}`,
		"maxAgeDays as a JSON number":      `{"settings":{"maxAgeDays":90}}`,
	} {
		t.Run(name, func(t *testing.T) {
			z := newPCZitadel()
			z.expiry = expiry
			c := z.client(t, changedAt.AddDate(0, 0, 90).Add(time.Second))

			res, err := c.CompleteIfSufficient(context.Background(), "V2_1", pcSession)
			require.NoError(t, err)
			require.Equal(t, OutcomePasswordChangeRequired, res.Outcome)
			require.Equal(t, PasswordChangeExpired, res.PasswordChange.Reason)
			require.False(t, z.wasFinalized())
		})
	}
}

// Zitadel's own rule is strictly after passwordChanged + maxAgeDays; the
// instant itself, and anything before it, is not expired.
func TestPasswordExpiryNotYetElapsedCompletes(t *testing.T) {
	for name, now := range map[string]time.Time{
		"a day before":      changedAt.AddDate(0, 0, 89),
		"exactly at expiry": changedAt.AddDate(0, 0, 90),
	} {
		t.Run(name, func(t *testing.T) {
			z := newPCZitadel()
			z.expiry = `{"settings":{"maxAgeDays":"90"}}`
			c := z.client(t, now)

			res, err := c.CompleteIfSufficient(context.Background(), "V2_1", pcSession)
			require.NoError(t, err)
			require.Equal(t, OutcomeComplete, res.Outcome)
		})
	}
}

func TestNoExpiryPolicyNeverExpires(t *testing.T) {
	z := newPCZitadel()
	c := z.client(t, changedAt.AddDate(30, 0, 0))

	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", pcSession)
	require.NoError(t, err)
	require.Equal(t, OutcomeComplete, res.Outcome)
}

// Zitadel's own rule: no recorded passwordChanged is not expired — and then
// there is nothing to compare an expiry policy against, so it is not read.
func TestNoPasswordChangedIsNotExpiredAndSkipsTheExpiryRead(t *testing.T) {
	z := newPCZitadel()
	z.user = userJSON("")
	z.expiry = `{"settings":{"maxAgeDays":"1"}}`
	c := z.client(t, changedAt.AddDate(5, 0, 0))

	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", pcSession)
	require.NoError(t, err)
	require.Equal(t, OutcomeComplete, res.Outcome)
	require.False(t, z.requested(expirySettingsPath))
}

// Both settings reads are scoped to the SESSION's org, by query parameter AND
// header, so neither can silently fall back to the PAT's own org (#913's bug
// class).
func TestPasswordSettingsReadsAreScopedToTheSessionsOrg(t *testing.T) {
	z := newPCZitadel()
	z.expiry = `{"settings":{"maxAgeDays":"90"}}`
	c := z.client(t, changedAt.AddDate(1, 0, 0))

	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", pcSession)
	require.NoError(t, err)
	require.Equal(t, OutcomePasswordChangeRequired, res.Outcome)
	for _, path := range []string{expirySettingsPath, complexitySettingsPath} {
		require.Equal(t, pcOrgID, z.settingsQueries[path], "%s ctx.orgId", path)
		require.Equal(t, pcOrgID, z.settingsHeaders[path], "%s x-zitadel-orgid", path)
	}
}

// Every answer the gate cannot interpret is a retryable ErrUnavailable — never
// a completed login and never "no change due".
func TestPasswordGateFailsClosed(t *testing.T) {
	cases := map[string]func(z *pcZitadel){
		"user read 500":                     func(z *pcZitadel) { z.userStatus = http.StatusInternalServerError },
		"user vanished (404)":               func(z *pcZitadel) { z.userStatus = http.StatusNotFound },
		"no human object":                   func(z *pcZitadel) { z.user = `{"user":{"userId":"u","state":"USER_STATE_ACTIVE"}}` },
		"human is null":                     func(z *pcZitadel) { z.user = `{"user":{"human":null}}` },
		"renamed passwordChangeRequired":    func(z *pcZitadel) { z.user = userJSON(`"password_change_required":true`) },
		"re-cased passwordChangeRequired":   func(z *pcZitadel) { z.user = userJSON(`"PasswordChangeRequired":true`) },
		"passwordChangeRequired not a bool": func(z *pcZitadel) { z.user = userJSON(`"passwordChangeRequired":"true"`) },
		"passwordChanged unparsable":        func(z *pcZitadel) { z.user = userJSON(`"passwordChanged":"yesterday"`) },
		"passwordChanged not a string":      func(z *pcZitadel) { z.user = userJSON(`"passwordChanged":1700000000`) },
		"expiry read 500":                   func(z *pcZitadel) { z.expiry = "" },
		"expiry without settings":           func(z *pcZitadel) { z.expiry = `{"details":{}}` },
		"maxAgeDays not numeric":            func(z *pcZitadel) { z.expiry = `{"settings":{"maxAgeDays":"ninety"}}` },
		"maxAgeDays negative":               func(z *pcZitadel) { z.expiry = `{"settings":{"maxAgeDays":-1}}` },
		"maxAgeDays fractional":             func(z *pcZitadel) { z.expiry = `{"settings":{"maxAgeDays":1.5}}` },
		"maxAgeDays renamed":                func(z *pcZitadel) { z.expiry = `{"settings":{"max_age_days":"1"}}` },
		"session without an org id":         func(z *pcZitadel) { z.orgID = "" },
		"complexity unreadable when due": func(z *pcZitadel) {
			z.user = userJSON(`"passwordChangeRequired":true`)
			z.complexity = ""
		},
		"complexity without settings when due": func(z *pcZitadel) {
			z.user = userJSON(`"passwordChangeRequired":true`)
			z.complexity = `{}`
		},
		"complexity bool renamed when due": func(z *pcZitadel) {
			z.user = userJSON(`"passwordChangeRequired":true`)
			z.complexity = `{"settings":{"minLength":"8","requires_symbol":true}}`
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			z := newPCZitadel()
			// An org with an expiry policy, so the expiry read is reached.
			z.expiry = `{"settings":{"maxAgeDays":"3650"}}`
			mutate(z)
			c := z.client(t, changedAt.Add(time.Hour))

			res, err := c.CompleteIfSufficient(context.Background(), "V2_1", pcSession)
			require.ErrorIs(t, err, ErrUnavailable)
			require.Equal(t, Result{}, res)
			require.False(t, z.wasFinalized(), "finalize was called on an unreadable password state")
		})
	}
}

// --- placement: the gate never precedes a factor (spec D2) ----------------

func TestPasswordGateNeverRunsBeforeTheSecondFactor(t *testing.T) {
	z := newPCZitadel()
	z.methods = `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]`
	z.user = userJSON(`"passwordChangeRequired":true`)
	c := z.client(t, changedAt)

	res, err := c.CompleteIfSufficient(context.Background(), "V2_1", pcSession)
	require.NoError(t, err)
	require.Equal(t, OutcomeFactorRequired, res.Outcome,
		"a password change was offered before TOTP: someone holding only the password could rotate it")
	require.False(t, z.requested("/v2/users/"+pcUserID), "the password state was read before the factor was proven")
}

// A step the factor decision still needs — an uncollectible factor to
// refuse, or (#948) a TOTP to enrol — outranks a due change: the change is
// asked for only once every factor is proven, and never instead of one.
func TestFactorDecisionsOutrankADueChange(t *testing.T) {
	cases := map[string]struct {
		methods string
		policy  string
		outcome Outcome
		reason  RefusalReason
	}{
		"forced MFA, nothing enrolled": {`["AUTHENTICATION_METHOD_TYPE_PASSWORD"]`, forceMFAPolicy, OutcomeEnrollmentRequired, RefusalUnspecified},
		"uncollectible factor":         {`["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_U2F"]`, nonForceMFAPolicy, OutcomeRefused, RefusalFactorUnsupported},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			z := newPCZitadel()
			z.methods, z.loginPolicy = tc.methods, tc.policy
			z.user = userJSON(`"passwordChangeRequired":true`)
			c := z.client(t, changedAt)

			res, err := c.CompleteIfSufficient(context.Background(), "V2_1", pcSession)
			require.NoError(t, err)
			require.Equal(t, tc.outcome, res.Outcome)
			require.Equal(t, tc.reason, res.Reason)
			require.False(t, z.wasFinalized())
			require.False(t, z.requested("/v2/users/"+pcUserID), "the password state was read before the factor decision finished")
		})
	}
}

func TestCompleteAfterFactorAsksForADueChangeAfterAVerifiedTOTP(t *testing.T) {
	z := newPCZitadel()
	z.methods = `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]`
	z.totpVerified = true
	z.user = userJSON(`"passwordChangeRequired":true`)
	c := z.client(t, changedAt)

	res, err := c.CompleteAfterFactor(context.Background(), "V2_1", pcSession)
	require.NoError(t, err)
	require.Equal(t, OutcomePasswordChangeRequired, res.Outcome)
	require.Equal(t, PasswordChangeRequired, res.PasswordChange.Reason)
	require.False(t, z.wasFinalized(), "finalize was called after TOTP for a user whose password must change")
}

func TestCompleteAfterFactorWithoutAVerifiedTOTPNeverOffersAChange(t *testing.T) {
	z := newPCZitadel()
	z.methods = `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]`
	z.user = userJSON(`"passwordChangeRequired":true`)
	c := z.client(t, changedAt)

	res, err := c.CompleteAfterFactor(context.Background(), "V2_1", pcSession)
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, res.Outcome)
	require.Equal(t, RefusalFactorNotVerified, res.Reason)
}

// --- ChangePassword ---------------------------------------------------------

func TestChangePasswordSendsTheCurrentPasswordVerification(t *testing.T) {
	z := newPCZitadel()
	c := z.client(t, changedAt)

	require.NoError(t, c.ChangePassword(context.Background(), pcSession, "Old-pass-1", "New-pass-12345"))
	require.Equal(t, "/v2/users/"+pcUserID+"/password", z.setPasswordPath, "the user id must come from the held session")
	require.Equal(t, map[string]any{
		"newPassword":     map[string]any{"password": "New-pass-12345", "changeRequired": false},
		"currentPassword": "Old-pass-1",
	}, z.setPasswordReq)
}

// Refused locally because Zitadel answers it with a 500 AND a lockout strike
// (see ErrPasswordUnchanged).
func TestChangePasswordRefusesAnUnchangedPasswordWithoutCallingZitadel(t *testing.T) {
	z := newPCZitadel()
	c := z.client(t, changedAt)

	err := c.ChangePassword(context.Background(), pcSession, "Same-pass-1", "Same-pass-1")
	require.ErrorIs(t, err, ErrPasswordUnchanged)
	require.Empty(t, z.paths, "Zitadel was called for an unchanged password")
}

func TestChangePasswordRefusesAnOverlongPasswordWithoutCallingZitadel(t *testing.T) {
	z := newPCZitadel()
	c := z.client(t, changedAt)

	err := c.ChangePassword(context.Background(), pcSession, "Old-pass-1", strings.Repeat("é", maxPasswordLength+1))
	require.ErrorIs(t, err, ErrPasswordPolicy)
	require.Equal(t, PasswordRuleMaxLength, PasswordPolicyRule(err))
	require.Empty(t, z.paths)

	// Exactly the limit, counted in characters rather than bytes, is sent.
	require.NoError(t, c.ChangePassword(context.Background(), pcSession, "Old-pass-1", strings.Repeat("é", maxPasswordLength)))
}

func TestChangePasswordNamesTheComplexityRuleZitadelRefused(t *testing.T) {
	for id, rule := range map[string]string{
		"DOMAIN-HuJf6": PasswordRuleMinLength,
		"DOMAIN-co3Xw": PasswordRuleLowercase,
		"DOMAIN-VoaRj": PasswordRuleUppercase,
		"DOMAIN-ZBv4H": PasswordRuleNumber,
		"DOMAIN-ZDLwA": PasswordRuleSymbol,
	} {
		t.Run(id, func(t *testing.T) {
			z := newPCZitadel()
			z.setPasswordStatus = http.StatusBadRequest
			z.setPasswordBody = `{"code":3,"message":"Errors.User.PasswordComplexityPolicy (` + id + `)","details":[{"id":"` + id + `"}]}`
			c := z.client(t, changedAt)

			err := c.ChangePassword(context.Background(), pcSession, "Old-pass-1", "new")
			require.ErrorIs(t, err, ErrPasswordPolicy)
			require.Equal(t, rule, PasswordPolicyRule(err))
			require.Equal(t, id, ZitadelErrorID(err), "the Zitadel id must survive for the log line")
			require.False(t, IsCredentialRefusal(err), "a policy refusal is not a credential refusal")
		})
	}
}

func TestChangePasswordMapsZitadelRefusals(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   error
	}{
		"wrong current password": {http.StatusBadRequest, `{"details":[{"id":"COMMAND-3M0fs"}]}`, ErrBadCredentials},
		"locked by this check":   {http.StatusBadRequest, `{"details":[{"id":"COMMAND-SFA3t"}]}`, ErrAccountLocked},
		"unrecognised 400":       {http.StatusBadRequest, `{"details":[{"id":"COMMAND-G8dh3"}]}`, ErrRejected},
		"zitadel down":           {http.StatusInternalServerError, `{}`, ErrUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			z := newPCZitadel()
			z.setPasswordStatus, z.setPasswordBody = tc.status, tc.body
			c := z.client(t, changedAt)

			err := c.ChangePassword(context.Background(), pcSession, "Old-pass-1", "New-pass-12345")
			require.ErrorIs(t, err, tc.want)
			require.False(t, errors.Is(err, ErrPasswordPolicy))
			require.Empty(t, PasswordPolicyRule(err))
		})
	}
}

// --- CompleteAfterPasswordChange (spec D5) ---------------------------------

func TestCompleteAfterPasswordChange(t *testing.T) {
	totp := `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]`
	cases := map[string]struct {
		setup     func(z *pcZitadel)
		outcome   Outcome
		reason    RefusalReason
		finalized bool
	}{
		"password-only, change took effect": {
			setup: func(*pcZitadel) {}, outcome: OutcomeComplete, finalized: true,
		},
		"TOTP enrolled and verified": {
			setup:   func(z *pcZitadel) { z.methods, z.totpVerified = totp, true },
			outcome: OutcomeComplete, finalized: true,
		},
		"TOTP enrolled but not verified on this session": {
			setup:   func(z *pcZitadel) { z.methods = totp },
			outcome: OutcomeRefused, reason: RefusalFactorNotVerified,
		},
		"org forces MFA and nothing is enrolled": {
			setup:   func(z *pcZitadel) { z.loginPolicy = forceMFAPolicy },
			outcome: OutcomeRefused, reason: RefusalFactorNotVerified,
		},
		"an uncollectible factor": {
			setup: func(z *pcZitadel) {
				z.methods = `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_PASSKEY"]`
			},
			outcome: OutcomeRefused, reason: RefusalFactorUnsupported,
		},
		"the change is still due": {
			setup:   func(z *pcZitadel) { z.user = userJSON(`"passwordChangeRequired":true`) },
			outcome: OutcomeRefused, reason: RefusalPasswordChangeUnconfirmed,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			z := newPCZitadel()
			tc.setup(z)
			c := z.client(t, changedAt.Add(time.Hour))

			res, err := c.CompleteAfterPasswordChange(context.Background(), "V2_1", pcSession)
			require.NoError(t, err)
			require.Equal(t, tc.outcome, res.Outcome)
			require.Equal(t, tc.reason, res.Reason)
			require.Equal(t, tc.finalized, z.wasFinalized())
			if tc.finalized {
				require.NotEmpty(t, res.CallbackURL)
			}
		})
	}
}

func TestCompleteAfterPasswordChangeFailsClosedOnAnUnreadablePasswordState(t *testing.T) {
	z := newPCZitadel()
	z.userStatus = http.StatusServiceUnavailable
	c := z.client(t, changedAt)

	res, err := c.CompleteAfterPasswordChange(context.Background(), "V2_1", pcSession)
	require.ErrorIs(t, err, ErrUnavailable)
	require.Equal(t, Result{}, res)
	require.False(t, z.wasFinalized())
}

func TestPasswordChangeNames(t *testing.T) {
	require.Equal(t, "password_change_required", OutcomePasswordChangeRequired.String())
	require.Equal(t, "password_change_unconfirmed", RefusalPasswordChangeUnconfirmed.String())
	require.Equal(t, "required", PasswordChangeRequired.String())
	require.Equal(t, "expired", PasswordChangeExpired.String())
}

func TestExpiredNeverOverflowsIntoThePast(t *testing.T) {
	require.False(t, expired(changedAt, 1<<40, changedAt.AddDate(100, 0, 0)))
}
