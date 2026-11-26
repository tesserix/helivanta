# Native security keys, slices 1–2: register a key during required setup and sign in with it

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A user whose organisation requires two-step verification and who has nothing enrolled can choose a security key on Helivanta's sign-in page, register it with helivanta.app as the relying party, and finish signing in with that key. Later sign-ins with a Helivanta-registered key also happen on Helivanta's page. No Zitadel page is involved. The spec's slices 1 and 2 are one plan here, because a key that can be registered but not used at the next sign-in cannot ship on its own.

**Architecture:** The password step's `enrollment_required` answer gains a second option, alongside the TOTP secret that is already there. That option is the `publicKeyCredentialCreationOptions` from `POST /v2/users/{id}/u2f`, using the configured RP ID. `POST /v1/auth/login/enroll` with `factor: "security_key"` relays the browser's attestation to `POST /v2/users/{id}/u2f/{u2fId}`. It records the key in a new IAM registry, then returns a WebAuthn challenge on the same session. The page asserts, and `POST /v1/auth/login/factor` with `kind: "security_key"` relays the assertion as `checks.webAuthN`. Then the existing finalize path runs. Zitadel verifies every signature, and Helivanta only relays.

**Tech Stack:** Go (gin, GORM, the `loginclient` package), Next 16 shell (`apps/shell/app/login/page.tsx`, `apps/shell/lib/login-client.ts`), Playwright with the CDP WebAuthn virtual authenticator, and dev Zitadel v4.19.5.

**Spec:** `docs/superpowers/specs/2026-11-27-native-security-key-design.md` (D1, D3 required-setup path, D4 registry, D6; S1 result).

## Global Constraints

- RP ID from `WEBAUTHN_RP_ID`, validated at boot. In production it must be exactly `helivanta.app`, and in dev exactly `helivanta.localhost`. Any other value is a boot failure (spec D1, S1(3): Zitadel accepts any domain, so this is the only control).
- No request field, header or query can choose the RP ID.
- Zitadel verifies attestations and assertions. Helivanta never parses or verifies WebAuthn signatures (anti-goal).
- No Zitadel `/ui/` URL in any response this slice adds.
- Every login handler keeps its existing floor timing, rate-limit bucket and equalised refusals. A wrong or cancelled key spends one factor attempt (spec D2).
- New user-facing strings need product-owner approval before merge (see Task 5).
- No new `Permissions-Policy` entry and no new CSP origin (spec D6).
- Migration ID `0057_iam_security_keys`, append-only.

## Review Focus

1. **A replayed or second `/enroll` for the same attempt** after a key was registered should not register a second key. It should answer like any repeated enrolment (Task 3 test `TestEnrollSecurityKeyTwiceRegistersOnce`).
2. **Zitadel accepts registration but the registry insert fails.** The user must not be left with an unrecorded key that D4 would later treat as legacy. Record before answering, and fail with 503 so the attempt is retried (Task 3 test `TestEnrollSecurityKeyRegistryFailureIs503`).
3. **A browser without WebAuthn, or one that cancels the prompt.** The page offers the authenticator app instead and never falls through to a password-only session (Task 5 tests).
4. **Assertion posted on the wrong route or for a TOTP attempt.** It is refused before any Zitadel call, indistinguishably from a wrong code (Task 4 test `TestFactorSecurityKeyOnTOTPAttemptRefused`).
5. **A session holding only a verified key, with no password factor**, must not finalize. Password and key are both required (Task 2 test `TestSufficiencyNeedsPasswordWithWebAuthN`).

---

### Task 1: RP ID configuration, validated at boot

**Files:**

