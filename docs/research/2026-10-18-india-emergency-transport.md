# AmbulanceConnect: Indian emergency transport research

Programme [#1220](https://github.com/tesserix/helivanta/issues/1220). Research date: 18 October 2026.

## Primary sources and limits

| Source                                                                                                                                                                                                                | Findings used                                                                                                                                  | Limits                                                                                                                                                                      |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [MoHFW/NHSRC NAS Operational Guidelines 2026](https://nhsrcindia.org/sites/default/files/2026-09/Operational%20Guidelines%20for%20National%20Ambulance%20Services.pdf), printed pp. 25–26, 30–33, 35–38, 53–58, 60–64 | AIS-125 types; appropriate staffing/equipment; response intervals; integrated command centres; quality and grievances                          | National framework; state implementation and contracts require verification. Not an API specification or permission to integrate.                                           |
| [PIB, 29 June 2026](https://www.pib.gov.in/PressReleasePage.aspx?PRID=2279073)                                                                                                                                        | Official launch, capability-aware referral, GPS, structured triage, progressive 112 integration                                                | Describes policy direction, not universal deployed interoperability.                                                                                                        |
| [NHM ERS/patient transport](https://nhm.gov.in/index1.php?lang=1&level=2&sublinkid=1217&lid=189), accessed in browser                                                                                                 | 108 predominantly emergency response; 102 principally pregnant women/children and JSSK transfer/drop-back; operation reported in 35 states/UTs | Historical fleet totals differ from the newer guidelines. Do not use these totals as a live fleet or coverage feed.                                                         |
| [Odisha emergency directory](https://odisha.gov.in/en/contacts/emergency), official search result inspected                                                                                                           | Published ambulance 108 and Janani 102                                                                                                         | Direct page timed out during research. Search evidence alone does not establish current operational availability; regional dial actions require direct source verification. |

The national guidelines describe continuity of 108/102 during progressive integration into 112. Keep 112 as the national emergency calling route in the India profile. Do not advertise a Helivanta-to-112 dispatch connection. #133 requires a signed authority arrangement, technical conformance and operational approval for each adapter.

## What the citizen experience must reflect

Emergency transport is not ordinary ride-hailing. The nearest car may be clinically unsuitable. A first responder (AIS Type A) may not transport a supine patient. Type B is patient transport, Type C is BLS and Type D is ALS; these require suitable equipment **and** appropriately qualified staff. Special requirements (neonatal, obstetric, bariatric, ventilator/oxygen, infection control and accessibility) need approved matching rules, not a higher-priced vehicle icon.

A request has separate milestones: received, reviewed, vehicle reserved, crew acknowledged, en route, on scene, transporting, at destination and clinician handover. Show only server-confirmed milestones. A receipt does not establish that a crew is coming; a hospital listing or caller preference does not establish acceptance or clinical suitability.

Keep a call action visible with no login, OTP, location permission, ABHA or payment prerequisite. Manual landmark, building, floor and access instructions must work when GPS is denied. A relative can request for someone else without obtaining their clinical records. Accessibility, low literacy, large text, screen readers, local-language voice/call support and intermittent data are first-class journeys.

## Operational gaps to resolve

- **Unattended intake:** a request accepted while no dispatcher is staffed can be more dangerous than a clear unavailable state. Activation requires staffed coverage, escalation and monitored queue-age alarms.
- **Ambiguous submission:** timeout followed by a phone call and late app acknowledgement can create two responses. Keep one submission key, surface related cases to the dispatcher, record call fallback and require human reconciliation. Do not auto-merge casualties or silently auto-resend indefinitely.
- **Availability freshness:** readiness, crew qualifications, equipment and unexpired documents are independent of GPS. Stale/unknown candidates are not bookable. Do not infer available from no active trip.
- **Reservation and cancellation:** reserve atomically; one active trip per vehicle and compatible crew. A caller's cancellation request is not a stand-down. Confirm crew recall, preserve clinical escalation and then release the resource.
- **Hospital receiving state:** distinguish verified directory capability, last-known capacity, current diversion and explicit receiving acceptance. Route alternatives through qualified dispatch/clinical oversight and update both old and new pre-alert recipients.
- **Patient safety:** no autonomous diagnosis, dispatch, priority downgrade or clinical instructions from a language model. #743 is optional assistance with separate governance. Triage algorithms and phrases require medical-director approval and versioning.
- **Public map privacy:** coarse nearby coverage only before assignment. Exact vehicle location is limited to the active trip's authorised viewers; no fleet scraping or other patients' routes.
- **No suitable response:** no-fleet, out-of-area, unreachable caller, rural access, flooding, crash/hazmat, mass casualty, refusal, death and inter-facility transfers need explicit procedures and escalation. A timer must never imply rescue has occurred.
- **Operational CRM:** callbacks, delays, complaints, refunds and lost property belong to case operations. Clinical details and location must not flow into sales CRM or marketing without a separate valid basis.
- **Tariffs:** distinguish public entitlements from private quotes. No promise that all services are free; no deposit prerequisite for emergency access/dispatch. Scheduled transport is a separate journey.

## Measurements

The NAS guidelines recommend up to 20 minutes from call receipt to reaching the patient, with improvement towards 10 minutes. These are service planning benchmarks, not a universal statutory SLA or an app guarantee. The same document also labels dispatch-to-scene as a component “response time”; metric names must include their endpoints to avoid ambiguity.

Capture received → assigned, assigned → moving, received → scene, scene → departed, departed → destination and destination → handover separately. Report p50/p90/p95, breach denominators, rural/urban geography, no-fleet, cancellations, missing telemetry and hospital diversion. Never turn missing data into zero response time. Technical API latency and operational response time have different owners and budgets.

## Identity, tenancy and data governance

Reuse current Zitadel sessions and OpenFGA for staff; no new Keycloak/GIP dependency. Each independent provider or hospital legal data owner is a tenant. A hospital group may own multiple facilities; provider bases and vehicles remain within their provider. One person may have independently authorised memberships in multiple tenants. Membership in one does not create access to another.

A signed, effective service agreement permits narrowly defined trip-scoped disclosure between provider and receiving hospital; it is not a shared tenant database or a blanket care relationship. Persist forced RLS and composite tenant/object foreign keys; enforce facility/base and case assignment permissions on each read and transition. Citizen request/status capabilities are separate from staff sessions and never allow clinical-record lookup. Store bearer capabilities hashed, short-lived, revocable and outside URL query strings, analytics and logs.

The issue text's `vital_interest` is not by itself an Indian statutory basis. Record an approved India policy reference, purpose, notice version, initiator and emergency context; DPO/legal must verify applicable DPDP commencement/rules, children's/bystander handling, retention, processor agreements and statutory duties before collecting live data. No Aadhaar/ABHA requirement for emergency help. Do not call software SOC2-certified or HIPAA-compliant based on an engineering checklist; applicability, contracts and operating evidence are separate.

New secrets belong in namespace-bound OpenBao paths, with workload identity and exact-path policies. No new GCP Secret Manager dependency. Logs and crash reports use allowlisted operational identifiers; no precise coordinates, caller phone, clinical payload, token or raw request body. Native unsynced clinical data needs encrypted storage and a loss/reconciliation policy before crew capture is enabled.

## Research still required before live booking

Operator and receiving-hospital interviews in the chosen pilot; state-specific operating arrangements, tariffs and current contacts; medical-director protocol approval; legal applicability review; maps/telephony processor and coverage review; manual dispatch drills and clinical safety sign-off. No test in this work calls emergency numbers or creates a real emergency response.
