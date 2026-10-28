# Global API integration specification

User request: implement one Global API used by Helivanta's UI/apps and connected
to the product APIs. Decision and alternatives: [ADR-0036](../../adr/0036-global-api-client-boundary.md).
Rules and acceptance: [Global API contract](../../api/global-connector.md).

Current runtime has one Go module registry; MediConnect and other planned products
are not implemented APIs. Deliver a separate transport service for current owners,
wire development, UI/logout and the production-build E2E harness through it, and
package the executable for the owning GitOps repository. Keep current canonical
schemas, session authority, permissions and RLS unchanged. No fabricated product
endpoints, clinical/financial orchestration or independent database extraction.

Protected assets: session credentials, patient/staff records, stock and financial
state. Threats include another tenant, unauthenticated callers, forged forwarding
headers and compromised adapters. Each product remains responsible for verifying
credentials and current scope. The connector accepts only configured origins and
must preserve denial/errors rather than translate them into success.

The capacity assumptions and outstanding production requirements are recorded in
the contract. A local routing test does not establish live service availability,
clinical acceptance, delegated M2M authentication or production readiness.