- Modify: `backend/internal/config/config.go` (add `WebAuthnRPID string` read from `WEBAUTHN_RP_ID`)
- Create: `backend/internal/config/webauthn.go`
- Test: `backend/internal/config/webauthn_test.go`
- Modify: `backend/cmd/api/main.go` (call the validator next to the other fail-closed boot checks, before any network call)
- Modify: `Makefile` / dev env (export `WEBAUTHN_RP_ID=helivanta.localhost` where `ZITADEL_ISSUER_URL` is exported for the API)
- Modify: `tesserix-k8s/charts/apps/helivanta-api/templates/deployment.yaml` + `values.yaml` (`WEBAUTHN_RP_ID: helivanta.app`). This is a separate tesserix-k8s PR, merged first, with the chart version bumped.

**Interfaces:**

- Produces: `func (c Config) ValidatedWebAuthnRPID() (string, error)`

- [ ] **Step 1: Write the failing test** `TestValidatedWebAuthnRPID`. Table: `{prod,"helivanta.app"}` → ok. `{prod,""}`, `{prod,"helivanta.localhost"}`, `{prod,"auth.tesserix.app"}`, `{prod,"helivanta.app:443"}`, `{prod,"https://helivanta.app"}` → error. `{dev,"helivanta.localhost"}` → ok. `{dev,"helivanta.app"}` → error.
- [ ] **Step 2:** `cd backend && go test ./internal/config/ -run TestValidatedWebAuthnRPID`. Expected: FAIL (undefined).
- [ ] **Step 3:** Implement as an exact match per environment, without parsing. A comment at the decision point argues fail-closed.
- [ ] **Step 4:** Run the test. Expected: PASS. Wire it in `main.go` so that `return err` aborts boot.
- [ ] **Step 5:** Commit `feat(auth): validate the WebAuthn relying party at boot (#1898)`.

### Task 2: loginclient: register, verify and check a security key

**Files:**

- Create: `backend/internal/modules/iam/loginclient/webauthn.go`
- Test: `backend/internal/modules/iam/loginclient/webauthn_test.go`
- Modify: `backend/internal/modules/iam/loginclient/client.go` (`Factors` gains `WebAuthN bool` from `factors.webAuthN`)
- Modify: `backend/internal/modules/iam/loginclient/sufficiency.go` (a verified `WebAuthN` satisfies the second factor exactly as `TOTP` does, and only alongside `Password`)

**Interfaces:**

- Produces:
  - `type SecurityKeyRegistration struct { ID string; CreationOptions json.RawMessage }`
  - `func (c *Client) RegisterSecurityKey(ctx context.Context, sessionID, rpID string) (SecurityKeyRegistration, error)`: `POST /v2/users/{id}/u2f {"domain": rpID}`. It returns `u2fId`, and `publicKeyCredentialCreationOptions` passed through verbatim. A missing field is `ErrUnavailable`.
  - `func (c *Client) VerifySecurityKey(ctx context.Context, sessionID, keyID string, credential json.RawMessage, name string) error`: `POST /v2/users/{id}/u2f/{keyID} {publicKeyCredential, tokenName}`. A 400 maps through the existing classification to a credential refusal.
  - `func (c *Client) ChallengeSecurityKey(ctx context.Context, s Session, rpID string) (Session, json.RawMessage, error)`: `PATCH /v2/sessions/{id}` with `challenges.webAuthN{domain: rpID, userVerificationRequirement: "USER_VERIFICATION_REQUIREMENT_DISCOURAGED"}`. It returns the new token and `publicKeyCredentialRequestOptions`.
  - `func (c *Client) CheckSecurityKey(ctx context.Context, s Session, assertion json.RawMessage) (Session, error)`: `PATCH /v2/sessions/{id}` with `checks.webAuthN.credentialAssertionData`. A 400 is a credential refusal.

