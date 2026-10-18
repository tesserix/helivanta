# EmergencyConnect citizen and operational boundary

Status: proposed for the live booking programme; implemented for #1221 call access.
Programme #1220. [Full design](../superpowers/specs/2026-10-18-ambulanceconnect-design.md).

AmbulanceConnect is the public web/native entry to Helivanta EmergencyConnect.
It is not a new identity provider or a consumer view of the hospital tenant.
Public calling uses no account and no health data. Future booking uses a narrow
case capability, while operators retain Zitadel/OpenFGA identities, tenant/facility
scope, forced RLS and explicitly accepted cross-provider/hospital disclosures.
A citizen's hospital preference never authorises or clinically confirms admission.

Use Expo for new mobile clients and reuse source-backed shared contracts. Keep
Go module data ownership and transactional outbox, adding a separate capacity
boundary only with the measured reliability evidence required before live intake.
Do not introduce a second datastore or proprietary government API assumptions.

Consequences: the call-first slice can work offline in native without staff
provisioning. Booking is activated only after the safety gates in #1222, not by
showing a map or returning 202. A combined public+clinical app with permissive
membership is rejected because it couples emergency access to login and risks
exposing tenant records. Autonomous nearest-vehicle dispatch is rejected because
proximity does not establish clinical suitability, readiness or acceptance.
