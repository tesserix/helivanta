# HMS Platform SDK — Documentation

The HMS platform ships ~9 backend services (Go) and ~6 web portals (React/Next.js) that must all behave identically on the concerns that cannot be allowed to diverge: **tenant/data isolation, security, privacy (PHI/DPDP), observability, and reliability**. The SDK is where those concerns are solved once, tested once, and consumed everywhere.

| Document | Contents |
|---|---|
| [architecture.md](architecture.md) | Goals, non-goals, design principles, key decisions **with pros/cons and alternatives considered**, repo & versioning strategy, dependency rules |
| [go-sdk.md](go-sdk.md) | Backend Go SDK: full package inventory, responsibilities, API sketches, testing approach |
| [web-sdk.md](web-sdk.md) | Frontend Web SDK: full package inventory, responsibilities, API sketches, testing approach |
| [rollout-plan.md](rollout-plan.md) | Build order, adoption model, success metrics, mapping to board issues #664–#716 |

## TL;DR

- **Two SDKs**: `hms-go-sdk` (backend, evolving from `go-shared`) and `@tesserix/hms-*` (frontend, evolving from `design-system` / `@tesserix/web`).
- **Golden rule**: the SDK contains *generic platform capability only* — never domain logic. "Audit event emitter" belongs; "prescription validation" never does.
- **Non-negotiables enforced by the SDK, not by review**: tenant binding on every DB access (RLS), fail-closed authorization, PHI-redacted logs and telemetry, standard error envelope, audit emission for sensitive actions.
- **Consumption**: versioned packages via GitHub Packages; services/apps are generated from starter templates (`create-hms-service`, `create-hms-app`) with everything wired.
- Work is tracked on the [Hospital Management System board](https://github.com/orgs/tesserix/projects/12) under **MVP 0 — Planning & SDK Foundation** (issues #664–#716).
