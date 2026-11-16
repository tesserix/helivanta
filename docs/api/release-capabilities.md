# Release capability admission

Implementation candidate for #1728; not deployed or enabled for Helivanta.
The platform owns release availability. IAM and each product retain membership,
permission, professional and object-scope authority. A released flag grants none
of those authorities (ORG-AC and ORG-AU controls remain unchanged).

Canonical source: `contracts/release-capabilities.json`. Run
`node scripts/generate-release-capabilities.mjs` and its `--check` mode to
regenerate/verify Go and Zod outputs and registered route/page coverage. CI also
runs `node --test scripts/release-capabilities.test.mjs`.

| Global API read                              | Requirements                                                                         | Response                                                                                    |
| -------------------------------------------- | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------- |
| `GET /api/v1/auth/release-capabilities`      | Anonymous, shared IP rate limit, no query/body                                       | Policy version, issued/expiry instants, onboarding boolean only                             |
| `GET /api/v1/workspace/release-capabilities` | Verified session/current membership; exact expected tenant and subject preconditions | Same timestamps plus verified tenant/subject and every known capability's effective boolean |

Both reads return `Cache-Control: no-store` (private for the account read).
Unknown keys, bad policy versions, invalid lifetimes, mismatched accounts and
missing dependencies are refused by the browser. Lifetimes cannot exceed 60
seconds. The account-bound request validates the account again after the read;
the query key includes tenant and subject. Switching accounts closes business
content immediately. No provider SDK/management credential reaches the browser.

New API commands intersect local GrowthBook evaluation, the compiled eligible
set and dependency closure with existing authorization. Explicit off is 404;
unknown/unloaded/expired provider state is 503. Unknown route classification
refuses startup. Evaluation performs no network request in the admission path.
Provider refresh is bounded, redirects are refused, and provider errors never
extend an old snapshot's expiry. Retained baseline authentication, verification,
password setup, session recovery and tenant security keep their existing checks.

Navigation, dashboard cards, connected-workspace links and bookmarked workspace
pages use the same canonical page mapping. Unavailable children are not mounted,
so their effects and business API queries do not run. R1 pharmacy navigation is
Staff source plus Outlets & access; Receiving belongs to supplier collaboration.
Scheduling, payroll, stock, sales, receiving and other later groups stay dark.
New signup is gated independently of already-issued verification/password work.

Production requires `GROWTHBOOK_API_URL` and a mounted namespace-scoped reader
file at `GROWTHBOOK_SDK_KEY_FILE`. The reader must select a dedicated Helivanta
project/environment with only `helivanta.release.*` keys. Store it in OpenBao
and deliver it using the namespace's ESO identity. Missing configuration refuses
startup. The explicit local synthetic-identity fixture is allowed only for the
existing dev issuer/environment; it cannot substitute for production wiring.

## Remaining activation gates

Nested Staff panel admission and its API argument-specific classification are
not complete. Dedicated provider project/reader, OpenBao/ESO wiring, live
on/off/outage acceptance, database authorization acceptance and version-bound
release approval remain required before deploying/enabling this application
candidate. No existing hospital, account, MFA or provider project is altered.
See the [delivery plan](../superpowers/plans/2026-11-16-first-release-capabilities.md).