- [ ] **Step 1: Write failing tests** against an `httptest` Zitadel, with the request bodies asserted byte for byte:
  - `TestRegisterSecurityKeySendsConfiguredDomainOnly`: the body is exactly `{"domain":"helivanta.localhost"}`, and options are returned unmodified.
  - `TestRegisterSecurityKeyMissingOptionsIsUnavailable`
  - `TestVerifySecurityKeyWrongCredentialIsCredentialRefusal` (400 → `IsCredentialRefusal`)
  - `TestChallengeAndCheckSecurityKey`: the challenge body carries the domain, and the check body nests the assertion under `checks.webAuthN.credentialAssertionData`.
  - `TestSessionFactorsReadsWebAuthN`
  - `TestSufficiencyNeedsPasswordWithWebAuthN`: `{Password:true, WebAuthN:true}` is sufficient. `{WebAuthN:true}` alone is not, and neither is `{Password:true}` when U2F is enrolled.
- [ ] **Step 2:** `go test ./internal/modules/iam/loginclient/ -run 'SecurityKey|WebAuthN'`. Expected: FAIL.
- [ ] **Step 3:** Implement on the existing `c.do` and `sessionSubject` helpers, with the paths and bodies from the S1 result. Nothing from these structs is logged.
- [ ] **Step 4:** Run the tests. Expected: PASS. Then `go test ./internal/modules/iam/loginclient/`. Expected: PASS, including the existing U2F refusal tests, which still hold because a password-only U2F session is still refused.
- [ ] **Step 5:** Commit `feat(auth): relay security-key registration and checks to Zitadel (#1898)`.

### Task 3: key registry and the `/enroll` security-key path

**Files:**

- Create: `backend/internal/modules/iam/security_keys.sql`, and register `{ID: "0057_iam_security_keys", SQL: securityKeysSchema}` in `module.go`
- Create: `backend/internal/modules/iam/security_keys.go` (store)
- Modify: `backend/internal/modules/iam/loginattempt.go` (`SecurityKeyID string`, persisted in a new nullable `security_key_id` column added by the same migration)
- Modify: `backend/internal/modules/iam/loginui.go` (enrolment branch and `Enroll`)
- Test: `backend/internal/modules/iam/loginui_securitykey_enroll_test.go`, plus registry store tests

**Interfaces:**

- Consumes: Task 1 `ValidatedWebAuthnRPID` (injected as `h.webauthnRPID` through a setter beside `SetNativeWebClientID`), and Task 2's four methods.
- Produces:
  - Table `iam_security_keys (subject text, key_id text, rp_id text, registered_at timestamptz, PRIMARY KEY (subject, key_id))`. It is not tenant data, because the key belongs to the person's identity, and it follows the same non-RLS global rule as `login_attempt`. Confirm that `operating_governance.sql`'s `iam\_%` guard trigger attaches, and add a test asserting it.
  - `enrollment_required` response: `{"enrollment_required":["totp","security_key"],"totp":{...},"security_key":{"creation_options":<raw>}}`.
  - `POST /v1/auth/login/enroll` with `{"auth_request_id","factor":"security_key","credential":<raw>}` → `{"security_key":{"request_options":<raw>}}`.

- [ ] **Step 1: Write failing tests:**
  - `TestPasswordEnrollmentOffersSecurityKey`: both options are present, and `security_key_id` is stored on the attempt.
  - `TestEnrollSecurityKeyRecordsThenChallenges`: the Zitadel verify is called once, the registry row exists before the response, and the response carries the request options.
  - `TestEnrollSecurityKeyTwiceRegistersOnce` (Review Focus 1).
  - `TestEnrollSecurityKeyRegistryFailureIs503` (Review Focus 2).
  - `TestEnrollSecurityKeyWrongCredentialSpendsAttempt`: the equalised refusal, with the counter bumped.
  - `TestEnrollRejectsUnknownFactor`: it stays a 400 for anything other than `totp` or `security_key`.
  - `TestSecurityKeyRegistryIsGuarded`: the global-mutation guard trigger exists on `iam_security_keys`.
