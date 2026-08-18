# Handoff — 2026-08-17 (evening)

Supersedes `HANDOFF-2026-08-17.md`. Written at the end of a session that merged
four features, renamed the product, and found five security controls that
existed in code while being asserted by nothing.

**The repository is now `tesserix/helivanta`. The local path is
`~/personal/tesserix-new/helivanta`. The Go module is
`github.com/tesserix/helivanta`.**

---

## Where the code is

`main` is green. **M0 — Platform Foundation: 14 closed, 35 open.** No open PRs.

```
dd7ecdb  Native TOTP on our own login page (#867) (#870)
38275fa  @tesserix/web 1.8.0 → 2.2.1 from the public registry (#866) (#868)
5f15bb5  Rebrand: hms → helivanta (#863) (#865)
7c4f25e  Secrets management: boot secrets in OpenBao (#45) (#864)
```

Infra: `tesserix-k8s#365` and `#374` merged. `namespace/helivanta` +
`serviceaccount/helivanta-api` exist on `tesseract-prod-in-gke`;
`SecretStore/openbao-helivanta-api` is `Valid`/`Ready`; the old `hms` namespace
is deleted.

---

## Read this first: green means nothing until something has been mutated

This is the session's single most important finding, and it is not about any one
feature. **Five controls shipped or nearly shipped that existed in code and were
asserted by nothing.** Every one surfaced only by deliberately breaking the code
to see whether a test noticed.

| Control | How it was nominal |
|---|---|
| `.golangci.yml` depguard + `coverage-gate.sh` | Both matched on the **old module path** after the rebrand, so the cross-module import rule and the 70% floor silently enforced nothing. `make lint-go` reported `0 issues` with a real `lab → pharmacy` import planted. |
| The D3 token-rotation regression test | Its fixtures ignored `*http.Request`, so no token was ever validated. The stale-token mutation **passed**. This guarded the highest-risk defect in the whole plan. |
| Three `CompleteAfterFactor` tests | Passed by accident: their `SessionFactors` fixture also reported no TOTP, so they'd have passed with the guard deleted. |
| The attempt-expired timing floor | No test read elapsed time. Deleting `waitUntilFailureFloor` passed the entire suite. |
| The rate limiter itself | `ClientIP()` was attacker-controlled (see below), so every IP-keyed limit was bypassable with a header. |

Also caught this way: **two defects in the task briefs I wrote.** One instructed
an implementer to prove an arch test by adding a second call to an unexported
function — that test pins the *wire POST*, not the Go call, so the proof was
**unfalsifiable**. The other omitted a real `handoff_url` outcome and would have
shipped a client union that silently mishandled a documented response.

**The practice that works:** for every control, ask *what mutation would break
this*, apply it, and watch a test fail. A test you have not watched fail against
the specific defect it names is not evidence. The corollary bit us too — see
"#866 shipped with an unrun suite".

---

## Key documents

- `specs/2026-08-17-secrets-management-design.md` — #45's boot-secrets slice (D1–D7)
- `specs/2026-08-17-helivanta-rebrand-design.md` — the rebrand (D1–D8)
- `specs/2026-08-17-native-mfa-auth-components-design.md` — native TOTP (D1–D8)
- `spikes/2026-08-17-zitadel-login-client-mfa.md` — **trust this over Zitadel's
  docs.** Observed, not read.
- `docs/runbooks/secrets.md` — secret inventory and provisioning
- `spikes/2026-08-16-zitadel-login-client.md` — still authoritative for the
  login flow

---

## What shipped, and the one thing each will bite you on

### #45 — boot secrets

Two secrets, not three: `SESSION_SIGNING_KEY` and `ZITADEL_LOGIN_CLIENT_TOKEN`.
`HELIVANTA_WEB_ORIGIN` is **config**, not a secret — it is a public origin and
putting it in a vault would add a rotation surface for a public string.

`make secret-session-key` mints a key in exactly the format the validator
accepts. A gitleaks gate runs in CI, pinned by tag **and digest** — `:latest`
floats, and because `.gitleaks.toml` declares a rule carrying only an allowlist,
a version that *replaced* rather than *merged* the default rule would silently
disable the detector and report `no leaks found` forever.

**Task 4 (proving the OpenBao grant) is deliberately unstarted.** Nothing has
been written to `kv/data/helivanta/helivanta-api/*`. It was held first for the rebrand and
now for #824.

### #863 — the rebrand

