# Effective partner shipment implementation

Design: [ADR-0076](../../adr/0076-effective-partner-shipment-disclosure.md).
This is an execution plan, not a completion record.

1. Add failing exact-scope/minimal-wire and actor/header tests. Extend the canonical
   supplier-access projections using source-owned field references; generate Go/Zod
   and add strict SDK scope, cursor, duplicate and abort/deadline acceptance.
2. Consume `PartnerScopes.WithPartnerScope` in SupplyConnect's two bounded reads.
   Add the isolated source SELECT purpose, dependent-table/private-field and write
   denial, exact owner/order predicates, audit and concurrent index. Preserve
   existing source/seller, buyer and exchange purposes without widening them.
3. Run hosted native owner/RLS tests and positive real IAM/Staff/graph admission,
   including both source products, named and group recipients and current cutoff.
   A synthetic admission fixture is component evidence only.
4. Implement current recipient grant discovery and human source labels through
   owning ports, then a reachable account-bound UI with named choices and no raw-ID
   entry. Test refusal conceals loaded evidence and context changes clear selection.
5. Complete local builds/checks, remove generated outputs, commit/push, pass protected
   hosted native/E2E/visual gates and integrate normally. No admin/check bypass.
6. Prepare a concrete named release/fixture/activation request, deploy only with its
   required approval and record authenticated acceptance. Keep catalogue/invoice
   consumers and wider supplier acceptance explicit; do not close the whole goal
   from this shipment checkpoint.
