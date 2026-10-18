# EmergencyConnect and AmbulanceConnect

Programme #1220; first implementation #1221, child of citizen booking #122.
Related stories and dependencies are mapped in the programme issue. Research:
[India emergency transport](../../research/2026-10-18-india-emergency-transport.md).

## Product and journeys

EmergencyConnect remains the Helivanta product for dispatchers, fleet managers,
crews and hospital receivers. AmbulanceConnect is the public citizen experience
on web, iOS and Android. They share cases and contracts; they do not share
unrestricted data access.

1. Citizen opens without login, sees selected country and emergency call action.
2. In a verified live service area, citizen requests help for self/dependant/other,
   using GPS or manual location and an optional hospital preference. API durably
   records one case and a narrow status capability, not a dispatch promise.
3. Dispatcher reviews approved intake, related-call candidates and appropriate
   fresh fleet; atomically reserves a vehicle and awaits explicit crew acceptance.
4. Citizen sees confirmed status and uncertain ETA; crew sees only assigned cases.
5. Receiver confirms capability/acceptance, receives pre-alert and accepts handover.
6. Vehicle returns to service only after readiness clearance; callbacks, complaints,
   tariffs and reports refer to the same case without copying PHI into sales CRM.

The initial implementation completes step 1 only, with a read-only public policy
API and a native app that works from bundled information. It does not collect
patient/location data, accept bookings, claim available fleet, offer medical
advice or contact a government dispatch API. Parent issues remain open.

## Capacity assumptions and SLOs

Planning assumptions, not measured traffic: 100 pilot vehicles sending fixes
once per 10 seconds is 10 telemetry writes/s. At 1,000 vehicles this is 100/s.
100 new cases/day averages 0.0012/s; a 100x incident burst is 0.12/s; reserve an
initial tested intake budget of 10/s and 100 concurrent status readers. Payloads
are <=8 KiB intake and <=1 KiB telemetry; dashboard read:write is approximately
10:1 excluding telemetry. At 100 cases/day, 36,500/year and 109,500/three years;
200 events/case at 1 KiB means about 7.3 GB/year before indexes/replication.
Raw GPS at 1,000 vehicles is 8.64 million points/day: retain only the approved
bounded window, then aggregate per policy. Do not persist it indefinitely.

Design targets for later supervised service: intake 99.95% monthly availability,
p99 <1 second server processing, <=2 seconds end-to-end on supported networks;
status p99 <500 ms. These are targets requiring load/chaos evidence, not achieved
claims. Telemetry and status may be stale with visible age; reservations and
clinical transitions require transactional consistency. Core 112 calling must
not depend on these SLOs, authentication or the ability to fetch a policy.

The first slice has no database writes or upstream network dependencies. Its
small catalogue (<16 KiB) is bundled in native and server-rendered on web;
public config response is bounded. No new cloud workload or third-party calls,
so incremental cloud cost is existing web traffic; native signing/store costs
require the owner's account and are outside this implementation.

## Boundaries and contracts

Use current modular Go/Gin API, pgx/Postgres and outbox for future authoritative
cases/fleet; Valkey for expendable projections only. No new service is created
merely for naming. Before live intake, #1222 must verify a separately deployable
emergency entrypoint/capacity and a measured failure boundary independent of
appointments and expensive hospital workflows. This supersedes older stories'
assumption that writing a new service name alone provides isolation.

Temporal owns durable crew-acceptance, pre-alert and reassignment timers. No
HTTP call or notification inside a DB transaction. Atomic outbox + idempotent
consumers; version-guarded transitions; unique active resource constraints.
State and intended events commit together. Case creation never waits for the
patient master, CRM, routing provider, notifications or AI.

A citizen-facing preference is advisory; only qualified dispatch/clinical staff
confirm destination. Public status is minimised and authenticated per object.
Cross-tenant pre-alert/handover uses dedicated accepted-trip projections, not
cross-module SQL imports or a broad bypass of RLS. No tenant is trusted from a
mobile body/header. Provider selection resolves against published, signed
operator/service-area configuration. An unresolved provider never receives an
acknowledgement that implies operational acceptance.

