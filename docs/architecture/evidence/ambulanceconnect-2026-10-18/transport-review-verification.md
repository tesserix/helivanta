# Transport review verification — 18 October 2026

## Production source alignment

The authenticated production stock page was inspected read-only. Its route is
`apps/shell/app/pharmacy/stock/page.tsx`, which renders `PharmacyWorkspace` inside
`HmsShell`. `Dockerfile.shell` builds that workspace and the live deployment is
`helivanta-shell`. `apps/shell` is the current web application.

A later read-only deployment check found both shell and API at 2/2 ready, using
production revision `f7e642a6236e59424f52e6ed37752d1b76235163` through GAR. Its stock
CSV export release was merged into foundation `c3b02e6`; the clinical branch was
rebased onto that foundation. Pharmacy components and the stock route have no
feature diff from production. No production patient or stock data was changed or
copied into these fixtures. No emergency migration or rollout was performed.

## Checks completed locally

- `pnpm turbo lint type-check test build format:check --concurrency=1`: all 29
  tasks passed, including web production build, native export, 537 shell tests,
  302 API-package tests, 93 shared UI tests and nine public-access contract tests.
- `make lint-go`: zero issues before the final test assertion correction.
- `go test -race -cover ./internal/modules/emergency ./internal/archtest` passed
  after adding actor-bound verifier replay and concurrent observation tests;
  emergency coverage was 76.0% at that checkpoint.
- The later full `make coverage-go` run passed every other package above the
  70% floor. One newly added emergency assertion expected `409` for a deadline
  beyond delegation expiry; the API correctly returned `400 invalid_request`.
  The assertion was corrected. That run reported emergency coverage of 76.3%.
  The subsequent scoped rerun was interrupted when the local Docker socket
  disappeared. The current-head full backend gate must therefore be confirmed
  by PR CI; the interrupted run is not a pass.
- `gitleaks git --pre-commit --staged --redact --no-banner`: no leaks found.

Tests used Node 24.11.1 and Go 1.27.2. The broad Go run used a dedicated build
cache after missing files were observed in the shared cache; package concurrency
was bounded to two. No shared Docker service was restarted to recover the run.

## Browser verification against the built web app

`go test -tags emergencybrowser -run TestEmergencyConsoleBrowser -count=1 -v
./internal/modules/emergency` ran with the already built Next production server
at loopback port 4344. All five Chromium scenarios passed concurrently at a
390 × 844 viewport:

1. Public emergency calling while the hospital API is unavailable.
2. Narrow public configuration and authenticated neighbouring routes.
3. Base grants, encrypted intake, closure and access revocation.
4. Vehicle/crew/duty readiness, persistent failure, repair and duty end.
5. Independent clinical delegation, doctor review, immutable displayed history,
   revocation and immediate loss of the review form after refresh.

The fifth journey first exposed a test navigation race: it reloaded before the
case route had arrived. Waiting for the case URL and protected detail corrected
the test; the subsequent five-scenario run passed in 11.9 seconds. Domain
responses came from real Go handlers with disposable Postgres, OpenBao and NATS.
Authentication and the permission-response boundary used explicit synthetic
identities. This is not evidence of production clinical activation or mobile
device operation.

## Release limits

This slice records a requirement without changing case priority or reserving or
dispatching a vehicle. Canonical workforce identity/qualification integration,
atomic reservation, Temporal dispatch/acknowledgement, crew workflows, hospital
acceptance, handover and live citizen booking/status remain open under #1220.
The native application remains call-first information, not a live booking app.

Foundation PR #1233 and infrastructure PR tesserix/tesserix-k8s#1456 have their
required CI checks green. Clinical review adds additive `0003_emergency` and
remains a separate draft, subject to its own current-head CI and release review.
No government dispatch integration, clinical approval or compliance certification
is asserted.
