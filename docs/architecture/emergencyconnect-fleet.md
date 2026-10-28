# Fleet configuration and readiness

Programme #1220, fleet #126 and allocation #127. This is the staff fleet
foundation. It does not implement live dispatch, crew acknowledgement,
citizen booking, hospital acceptance or workforce/FacilityConnect integrations.
Those stories remain open. Do not activate a patient-facing response promise
using this board alone.

## Tenant, base and permissions

An independently governed hospital/provider is a tenant, while facilities and
operating bases within a hospital network are scoped resources in that tenant.
A user may have independently granted access to several tenants or bases; the
active verified session and an explicit base grant select the scope of every
request. Nothing here merges providers into a hospital tenant or authorises
cross-tenant patient sharing.

Every route below starts `/api/v1/emergency/bases/{base}`. Authentication uses
the existing Helivanta session, CSRF protection and OpenFGA permission chain.
Every table additionally enforces tenant/base/subject RLS. A tenant administrator
without a base grant gets 404 from fleet operations. Permissions alone do not
give object access. Crew subjects and verification references stay within the
scoped API; outbox events contain only opaque base/resource IDs and fixed actions.

| Operation | Route | Permission | Base grant |
| --- | --- | --- | --- |
| Fleet board | GET `/vehicles` | `emergency.fleet.read` | reader/dispatcher/supervisor |
| Configure or retire vehicle | PUT `/vehicles/{uuidv7}` | `emergency.fleet.manage` | supervisor |
| Credential registry | GET `/crew` | `emergency.fleet.read` | reader/dispatcher/supervisor |
| Verify/suspend credential | PUT `/crew/{uuidv7}` | `emergency.fleet.manage` | supervisor |
| Duty history | GET `/duties` | `emergency.fleet.read` | reader/dispatcher/supervisor |
| Start bounded duty | PUT `/duties/{uuidv7}` | `emergency.fleet.manage` | supervisor |
| End duty | POST `/duties/{uuidv7}/end` | `emergency.fleet.manage` | supervisor |
| Record measured readiness | PUT `/readiness/{uuidv7}` | `emergency.fleet.check` | dispatcher/supervisor |
| Clear failed check | POST `/constraints/{checkUUID}/{code}/clear` | `emergency.fleet.manage` | supervisor |

All collections use cursor pagination with the standard default/max limits.
The board's `computed_at` is the database evaluation time, not a guarantee that
readiness will remain valid until an operator next acts. Future reservation must
recheck and reserve in the same transaction; a green browser badge is insufficient.

## Configure and use the console

1. A tenant administrator configures an operating base in **EmergencyConnect**,
   records its approved policy/retention reference, verifies the staff identity
   subject and assigns the matching emergency role plus supervisor base access.
   The existing staff-intake guide describes this setup.
2. The supervisor opens that base, then **Configure vehicle**. Use an operational
   code rather than a patient or employee name, certified vehicle class, actual
   certification expiry, approved policy reference, approved minimum cylinder
   pressure and readiness validity. The application supplies no clinical threshold.
   The observer must have the actual reviewed equipment checklist named by that
   policy. Dates entered in the console use the device's local timezone and are
   transmitted/stored in UTC. Nonexistent or ambiguous daylight-saving wall
   times are rejected; API clients can supply an explicit RFC3339 offset.
3. **Verify crew credentials** for two different verified identity subjects. Record
   driver or EMT/paramedic qualification, real expiry and the restricted evidence
   reference. This is an attributed supervisor verification, not a licensing API
   check. Use **Verify another person** for a new subject. Review an existing person
   to renew, downgrade or suspend a credential; their subject is immutable.
4. **Manage duty** binds the vehicle, driver and clinician to a server-timestamped
   duty ending within 24 hours. The database prevents a person having two open
   duties in this tenant, including across bases. End the prior duty explicitly,
   even if its end time has passed, before starting another one. Expiry blocks
   readiness immediately; it does not silently release the historical allocation.
5. Select **Record readiness for …**, enter the measured cylinder pressure and
   actual roadworthiness, equipment and defibrillator results. An unchecked item
   records failure. The observation records the authenticated staff observer,
   current vehicle-policy version and duty; nobody supplies the actor or timestamp.
   It expires at the earliest of policy validity, vehicle/crew certification and
   duty end. An ALS vehicle with an EMT, without a paramedic, has BLS effective
   capability. Expired or suspended credentials block it entirely.
6. A failed critical check opens a persistent coded constraint. A later pass,
   policy change, duty change or future trip completion cannot clear it. A
   scoped supervisor reviews the repair evidence, enters its reference, selects
   **Clear constraint** and confirms. The exact original check/code is cleared;
   retrying that clearance cannot clear a later recurrence. If the latest measured
   check is still failing, a new passing observation is also necessary.
7. Refresh the fleet before relying on it. Use **End duty** only once the crew
   have actually finished. No ambulance is assigned, crew device notified or
   receiving hospital committed by any operation on this page.

## Replay and failure behaviour

New vehicle/credential configuration starts with `version:0`; updating uses the
currently read version. A successful update increments it once. Retrying the same
payload and expected version returns the committed result; different content at
a stale version is 409. Use a new UUIDv7 for each duty and readiness observation;
retry an ambiguous result with the same UUID and original payload. A different
payload at that UUID is 409. Duty end is idempotent; clearance additionally binds
the evidence and clearing subject. Changing those on replay is 409.

The console freezes the original payload after an ambiguous failure. **Retry**
resends it. Definitive 400/413 rejection allows correction. Refresh current state
before creating a different command after conflict or lost permission. No fleet
payload is persisted in browser storage. Every successful mutation, audit record
and metadata-only outbox event commits atomically. NATS outage retains events in
the outbox; current availability still queries Postgres rather than a stale cache.

Only the supported patient-transport/BLS/ALS classes are configurable. Neonatal,
mortuary, non-vehicle response, external-asset constraints, licences/roster event
consumption, shift-specific checklist catalogue, owner/repair-deadline escalation
and contracted-provider agreements require their remaining programme work. This
implementation is not evidence that all #126 acceptance criteria are complete.

## Release and evidence

The additive `0002_emergency` migration creates fleet, credential, duty,
membership, readiness and constraint tables plus a security-invoker availability
view. The preceding `0001_emergency` intake migration is unchanged. All new tables
use composite scope keys and forced RLS. History cannot be deleted/truncated;
observations are immutable; policy and credential changes are version guarded.
Readiness policy binding and constraint clearance are checked in SQL as well as
in handlers. Application credentials cannot forge a clearance without evidence
through raw scoped SQL.

Run migrations only through the approved owning Helm hook after production
approval and a verified backup/recovery check. Preserve these additive tables on
code rollback; schema/data rollback is intentionally irreversible. No cron,
new secret, new public endpoint or extra database is needed for this fleet slice.
Retention maintenance remains separately disabled until its operational gates pass.
See the programme verification record for commands and actual coverage limits.
