# ADR-0024: Pharmacy and outlet are first-class scopes; hospital links are optional

Date: 2026-10-15. Status: accepted implementation direction, following the user's explicit clarification. Related: #19, #159, #162, #166, #418, #476, #979.

A tenant can own independent pharmacies, pharmacy chains, hospitals, or a combination. A hospital can own one or many pharmacies, and a pharmacy can have one or many licensed physical outlets. A standalone pharmacy must never need a synthetic hospital to operate in Helivanta.

Use `tenant -> pharmacy organization -> outlet` as pharmacy's ownership/access hierarchy. Every catalogue/stock record belongs to a tenant and outlet; the immutable outlet row identifies its owning pharmacy. PostgreSQL RLS validates tenant, pharmacy and outlet together. Outlet-scoped business foreign keys plus the outlet-to-pharmacy foreign key enforce the hierarchy without independently mutable copies of ownership on each row. Private outlet catalogue rows can reference the governed shared drug master in the subsequent catalogue-governance slice. Do not turn local SKUs into a parallel platform drug master.

Hospital ownership/serving/partner affiliations are separate relationships. They may be declared during onboarding and remain pending validation until canonical hospital identity and the appropriate owner's authority are verified. A declared affiliation never grants patient, prescription, stock, financial or document access. Same-tenant or cross-tenant data exchange needs a separate explicit, directional, revocable agreement and purpose/consent checks. Cross-tenant affiliation activation cannot be inferred from a submitted UUID.

Hospital-owned pharmacy still uses pharmacy/outlet scope. A hospital encounter/prescription reference is optional on the pharmacy workflow; an independent pharmacy must not need an invented encounter. Standalone Rx identity and consents must use Platform person/patient contracts and their pharmacy-authorised projections, not a duplicate hospital patient master. OTC sales need only the legally required identity fields. A chain may share governed catalogue/configuration deliberately; patient and stock access never broaden from common ownership alone.

The initial API/database implementation supports organization/outlet registration, outlet grants, outlet-private product revisions and quarantine receiving. Affiliations are onboarding declarations, not approved data-sharing or dispensing authority. Licence/credential verification, country policy, clinical fulfilment and real hospital integrations remain explicit subsequent acceptance gates. Existing basic free-text dispensing is not the governed fulfilment workflow.

Update (2026-10-29, #1416): a same-tenant affiliation can now be verified. The hospital's registrar-administrator accepts, declines or revokes it in patientmaster, the pharmacy can withdraw it, and both sides keep append-only history. A verified affiliation is the precondition for hospital-scoped patient lookup, which is a separate slice (#1416b). It is still not patient, prescription or stock authority by itself. Cross-tenant affiliation remains unimplemented.

The prior unmerged hospital-mandatory draft is replaced before release. No production pharmacy migration or rollout has happened. Clinical hospital boundaries elsewhere in HMS remain unchanged.