## First slice: #1221 public call access

- Shared package owns source URLs, catalogue revision, review date, emergency
  number, supported-country semantics and all citizen strings. Web/API/native
  use this source of truth, not independently copied phone constants.
- `/ambulance` is an exact public page; `/api/emergency/access` is an exact,
  read-only public endpoint. Protected neighbours are still authenticated.
- API accepts only `country=IN|OTHER`; invalid, repeated or unknown parameters
  return stable `400 invalid_query`. It returns no tenant/user/private data and
  sets `Cache-Control: no-store` and `Referrer-Policy: no-referrer`.
- India is visibly selected, with an explicit “Outside India” choice that removes
  Indian dial actions. No IP/GPS guessing. Explain 108/102's regional roles without
  asserting local availability or offering unverified regional dial actions.
- Opening the dialler is user initiated. Never automatically place a call,
  request location, send SMS, submit a case or claim connection/dispatch.
- Native includes its catalogue offline; no network operation is needed. If the
  OS dialler fails, show the digits and manual-call guidance inline. A page visit
  does not collect symptoms, precise location, contact details or analytics.
- English is the initial reviewed interface language. Other language release
  requires human review; untranslated Odia/Hindi or RTL support is not claimed.
- Accessible controls, safe-area handling, scalable text, >=44pt touch areas;
  no login/session refresh on public emergency access.

## Threat model and failure behaviour

Assets in the first slice are the integrity of emergency numbers, the login
boundary and user trust. Attackers can forge query parameters, exploit a public
path prefix, or present false operational claims. The catalogue is code reviewed
and source backed, not admin-editable user input. API cannot accept phone URLs,
tenant IDs or return patient records. Exact route checks prevent sibling exposure.
No new credential is introduced. SSR page has no network dependency; a failure
of identity or the Go API does not block it. Native works with data off; a device
without voice/dialler capability gets manual-call guidance and no success claim.

Later slices additionally protect PHI, precise GPS, contact data and resource
integrity from anonymous abuse, other tenants, insiders and compromised crew
phones. Authorization failure denies access; capacity degradation must preserve
emergency call fallback. A map outage yields unknown ETA, not a fabricated one.
A notification outage retains durable delivery intent and alarms on unacknowledged
assignment; no fleet means explicit escalation rather than pending rescue forever.

## Migration, rollback and release

First slice has no schema migration, new secret, IAM grant or live mutation.
Rollback is removal/revert of its code, preserving existing tenant and clinical
flows. Do not publish as an active booking service. Native package identifiers
are proposed build settings, not registered store ownership. No store submission
or production emergency activation is part of this work.

Future schema changes are additive, forced-RLS, append-only migration IDs,
UUIDv7/composite tenant foreign keys and tested against a scratch DB. Never roll
back by deleting cases/events; disable new intake while preserving active case
operations and public calling, under an approved incident runbook.

## Alternatives

A fleet-map demo with invented ambulances is rejected: it can imply rescue that
is not available. A new Flutter/Keycloak stack is rejected: current repository
foundation reserves Expo and already uses Zitadel. A new dispatch service and
DB before a measured isolation design is rejected: it adds another owner and
failure mode. Shipping booking before staff/provider readiness is rejected: a
successful HTTP request alone cannot establish emergency response.

## Native runtime and dependency validation

The Expo-supported React version is scoped to the native app. Metro and Jest
resolve React and JSX subpaths from that app so the web runtime cannot leak into
native components. An observed invalid-hook failure drove this correction; both
Hermes platform exports and native component tests are required afterwards.
The [Expo Forge backport](../../security/2026-10-18-expo-forge-backport.md) records
an observed upstream vulnerability, exact source patch, automated red/green
regression and its narrowly associated registry-audit exception.