324 tracked files. Database identifiers deliberately did **not** move: `hms_app`
is a Postgres role and `hms_tenant_visible()` an RLS predicate, and migrations
are append-only.

**`hms_session` was in that exclusion list by mistake** — it is the HTTP session
cookie, not a database object. Renaming it was free (nothing deployed, no live
sessions) and would have been a fleet-wide forced sign-out later. It is now
`helivanta_session`.

The event subject namespace moved too (`hms.*` → `helivanta.*`, stream `HMS` →
`HELIVANTA`). Free only because nothing is deployed and no events are persisted;
after #824 that becomes a breaking contract change needing a version bump.

### #866 — `@tesserix/web` 2.2.1

The registry move is **provably inert**: same version from both registries is
byte-identical across 1940 dist files. All 13 imported symbols survive in 2.x,
which is why a major bump compiled untouched.

`.npmrc` was removed. It forced `@tesserix` to GitHub Packages and therefore
*caused* a 401 rather than preventing one — and on the Free plan
`secrets.PKG_READ_TOKEN` resolved to an empty string.

### #867 — native TOTP

A clinician with TOTP enrolled now enters the code on our own page.
`PATCH /v2/sessions/{id}` with `checks:{totp:{code}}` works under the
login-client PAT — observed, §1 of the MFA spike.

**The token rotates on every check.** Finalize needs the newest one; reusing the
creation token fails finalize *after* a correct code, so the clinician is told
their TOTP was wrong. Proven at unit and integration level.

Scope is TOTP only. Email/SMS codes are excluded because the spike could not
exercise delivery (no SMTP in dev). Everything else still hands off.

---

## Things that will bite you

**`ClientIP()` was attacker-controlled, and this is the most dangerous thing
found today.** Nothing called `SetTrustedProxies`, so gin trusted every proxy
and honoured a caller-supplied `X-Forwarded-For`; an unseen bucket key starts at
full burst. A fresh header per request bought a fresh budget. Fixed with
`TRUSTED_PROXY_CIDRS`, failing closed to the raw TCP peer.

**It is not set anywhere yet** (#824 owns the chart), so the limiter currently
runs in coarse mode: one bucket per ingress, i.e. `RATE_LIMIT_LOGIN_PER_MIN`
becomes a hospital-wide budget. Safe, not correct.

**The verified request path**, because it is not obvious: `browser → Cloudflare
edge → cloudflared POD (10.20.x) → istio-ingressgateway → app pod`. **cloudflared
runs inside the cluster**, so there are two in-cluster hops. The main gateway's
`xff-trust-hops` EnvoyFilter runs `use_remote_address: true`,
`xff_num_trusted_hops: 1`, appending — so trusting the pod CIDR `10.20.0.0/16`
lets gin walk XFF from the **right** past the in-cluster hop onto the real
client. **Unconfirmed against a live pod**, since none is deployed.

**Do not copy `go-shared`'s client-IP handling** (filed as
`tesserix/go-shared#3`). Its `getClientIP` prefers `X-Real-Client-IP` from an
EnvoyFilter that targets `custom-ingressgateway` — *not* the gateway most
services sit behind — carries `downstreamDirectRemoteAddress()`, which is a
**pod IP**, and is `add`ed rather than replaced. Its fallback then takes XFF's
**leftmost** entry. Every IP-keyed rate limit in that package is bypassable.

**#866 shipped with an unrun suite, and it cost a regression.** Its `make e2e`
hung 66 minutes on the `zitadel-login` stale-PAT race; it was killed and a manual
browser check substituted. That check was genuine — real OIDC round trip,
accessibility tree, an authenticated `GET /api/v1/pharmacy/medications` → 200 —
but a human driving a browser cannot detect a **Playwright selector ambiguity**,
which is exactly what 2.2.1's new "Show password" button introduced:
`getByLabel("Password")` matches by substring and became ambiguous for all
twelve specs. Fixed with `{ exact: true }`, found in #867's Task 6.

**`zitadel-login` reads its PAT once at boot** and can latch a stale one, logging
`failed to decrypt value` every 30s while its healthcheck never passes. It hangs
`make e2e` indefinitely and presents as a broken app.
`docker restart helivanta-dev-zitadel-login-1` fixes it.

**Zitadel core reports `unhealthy` and is fine.** Its healthcheck runs
`zitadel ready --config /dev/null`; OIDC discovery serves 200. A false signal.

**ArgoCD's `Synced/Healthy` can describe a stale revision.** Twice this session
a merged infra change did not apply until a hard refresh cleared a sync operation
parked on a completed bootstrap Job. Check
`.status.sync.revision` against `origin/main` before believing it.

