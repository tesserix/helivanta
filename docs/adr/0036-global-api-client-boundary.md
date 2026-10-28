# ADR-0036: Global API as the client integration boundary

Date: 2026-10-24. Status: user-requested direction; routing implementation in this change.

The UI and apps must integrate with one Global API, which connects to product
APIs. This refines [ADR-0007](0007-product-api-target-architecture.md): product
ownership and independent deployment remain the target, while clients no longer
select or call product origins. The same-origin BFF and ingress forward to Global
API. Existing modules remain the current data owners in one backend process.

Use a separate Go routing service with explicit destinations and unchanged `/v1`
contracts. Keep authorization at each product boundary and never turn routing
headers into identity. Current credential pass-through retains the owning API's
verification; audience-scoped machine delegation is a prerequisite for future
service extraction, not a guarantee of this proxy.

[Global API rules](../api/global-connector.md) define security, failure handling,
new-service onboarding and completion evidence. Direct client routing is rejected
because it conflicts with the requested common boundary. Centralizing domain logic
is rejected because it duplicates masters and destroys independent ownership.
The extra hop adds latency and an availability dependency; capacity validation,
private ingress and owner-controlled GitOps rollout are required before release.
No database migration or clinical workflow change is made by this routing slice.
