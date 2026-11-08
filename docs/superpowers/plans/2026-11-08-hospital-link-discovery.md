# Hospital link discovery and slice 3b-ii re-plan (#19)

Issue: #19. Base: main `4980728d`, which carries #1621 (ADR-0063, ADR-0064, ADR-0065).
#1627 (slice 3b-i, pharmacy login links) was closed. #1621 supersedes it: it
delivers pharmacy and supplier account links under ADR-0063.

## Defects found while reconciling #1627 against #1621

1. **Hospital pickers listed independent employers.**
   - `leaveReviewScopeCandidates` selected every active login link of the subject.
   - It did not check the employer kind. A reviewed supplier or pharmacy account
     link therefore became a hospital-scope candidate.
   - Effect: four pickers listed the supplier as a hospital scope.
   - Effect: three pickers returned 503, because the candidate reached the
     hospital authority.
   - Fix: join `workforce_hospitals` with `establishment_kind='hospital'` (ADR-0054).
2. **The guard inventory counted a nullable foreign key as a guard.**
   - The Staff-child inventory treated a foreign key through a nullable column
     as structural. On main this hid that the login-link tables reach Staff
     through the nullable `employment_generation_id`.
   - Fix: the inventory admits a foreign key only when every referencing column
     is `NOT NULL`, or is a stored generated literal.
   - The login-link tables are now on a reviewed list. Their direct guard test
     needs migration 0094 to admit each establishment only through its own
     identity basis.

## Tests (red first, then green)

- `globalapi.TestHospitalScopePickersIgnoreIndependentEmployerAccounts` checks all
  seven hospital pickers. Each must return an empty page to a subject whose only
  link is a reviewed supplier account. The hospital authority must never be asked
  about the supplier.
- `workforce.TestEveryEstablishmentTableIsGuardedOrReachedThroughAGuardedTable` uses probe
  tables to separate three foreign-key cases:
  - a nullable column, which is refused;
  - a generated literal, which is admitted;
  - a generated non-literal, which is refused.
- `workforce.TestLoginLinksAdmitEachEstablishmentOnlyThroughItsOwnIdentityBasis`
  checks four cases:
  - a pharmacy link with no Staff version is refused;
  - a pharmacy link bound to a generation is refused;
  - a pharmacy link on a stale Staff version is refused;
  - a hospital link that carries an independent basis is refused.
    A pharmacy link on its current basis is admitted.

## Slice 3b-ii re-plan

The original design is withdrawn. It would have put `PharmacyID` on the hospital
`authz.ScopeRequest` and made the pharmacy a root of hospital templates. ADR-0064
replaces it with a typed independent operating-assignment family. That family
already prepares, reviews, projects and cuts off `pharmacy.inventory_receiver.v1`
against a PharmaConnect owned-outlet source. The pharmacy staff page already
offers the duty panel.

The gap that remains: no PharmaConnect stock route consumes that duty. Today
`pharmacy.stock.receive` is a tenant role permission. SupplyConnect is different:
`stock_authority.go` admits a reviewed `supply.inventory_receiver.v1` duty at
`/v1/supply/parties/:id/operating-locations/:location/stock/receipts/:id`.

Slice 3b-ii is therefore one vertical slice:

- **Route.** Add an outlet-scoped pharmacy receipt route.
  - It admits the reviewed duty through `OperatingAuthorizer.WithOperatingScope`
    with employer kind `pharmacy`.
  - It records a quarantined receipt with the actor attributed.
  - It answers 404 after revocation, and for a foreign or hospital-owned outlet.
  - It does not widen the tenant role grant.
- **Tests.**
  - A globalapi acceptance test mirroring
    `TestIndependentReceiverUsesReviewedDutyAtSupplierStockAPI` for a pharmacy.
  - Pharmacy-kind cases in `operating_duty_test.go`.
  - A unit test for the pharmacy `operatingLocationGuard`.
- **Frontend.** The receiver UI and its panel tests.
- **Contracts.** Regenerate the contracts and re-pin the inventory.

Slice 4 (HR lifecycle on independent employers) and slice 5 (branch postings)
remain open. They are unchanged by this record.