- [ ] **Step 2:** `go test ./internal/modules/iam/ -run 'SecurityKey|Enroll'`. Expected: FAIL.
- [ ] **Step 3:** Register the key at the password step, next to the TOTP registration, with the same ordering argument: register before the row write. In `Enroll`, branch on `req.Factor`. `Code` becomes optional at bind time and is required per factor.
- [ ] **Step 4:** Run the tests. Expected: PASS. Then `go run ./cmd/contractdocs && go run ./cmd/contractdocs -check`. Expected: clean, with the migration in `docs/api/reference/inventory.json`. If `module.go` changed, re-pin its hash in `docs/qa/staff-operational-access-reviews.json`, then run `node scripts/staff-operational-access-inventory.mjs --check`.
- [ ] **Step 5:** Commit `feat(auth): register security keys during required setup (#1898)`.

### Task 4: `/factor` accepts a security-key assertion

**Files:**

- Modify: `backend/internal/modules/iam/loginui.go` (`Factor`: `kind: "security_key"` with `assertion`)
- Test: `backend/internal/modules/iam/loginui_securitykey_factor_test.go`

**Interfaces:**

- Consumes: Task 2 `CheckSecurityKey`, and Task 3's attempt `SecurityKeyID`.
- Produces: `POST /v1/auth/login/factor {"auth_request_id","kind":"security_key","assertion":<raw>}` → the existing success or refusal shapes.

- [ ] **Step 1: Write failing tests:**
  - `TestFactorSecurityKeyCompletes`: a check followed by the existing finalize gives `callback_url`.
  - `TestFactorSecurityKeyWrongAssertionSpendsAttempt`
  - `TestFactorSecurityKeyOnTOTPAttemptRefused` (Review Focus 4): an attempt without `SecurityKeyID` is refused before any Zitadel call.
  - `TestFactorSecurityKeyZitadelDownIs503`
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implement by reusing `completeWithVerifiedCode`'s tail after the check, factored so that TOTP and the key share it unchanged.
- [ ] **Step 4:** Run the package. Expected: PASS. Also run `make lint-go`.
- [ ] **Step 5:** Commit `feat(auth): finish sign-in with a just-registered security key (#1898)`.

### Task 4b: sign in later with a Helivanta-registered key

**Files:**

- Modify: `backend/internal/modules/iam/loginui.go` (`continueWithSession`, the factor-required branch) and `loginui_securitykey.go`
- Test: `backend/internal/modules/iam/loginui_securitykey_signin_test.go`

**Interfaces:**

- Consumes: Task 2 `ChallengeSecurityKey`, Task 3 registry (`func (r *securityKeys) Registered(ctx, subject string) (bool, error)`), and Task 4's `/factor` path.
- Produces: the password step answers `{"factor_required":["security_key"],"security_key":{"request_options":<raw>}}`, with `"totp"` also listed when TOTP is enrolled. It does this when U2F is enrolled and every U2F key of the user is in the registry.

- [ ] **Step 1: Write failing tests:**
  - `TestPasswordWithRegisteredKeyChallengesNatively`
  - `TestPasswordWithKeyAndTOTPOffersBoth`
  - `TestPasswordWithLegacyKeyKeepsADR0069Continuation`: a U2F key that isn't in the registry still gets `security_key_required` until slice 3. Decide by comparing Zitadel's ready U2F ids (auth-factor search) against the registry.
  - `TestRegistryUnreadableIs503`
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implement. The attempt row stores `SecurityKeyID` as `"registered"` so that Task 4's guard admits the assertion.
- [ ] **Step 4:** Run the package. Expected: PASS.
- [ ] **Step 5:** Commit `feat(auth): sign in with a Helivanta-registered security key (#1898)`.

### Task 5: shell: choose, register and assert on the enrol step

**Files:**