**A stale `golangci-lint` cache** replays findings against the old absolute path
after a directory rename, failing lint with issues in files that no longer exist.
`golangci-lint cache clean`.

**BSD sed silently ignores `\b`.** On macOS `sed 's|\bFOO_|BAR_|'` is a no-op
with no error. Use `perl -pi -e`. Verified — and the dangerous part is the
"fix": dropping the boundary is how protected identifiers get mangled.

**Leftover `next dev` processes** remain the top local-dev hazard. A surviving
process holding a port makes turbo fail the whole group and presents as *"the app
is broken"*.

---

## Pending, in the order I would take them

### 1. #824 — container images and deployment artefacts *(now the critical path)*

It blocks **three** things: `TRUSTED_PROXY_CIDRS` has nowhere to be set, #45's
Task 4 cannot provision secrets into a deployment that does not exist, and the
XFF assumption above cannot be confirmed without a pod. HMS has no Dockerfile,
no chart, no ArgoCD app.

When it lands, confirm the real `X-Forwarded-For` string at an app pod with an
actual request rather than inheriting today's inference.

### 2. #45 Task 4 — provision the boot secrets

Paths are `kv/data/helivanta/helivanta-api/session-signing-key` and
`.../zitadel-login-client-token`. The runbook has the steps. **No secret value
should pass through an agent session** — generate and write in one piped step, or
enter it in the `secret-service.tesserix.app` console directly.

### 3. #869 — nothing sweeps `login_attempt`

Rows are removed only on a read *of that id*, so an abandoned attempt leaves a
**live Zitadel session token** in Postgres indefinitely.
`login_attempt_expires_at_idx` exists as if for a reaper that was never written.

### 4. go-shared#3 — fleet-wide bypassable rate limits

Not this repo, but it affects every service using `go-shared`'s limiter.

### 5. #855 / #861 — the bounds this session deliberately did not add

#867 declined a per-subject failure bound because one keyed on a login name is an
**unauthenticated DoS against a named clinician** — anyone could lock a doctor
out of a hospital system. Zitadel's own choice is a delay, not a lockout. Both
issues carry comments explaining the residual.

### 6. Still real from before

**#856** (`passwordChangeRequired` unsignalled), **#858** (autofilled inputs
ignore the theme — **confirmed still present on 2.2.1**), **#840** (log redaction
screens no secrets), **#833** (pagination misses the newest row), **#836** (no
Playwright spec is type-checked), **#830** (`make new-module` emits non-compiling
code — **possibly stale**: a Task-1 implementer ran it and got code that
compiled cleanly).

---

## How this work has been running

- **One subagent per task, reviewed independently before the next is dispatched.**
  Six tasks on #867, nine fix rounds across them.
- **Subagents corrected me four times, each time correctly**, by checking the
  source instead of complying: the unfalsifiable arch-test proof; the missing
  `handoff_url` outcome; my wrong claim that `AuthOtpStep` could not accept
  `noValidate` (it extends `FormHTMLAttributes` and spreads props); and a
  selector change I proposed "to match e2e" that would have **diverged** from it
  (`e2e/tests/support/login.ts` uses `getByLabel`, not `getByRole`).
  **When a subagent pushes back with evidence, it is usually right.**
- **Reports contain factually wrong claims, including the controller's.** I told
  the user there was an upstream design-system gap; there was not. State what you
  checked and mark the rest unchecked.
- **Spec claims drift from what ships.** D6's heading said "destroys the
  session" when nothing calls `DELETE /v2/sessions/{id}`; D8 listed an outcome
  name that never existed and omitted one that does. Correct superseded docs in
  the same change — `CLAUDE.md` requires it, and a false claim in a security
  comment is worse than no comment.

---

## Not decided

- **`TRUSTED_PROXY_CIDRS`: documented prerequisite vs boot refusal.** It is
  currently documented in `.env.example`. The repo's own ladder (compile error >
  boot failure > CI failure > convention) argues for a boot refusal like
  `HELIVANTA_WEB_ORIGIN` has — but that would block deployment on a value with
  nowhere to live until #824.
- **Passkeys (#422).** The session API exposes a `webAuthN` check; nothing about
  challenge issuance or the browser assertion round trip has been observed.
  Needs its own spike.
- **Whether Helivanta gets its own Zitadel org.** Unchanged; the platform team
  owns it.
- **Email/SMS second factors.** Blocked on an SMTP path nobody has exercised.
