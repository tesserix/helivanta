# The e2e suite must be run and checked by something other than a person

**Issue:** [#920](https://github.com/tesserix/helivanta/issues/920)
**Realises:** the founding CI design
(`docs/superpowers/specs/2026-08-04-hms-repo-setup-design.md:132-135`), which
listed **"Playwright E2E"** as a CI job. It was never built.
**Adjacent:** [#919](https://github.com/tesserix/helivanta/issues/919) — the Go
half of the same defect: a documented gate reporting green on a tree it does
not check.

## The problem, stated precisely

Two gaps that are only serious together.

**Nothing runs the suite.** `.github/workflows/` holds `ci.yml` and
`images.yml`; neither invokes Playwright or a compose stack. Every e2e spec in
this repo runs only when a human runs it.

**Nothing checks the code either.** `e2e/package.json` declares exactly one
script — `test:e2e`. It has no `lint`, `type-check` or `format:check`, so
`pnpm turbo lint type-check test build format:check` never touches `e2e/` at
all: it is not excluded by config, it is simply absent from every task's
matched-package set. `scripts/` is worse — not a workspace member, no
`package.json`, invisible to turbo by construction. Neither is excluded by
`.prettierignore`; both are simply never reached.

**This is not abstract.** #866 shipped with an unrun suite and cost a
regression — a Playwright selector ambiguity, which the handoff notes a human
driving a browser could not have caught. And `e2e/` now holds the two specs
written to make #916's defect class visible: a cross-site property probe and a
renewal-survival test. **The controls built to stop our most expensive recent
bug recurring are themselves guarded by nothing.**

## D1 — The suite runs on every PR and every push to `main`

Chosen over nightly, path-filtered and label-triggered, deliberately:

- **Nightly catches regressions after merge**, which is precisely how #866
  shipped broken.
- **A path filter that is wrong is a silent gap**, and this repo has been bitten
  by exactly that shape twice this week.
- **A gate people opt into is not a gate.**

The cost is real and accepted: roughly 8–10 minutes added to every PR. It is
affordable because Actions minutes are free while the repo is public.

**State the dependency plainly:** the repo is public *because billing is
failing*, not by preference. If it is ever flipped private, this job stops
running along with every other check — see the handoff's standing note. That is
a reason to fix billing, not a reason to skip the job.

## D2 — A hard timeout, a loud failure, and no retry

`make e2e` has a **documented unbounded hang**: #866's run sat for **66
minutes** on a `zitadel-login` stale-PAT race before being killed, with no clear
failure signal (`HANDOFF-2026-08-17-evening.md:158-166`). The service reads its
PAT once at boot and can latch a stale one.

So the job carries a hard `timeout-minutes` well above the expected runtime and
far below 66. A hang fails the run.

**No automatic retry.** A retry would hide a known flake and make a genuine
intermittent regression indistinguishable from it — and this repo's entire
recent history is controls that reported green while doing nothing. The
stale-PAT race is filed as **#923** and fixed there, not papered over here.

**A hang must be legible.** On failure the job dumps `docker compose ps` and
the `zitadel-login` container logs, so the next person reads a cause rather
than a timeout.

## D3 — No GitHub secrets are required, and none may be added

Verified: every credential the dev stack needs is a committed literal or is
generated locally at first boot.

| Value | Source |
|---|---|
| `HELIVANTA_DEV_ZITADEL_MASTERKEY` | literal default in `Makefile` |
| `HELIVANTA_DEV_SESSION_SIGNING_KEY` | literal, documented as "committed to source, shared by every developer and CI runner", usable only with `HELIVANTA_ENV=dev` |
| Postgres/Redis/Zitadel-DB passwords | literals in `docker-compose.dev.yml` |
| `login-client.pat`, `helivanta-seed.pat` | written by Zitadel's own first-instance provisioning at container boot |
| `zitadel*.env` (client ids) | written by `scripts/zitadel-bootstrap.mjs` against the just-booted ephemeral instance |

This matters because the Free plan has no org secrets. **A design that needs
one is the wrong design** — it would mean a real credential had leaked into the
dev path.

## D4 — `*.localhost` resolution on the runner is UNVERIFIED and must be proven first

The harness depends on `helivanta.localhost` and `auth.tesserix.localhost`
resolving to loopback and being treated as **distinct registrable domains**
(#916, D6). That was verified on macOS. **It is not verified on an Ubuntu
GitHub runner, and the two resolve by different mechanisms:**

- **Chrome** resolves `*.localhost` to loopback itself, per RFC 6761 — this is
  the part #916 proved.
- **Everything else in the chain** — `curl` health checks, `node` bootstrap and
  seed scripts, the Go API dialling Zitadel, `preflight.sh` — uses the **OS
  resolver**. macOS resolves `*.localhost`; a stock Ubuntu image may resolve it
  via `systemd-resolved`, or may not resolve it at all, depending on the image.

**This is the first thing to establish, before any workflow is written.** If
the runner does not resolve it, the fix is explicit `/etc/hosts` entries added
as a step — which is fine, but it must be a decision made from an observed
result, not an assumption that happens to hold. A job that fails at
`make dev-infra` for a DNS reason nobody anticipated is how this work gets
reverted.

## D5 — The static gate covers `e2e/` and `scripts/`, or says why not

- `e2e/` gains `lint`, `type-check` and `format:check` scripts, so the
  documented five-task gate genuinely reaches it. It already has the
  `@playwright/test` and `@types/node` devDependencies; it needs the shared
  prettier config reference the other packages carry.
- `scripts/` is not a workspace member and has no `package.json`. Decide
  deliberately between making it one and adding an explicit root-level pass —
  and record which and why. Leaving it silently exempt is the option this
  spec rejects.
- The three files unclean today are formatted in the same change, so the gate
  goes green without a follow-up:
  `e2e/tests/support/hosts.ts`, `e2e/tests/session-renewal.spec.ts`,
  `scripts/lib/zitadel.mjs`.

## D6 — The job must be able to fail

A CI job that cannot go red is decoration. Before this is considered done,
**the new job must be observed failing** for a real reason — break a spec
assertion on a scratch branch, watch the job go red, revert. `continue-on-error`
and soft failures are forbidden here for the same reason retries are.

## Out of scope

- **Fixing the `zitadel-login` stale-PAT race** — [#923](https://github.com/tesserix/helivanta/issues/923). D2 bounds its damage rather than curing it.
- **Speeding the suite up.** Phase 3 already came down from 6.3m to 3.5m in
  #916. Further reduction is not this issue's job.
- **`make verify-local` in CI.** It is cheaper but explicitly cannot catch what
  Playwright catches — its own script comment defers the browser redirect flow
  and session renewal to `make e2e`.
- **Go formatting** — #919.
