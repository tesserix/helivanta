# Pharmacy payment/return journey (#1478)

- [x] Record real-owner boundaries and synthetic external-provider scope.
- [x] Add development-only loopback provider with real scoped OpenBao and
  automatically rotating ephemeral JWT; protect generated credentials/files.
- [x] Create licensed/sellable stock through owner APIs with two distinct actors.
- [x] Verify confirmed UPI, customer/tender hold, one invoice, partial return,
  quarantine quantities and return replay through the browser and owner reads.
- [x] Verify unused refund record, retained paid state and refused later sale use.
- [x] Run actual production-built shell acceptance on isolated ports; new journeys
  passed in 26.5s and 24.4s. The standard config also ran its two bulk tests; all
  four passed in 1.2m. No product request interception, SQL seeding or internal
  outcome injection was used. Razorpay/bank settlement is explicitly synthetic.
- [x] Fixture guards reject non-dev and invalid ports before starting services.
- [x] Workspace lint/type/format checks pass (21 tasks); all 33 script tests pass.
- [ ] Full CI with the fixture wiring remains required.

Local services belong only to `/private/tmp/helivanta-pharmacy-journey`: API
18080, Global API 18081, production shell 4399, provider 18892/18893 and its
OpenBao 18894. Core Compose project is Codex's reused `helivanta-opd-1389`.
Stop only listeners verified against this worktree; do not touch Claude's stack.

Implementation follow-up: unused-UPI fix #1465 is merged. Image gate #1477 is
draft pending worker acceptance. Worker infrastructure #1615 is merged and its
Atlantis apply added exactly three bindings without changing/deleting resources.
Credential initialization/activation and designated production synthetic scope
remain pending. Licence evidence #1391 and NPPA pricing #1371 follow this journey.