- Modify: `apps/shell/lib/login-client.ts` (`enrollmentRequired` carries `securityKey?: {creationOptions}`, `enrollSecurityKey()` and `checkSecurityKey()`)
- Create: `apps/shell/lib/webauthn.ts` (base64url ↔ ArrayBuffer for the options and the credential JSON, plus `supportsSecurityKeys()`)
- Modify: `apps/shell/app/login/page.tsx` (`EnrollStep`: a choice between "Authenticator app" and "Security key", with the key path calling `navigator.credentials.create`, then `get`)
- Modify: `apps/shell/lib/security-headers.test.ts` (D6 assertion)
- Test: `apps/shell/lib/webauthn.test.ts`, `apps/shell/app/login/login.test.tsx`, `e2e/tests/security-key-enrol.spec.ts`

**Copy for approval (send to the product owner before merge):**

- Choice heading: "Set up two-step verification"
- Options: "Authenticator app" / "Security key"
- Key prompt: "Insert or touch your security key."
- Name field label: "Key name" (default "Security key")
- No WebAuthn: "This browser can't use security keys. Use the authenticator app instead."
- Cancelled or failed: "The security key wasn't registered. Try again, or use the authenticator app."

- [ ] **Step 1: Write failing tests:**
  - `webauthn.test.ts`: the converters round-trip on S1's recorded option shapes, and a non-base64url string throws.
  - `login.test.tsx` (`renderWithProviders`):
    - Choosing the key calls `create` with the server's options, unchanged apart from the conversion.
    - Without `PublicKeyCredential` the key option is disabled and the no-WebAuthn copy is shown.
    - A rejected `create` shows the failure copy and stays on the choice.
    - A rejected `get` stays on the step and never posts a password-only completion.
  - `security-headers.test.ts`: `Permissions-Policy` does not contain `publickey-credentials`.
  - `login.test.tsx`: a `factor_required` answer with `security_key` calls `get` straight away, and offers "Use your authenticator app instead" when `totp` is also listed.
  - `e2e/tests/security-key-enrol.spec.ts`: a CDP virtual authenticator (`WebAuthn.addVirtualAuthenticator`, ctap2, usb, `isUserVerified: true`) and a seed user that requires MFA with nothing enrolled. Add the user to `scripts/seed-dev.mjs` with a forced-MFA login policy on its org, beside the existing `MFA_EMAIL` user. The test signs in, chooses Security key, lands signed in, and then reads Zitadel's auth factors through the API to find one ready U2F. It then signs out, signs in again with the same virtual authenticator, and lands signed in with no Zitadel page in the navigation history.
- [ ] **Step 2:** `pnpm --filter shell test -- login webauthn security-headers`. Expected: FAIL.
- [ ] **Step 3:** Implement with `useApiMutation` per frontend rules. This is not one of the `apps/shell/lib/` auth-lifecycle exceptions, so check how `EnrollStep` already calls `enroll` and match it. Inline errors go through the existing step error slot. Code steps keep focus on the first input.
- [ ] **Step 4:** Run the tests. Expected: PASS. Then `pnpm turbo lint type-check test format:check --filter shell`. Expected: green. The e2e test runs in the CI e2e job.
- [ ] **Step 5:** Commit `feat(login): register a security key on the enrol step (#1898)`.

### Task 6: docs and the slice boundary

**Files:**

- Modify: `docs/superpowers/specs/2026-11-27-native-security-key-design.md` (Slices: slices 1 and 2 are delivered together by this plan; slice 3 deletes the continuation once legacy keys are moved)
- Modify: `docs/api/http-contract.md` or the IAM login section that documents `/enroll` and `/factor` bodies

- [ ] **Step 1:** Update both docs to match Tasks 3–4 exactly.
- [ ] **Step 2:** `pnpm format:check`. Expected: clean.
- [ ] **Step 3:** Commit `docs(auth): record slice 1's security-key contract (#1898)`.

## Not covered by this slice

- Legacy keys on `auth.tesserix.app` keep ADR-0069's continuation until slice 3 moves them.
- Moving legacy keys and deleting ADR-0069's continuation: slice 3.
- Account-page add and remove: slice 4.
- Renaming `rp.name` ("ZITADEL"): decided in this slice's PR review, and a separate tesserix-k8s change if it goes ahead.
