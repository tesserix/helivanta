# SDK Rollout Plan

Maps the SDK build-out to the board ([project #12](https://github.com/orgs/tesserix/projects/12), **MVP 0 — Planning & SDK Foundation**, issues #664–#716) and defines the order, adoption model and success metrics.

## Sequencing principle

Two constraints drive the order:
1. **Planning before packages** — the RFC (#664), repo strategy (#665), API standard (#666) and event standard (#667) are inputs to almost every package.
2. **Risk-bearing packages before convenience packages** — tenancy, authN/Z, logging/redaction and audit are the reason the SDK exists; catalogs of helpers come later.

MVP 0 (M0–M2) deliberately overlaps MVP 1 (M1–M4): SDK packages harden *against* the first real services consuming them (rule of three), instead of being designed in a vacuum.

## Waves

### Wave 0 — Decisions (parallel, week 1–2)
#664 RFC · #665 repo strategy · #666 API standards · #667 event standard · #668 versioning · #672 workflow/DoR/DoD · #673 ownership · #674 ADRs
Gate: RFC approved ⇒ package issues unblocked.

### Wave 1 — Ground floor (weeks 2–4)
| Go | Web | DevEx |
|---|---|---|
| #676 workspace/CI · #677 config · #678 logging+redaction · #679 telemetry | #696 workspace/CI · #697 tokens | #709 CI templates · #714 local dev stack |
Gate: first versioned package published and consumed by a sample repo in CI, on both stacks.

### Wave 2 — The risk core (weeks 3–7)
| Go | Web | DevEx |
|---|---|---|
| #680 httpkit · #681 authn · #682 authz · #683 **tenant** · #684 **database/RLS** · #686 audit · #694 testkit | #699 auth · #700 api-client · #701 telemetry · #703 a11y | #713 security scanning · #710 contract tests |
Gate: **adversarial two-tenant isolation suite green**; fail-closed authz demonstrated; envelope contract test green Go↔Web.

### Wave 3 — Reliability & product enablement (weeks 6–10)
| Go | Web | DevEx |
|---|---|---|
| #685 events/outbox · #687 cache · #688 storage · #689 ratelimit · #690 i18n/profile · #691 flags | #698 components · #702 i18n · #704 flags · #705 data · #706 forms | #711 e2e harness · #716 dashboards |
Gate: chaos test on events (zero loss); noisy-neighbour load test passes (#712).

### Wave 4 — Multipliers (weeks 9–12)
#692 notify · #693 payments · #695 create-hms-service · #707 storybook/docs · #708 create-hms-app · #712 load harness · #715 preview envs
Gate: a brand-new service and portal generated from templates pass all gates unmodified — **this is the MVP 0 exit criterion**, and MVP 1 product teams build on the templates from here.

Planning items #669 (environments), #670 (security standards), #671 (test strategy), #675 (MVP 1 grooming) land alongside waves 1–3 as their consumers need them.

## Adoption model

- **First consumers**: the template sample service/app (SDK's own canary), then the first two MVP 1 services (recommended: MediCore patient-registration slice + AdminConnect tenant management, since together they exercise tenancy, authZ, audit, events and billing surfaces).
- Products pin exact SDK versions; upgrades arrive as automated PRs. No floating tags.
- A package is **stable** only after two independent consumers; until then it lives under an `experimental` label and may break without a major bump.
- Contribution: product teams PR into the SDK; platform reviews within an agreed SLA (#673 defines owners).

## Success metrics (reviewed at each MVP boundary)

| Metric | Target |
|---|---|
| New service/portal to first deploy | < 1 day from template |
| Adversarial isolation suite | green in SDK CI **and** in every consuming service |
| PHI in logs/telemetry (staging spot audit + scrub counters) | zero findings |
| Envelope/event contract drift | zero (CI-blocked) |
| Services on latest SDK minor | ≥ 80% within 2 weeks of release |
| SDK review SLA | 90% of PRs reviewed within 2 working days |

## Out-of-scope for MVP 0 (explicitly deferred)

- Mobile (Flutter/RN) SDK — after web SDK stabilises (MVP 2+).
- Nepal/Afghanistan provider implementations (interfaces only now) — MVP 5.
- Full offline sync/conflict resolution — mobile milestone.
- Multi-region/per-country deployment automation — topology decided in #669, built later.
