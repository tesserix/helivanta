# AmbulanceConnect delivery plan

Programme #1220. Design: [spec](../specs/2026-10-18-ambulanceconnect-design.md).

| Order | Deliverable                                                                              | Existing issues / new gaps                      | Release evidence                                                                                             |
| ----- | ---------------------------------------------------------------------------------------- | ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| 1     | Public call-first web/API and native citizen app                                         | #1221 (child of #122), #11                      | Red/green unit, interaction, exact-route auth tests; real web; native platform exports and target evidence   |
| 2     | Provider/base onboarding, facility hierarchy, service agreements and safety activation   | #21, #22, #623, #1222                           | Verified documents, explicit permissions, effective agreement tests; no unstaffed booking                    |
| 3     | Canonical intake, immutable timeline, caller/patient basis and private status capability | #121, #122, #125, #131, #57                     | Idempotent ambiguous retries; tenant/facility/object denial; retention and no-PHI logs                       |
| 4     | Fleet/crew readiness and accountable dispatch                                            | #126, #127, #134, #420, #123                    | Concurrent one-vehicle race; stale state; qualifications; crew non-response; cancellation reconciliation     |
| 5     | Citizen nearby service/hospital preference, routing and receiving pre-alert              | #1223, #239, #128, #129, #135                   | Capability and acceptance distinct; privacy; diversion and routing outage drills                             |
| 6     | Offline crew app, digital handover, ED integration and management                        | #124, #132, #130, #119, #116, #579              | Kill/restart encrypted queue; duplicate delivery; late evidence; receiver identity; real source metrics      |
| 7     | Operational CRM, tariffs, grievances and supervised pilot                                | #1224, #388, #389, #808, #603, #571, #653, #663 | No emergency payment gate; minimised CRM; failover/restore; local language and accessibility reviews; drills |
| 8     | Home-visit extension and authority adapters                                              | #120, #133                                      | Approved clinical exclusions; authority agreements and conformance, no simulated live government integration |
| 9     | Optional governed dispatch assistance                                                    | #743                                            | No autonomous dispatch/diagnosis; evaluation, override, protocol-only fallback                               |

## Implementation sequence for #1221

1. Write shared catalogue/country boundary tests and observe failure.
2. Implement source-backed India access; unsupported country contains no Indian
   number; expose regional service descriptions as information, not availability.
3. Add exact-route middleware and session-renewal regressions; observe red before
   allowing the two public routes.
4. Build responsive public web access and bounded read-only API. Exercise no
   cookies, malformed query, neighbouring protected routes and source labels.
5. Scaffold the Expo SDK 57 citizen app consuming the same package. Test the
   real screen, country choice, dialler failure, labels and no success claim.
6. Typecheck/lint/test/format/build affected workspaces and full existing web
   gates. Expo doctor and iOS/Android exports. Document simulator/device limitations
   explicitly; no native release claim without both platform evidence.
7. Run local browser at small viewport and large text, no login or patient data.
   Record evidence and update issues without closing parent booking stories.

## First operational intake slice, after call-first

Use #121's canonical case with one transaction for case, immutable received event
and outbox. Introduce a dedicated dispatcher role and explicit operating-base /
facility grants; do not grant ambulance dispatch to all clinicians by default.
No clinical priority is invented: untriaged intake remains visibly untriaged until
an approved protocol/qualified dispatcher records it. Resolve case IDs/status
capabilities without exposing tenant membership or clinical records publicly.
Observe two-tenant and two-facility tests fail before implementation, and test
ambiguous commit, same-key/different-body conflict, duplicate phone/app reports,
unreachable caller, downtime escalation and retention before enabling intake.

## Pilot assumptions and decisions pending

Bhubaneswar/Odisha is a proposed pilot, pending user confirmation. Partner model,
staffed hours, receiving hospitals and approved languages require explicit named
operational owners. No provider, hospital capacity or government integration is
assumed from software configuration. Technical code completion does not satisfy
clinical activation or app-store release approval.
