# hms becomes helivanta, everywhere at once

**Issue:** [#863](https://github.com/tesserix/hms/issues/863)
**Blocks:** [#45](https://github.com/tesserix/hms/issues/45) Task 4 — secrets are
deliberately not provisioned at `kv/data/hms/api/*` while that path is about to
move.
**Feeds:** [#824](https://github.com/tesserix/hms/issues/824) — `helivanta.app`
becomes the deployed origin, the Zitadel redirect URIs and the Cloudflare record.

The product has a name and a domain: **Helivanta**, `helivanta.app`. Everything
in this repository still says `hms`, which was a placeholder. This spec renames
it, in one coordinated change, because a half-rename costs more in confusion
than it buys in progress.

---

## Observed state, 2026-08-17

Each checked, not assumed. Three of these are why this is cheap now and will not
be later.

| Claim | How it was checked |
|---|---|
| 324 tracked files contain `hms` | `git ls-files \| xargs grep -ril hms` — 143 backend, 64 apps, 61 docs, 26 packages, 10 scripts, 8 e2e, plus root config |
| 110 Go files / 364 lines carry the module path | `grep -rn "github.com/tesserix/hms" --include="*.go"` |
| **All three `@hms/*` packages are private at `0.0.0`** | `packages/{api,config,ui}/package.json` — `private: true`, never published, so no external consumer breaks |
| **Production Zitadel has no HMS app** | No `hms`-named config objects in the prod `zitadel` namespace; the dev app is provisioned by `scripts/lib/zitadel.mjs` at first boot |
| **Nothing is deployed** | No Dockerfile, chart or ArgoCD app (#824). There is no running configuration to migrate. |
| OpenBao holds no HMS secrets | `namespace/hms`, `serviceaccount/hms-api` and `SecretStore/openbao-hms-api` exist but are empty — #45 Task 4 was held |

**The consequence that shapes this spec:** almost nothing here is a breaking
change. No published package, no live deployment, no production IdP app, no
provisioned secret. The costs are diff size, local developer disruption, and
care.

---

## D1 — The identifier is `helivanta`, and case forms map predictably

The bare word, never the TLD. `helivanta.app` is a domain, not an identifier.

| Current form | Becomes | Appears in |
|---|---|---|
| `hms` | `helivanta` | module path, package names, dirs, k8s, OpenBao |
| `HMS` | `HELIVANTA` | env var prefixes |
| `HMS` (prose, as the product) | `Helivanta` | docs, comments, UI copy |
| `Hms` | `Helivanta` | Go identifiers, React components |

**`HMS` is also an acronym, and that is the trap.** It expands to "Hospital
Management System" — a generic description of the category. `Helivanta` is a
name and expands to nothing. So a blind replace turns "a hospital management
system platform" into gibberish.

The rule: **the name renames, the description stays.** `README.md`'s "Hospital
Management System platform" becomes "Helivanta — a hospital management system
platform". Lowercase generic usage describing the category is left alone; every
use of `HMS` as *this product's name* becomes `Helivanta`.

This cannot be done by `sed` alone, and the plan must not pretend otherwise.

## D2 — Every document renames, including dated records

**Decided by the product owner, against this author's initial recommendation,
and recorded here with the reasoning on both sides so the choice is legible
later rather than looking like an oversight.**

The argument for leaving dated specs, plans, spikes and handoffs untouched: they
are a design record, and a 2026-08-16 document that says `helivanta` claims a
name that did not exist until 2026-08-17.

The argument that won: a reader hitting `hms` in any document has to stop and
work out whether it is a stale name, a different component, or a live
identifier — and that cost is paid on every read by every reader, forever,
while the historical-accuracy cost is paid once by whoever cares about
provenance, who has `git log` and this spec.

**One narrow carve-out, and it is about evidence, not naming.** Literal
transcripts of observed API requests and responses keep their original values.
In `spikes/2026-08-16-zitadel-login-client.md` exactly two lines are affected:
a request body containing `pwchange-test@hms.dev` and one containing
`HmsDev123!`. Those record what was actually sent on the wire during a live
spike. Rewriting them would make the document assert it observed something it
did not observe — and the handoff instructs readers to trust that spike over
Zitadel's own documentation, so its evidentiary value is load-bearing. A
one-line note marks them.

Everything else in those documents — prose, headings, identifiers, paths —
renames.

## D3 — Environment variables become `HELIVANTA_*`

All 16, including `HELIVANTA_ENV` and `HELIVANTA_WEB_ORIGIN`.

Free now for the reason D-observed records: nothing is deployed, so there is no
running configuration to migrate. After #824 ships a deployment this stops being
free, because the variables become part of a live pod spec.

The cost is local and real: every developer's `.env` breaks. `.env.example`,
`docker-compose.dev.yml`, the `Makefile` and `docs/` change in the same PR so
that a fresh clone works, and the PR body tells existing developers what to
rename in their own `.env`.

**`HELIVANTA_ENV` deserves a specific note.** `config.IsDev()` compares it to `"dev"`
and defaults to false so that an unset or misspelled value fails closed. A
rename that leaves a stale `HELIVANTA_ENV=dev` in someone's shell does not silently
unlock dev behaviour — it fails closed into production mode, which is the safe
direction and is why this rename cannot open a hole.

## D4 — Repo first, then module path, then the local directory

Order matters, and only for one reason: `go.mod` must never be pushed declaring
a path that does not resolve.

1. Rename `tesserix/hms` → `tesserix/helivanta` on GitHub. Existing clones and
   issue links redirect.
2. In the same PR, move the module path to `github.com/tesserix/helivanta` and
   rewrite all 364 import lines.
3. Rename the local working directory last, because it changes the path every
   tool in the session is using.

GitHub's redirect makes step 1 tolerant of ordering, but relying on a redirect
to make `go get` work is a fragile thing to leave in place, which is why the
module path moves in the same change rather than "later".

## D5 — Database identifiers do NOT rename

`hms_app` (the Postgres role the application connects as) and
`hms_tenant_visible()` (the RLS predicate function called by every tenant
policy), plus the role names inside migrations, stay exactly as they are.

**Corrected 2026-08-17, after Task 2's review surfaced it.** An earlier version
of this list also named `hms_session`, described as a database identifier. It is
not one: `hms_session` is the **HTTP session cookie name**
(`backend/pkg/authn/authn.go:15`, `const SessionCookie = "hms_session"`), 45
occurrences across backend, frontend and e2e. Excluding it was a mistake of fact,
not of judgement — and it matters, because a cookie rename is **free right now**
(nothing is deployed, so there is no live session to invalidate) and costs a
fleet-wide forced sign-out once there is. That is exactly D3's argument for
renaming the environment variables, and it applies here identically. The cookie
**does** rename; see the plan's Task 3.

Migrations are append-only (`docs/standards/backend.md`), so renaming a role
means a *new* migration mutating roles on live data, plus every RLS policy and
connection string that names them — to change a string no user, and almost no
developer, ever reads. The blast radius is inside the one component where
mistakes are least recoverable.

Stated explicitly rather than left as an omission, because a future reader will
otherwise find `hms_app` in a `helivanta` codebase and assume the rebrand was
abandoned half-done. It was not: this is the line, and this is why.

## D6 — Zitadel, Kubernetes and OpenBao identifiers rename

| Identifier | Becomes | Why it is free |
|---|---|---|
| Zitadel app `hms-web`, client, org `HMS` | `helivanta-web`, org `Helivanta` | dev-only; provisioned by `scripts/lib/zitadel.mjs` at first boot, and prod has no HMS app |
| k8s `namespace/hms`, `serviceaccount/hms-api` | `helivanta`, `helivanta-api` | created 2026-08-17, empty, nothing scheduled |
| `kv/data/hms/api/*`, `SecretStore/openbao-hms-api` | `kv/data/helivanta/api/*`, `openbao-helivanta-api` | no secret was ever written; #45 Task 4 was held for exactly this |
| `tesserix-k8s` whitelist entry | `helivanta` / `helivanta-api` | one PR against the infra repo |

**Renaming the Zitadel app mints a new client ID.** `NEXT_PUBLIC_ZITADEL_CLIENT_ID`
changes with it, in `.env.example`, `docker-compose.dev.yml` and CI's build env.
This is the single most likely thing to be missed, because a wrong client ID
does not fail the build — CI's own comment records that `next build` succeeds
with these unset and surfaces as a runtime 500 on `/login`. So it must be caught
by running a sign-in, not by a green pipeline.

## D7 — What must not break, and how each is checked

Four things fail quietly under a rename. Each gets a check that would actually
catch it.

1. **The e2e login contract.** `e2e/tests/support/login.ts` drives sign-in by
   accessible name — `Email`, `Password`, `Sign in` (login-client spec D6).
   These are **UI copy, not identifiers, and must not change.** A rename that
   touches them fails all eleven specs at the login step, which reads as a
   broken application rather than a renamed label. *Check:* the full e2e suite
   passes, and the three accessible names are asserted unchanged by grep before
   the suite is run.

2. **The arch tests.** `internal/archtest` walks package paths. After a module
   move it could match *nothing* and pass while enforcing nothing — the worst
   failure mode this codebase has, and the one its own principles single out.
   *Check:* deliberately introduce a violation the arch test should catch
   (a module importing another module) and observe it FAIL after the rename.
   A passing arch suite proves nothing on its own here.

3. **The gitleaks allowlist.** `.gitleaks.toml` and `docs/runbooks/secrets.md`
   hardcode `kv/data/hms/*`. *Check:* the pinned scanner reports clean AND is
   observed still firing on a probe — clean alone is what an inert gate reports.

4. **Zitadel dev bootstrap.** `scripts/lib/zitadel.mjs` and the compose stack
   provision the app. *Check:* a full local sign-in completes against a freshly
   reset stack — `make reset && make up` then an actual login, not a green
   build.

## D8 — One PR, executed when nothing else is in flight

A 364-line import rewrite conflicts with every open branch. #864 (#45's boot
secrets) must land first.

The rename itself is mechanical but not uniform: D1's acronym rule and D2's
evidence carve-out need judgement, and the four D7 checks need running rather
than reading. So it is one PR, but not one `sed`.

---

## Errors and failure handling

- **A missed identifier in Go or TS** — caught by the compiler and
  `pnpm turbo type-check`. This is the easy majority.
- **A missed identifier in a string literal, env var or YAML** — NOT caught by
  any compiler. This is where the real risk lives: the Zitadel client ID, the
  OpenBao path, the k8s namespace. Each is covered by a D7 check that exercises
  the runtime path rather than the build.
- **A wrongly renamed accessible name or UI string** — caught by e2e, and only
  by e2e.
- **An over-eager replace mangling prose** — "a helivanta platform" where the
  text meant the category. Caught by review of the docs diff, which is why the
  66 markdown files are reviewed as a group rather than skimmed inside a
  324-file diff.

## Testing

- `make lint-go`, `make test-go`, `make coverage-go` green.
- `pnpm turbo lint type-check test build` green.
- Full e2e suite green, with the three accessible names verified unchanged first.
- The arch-test violation probe from D7(2), **observed failing**.
- The gitleaks probe from D7(3), **observed firing**.
- A real local sign-in against a reset stack, per D7(4).

## Risks

- **The Zitadel client ID is the most likely miss** (D6), and it fails at
  runtime rather than at build.
- **`git log` archaeology gets harder** — searching for `hms` in history still
  works, but a reader diffing a renamed dated document against its original sees
  churn unrelated to the design. Accepted under D2.
- **`tesserix-k8s` needs a matching PR**, and the already-merged
  `tesserix-k8s#365` created the objects in the old name — so that repo has one
  entry to rename, not just add.
- **Nothing structurally prevents `hms` reappearing.** After this lands, a
  grep for `hms` in tracked files should return only the D5 database
  identifiers and the D2 evidence lines. Worth a CI check, though that belongs
  with #713 rather than here.

## Out of scope

- Database identifiers (D5).
- The `helivanta.app` deployment, DNS and TLS — #824.
- A CI guard preventing `hms` reappearing — #713.
- Any product, UI or marketing copy beyond mechanical renaming. Naming the
  zones, the tone of the login page, and anything user-facing beyond the
  product name is a design decision, not a rename.
