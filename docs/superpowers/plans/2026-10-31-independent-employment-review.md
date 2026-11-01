# Independent employment terms review delivery

Implements the [review design](../specs/2026-10-31-independent-employment-review.md)
as part of the full A01–A12 objective. No item is complete merely because a route,
permission, schema or screen exists.

- [x] Add a signed Global API regression for immutable employment submission
      consuming an exact approved person finding and released terms evidence;
      observe the current missing behavior fail.
- [x] Add canonical generated Go/Zod submission/read contracts, owner-only
      immutable head/history/children, exact replay and scoped RLS.
- [x] Extend those records with independently authorized legal decision,
      withdrawal and revocation transitions; preserve immutable source/receipts.
- [x] Add the separately declared employment-review permission and additive,
      digest-pinned template migration/catalog. Preserve historical templates and
      deny tenant-admin/identity-reviewer fallback on the new owner commands.
- [ ] Implement scoped discovery, preparation, exact evidence selection, protected
      detail/history/download and independent decision/withdraw/revoke behavior.
- [ ] Build the normal preparer/reviewer UI with required human attestations,
      pinned workspace/source, frozen uncertain commands and stale/private-data
      cutoff. Approved terms must remain distinct from active access.
- [ ] Prove historical contributor independence and creation completeness,
      all owner/IAM/storage phase boundaries, failed persistence, races, expiry,
      foreign scopes and exact retries with actual signed sessions and databases.
- [ ] Run owner/SDK/UI race/type/lint/build/coverage and generator/contractdocs
      checks; exercise actual Staff Admin/reviewer/provider/scanner UI journeys.
- [ ] Consume reviewed terms in the explicit operational-generation transition,
      complete legacy/governance and every HTTP/background consumer cutoff, and
      finish provisioning/recovery/renewal/suspension/exit/expiry/rehire A01–A12.
- [ ] Commit/push/merge only validated work; obtain named migration/rollout
      approval for the exact candidate, deploy and verify before full acceptance.

## Next normal-workspace implementation gates

- [x] Validate the selected employment-evidence byte-release API/SDK slice locally:
      current exact scoped authority and frozen basis before/after storage,
      released generation/length/raw hash, actor/assignment replacement and review
      ending cutoff, audit failure without bytes, bounded transfer admission,
      bounded client streams and cancellation. Reuse the person-review integrity
      primitive, with regression and real-browser checks for that existing
      consumer. No schema, new grant or migration is part of this slice.

- [x] Validate the selected-review history API/SDK slice locally: current scoped HR-intake or
      employment-review authority on every page, immutable original terms and
      selected evidence metadata, original workspace assertions, bounded cursor
      transport, sensitive-read audit and no byte-release or activation authority.
      The initial signed Global API test observed 404 for the missing history
      route. SDK response checks must reject foreign/duplicate/out-of-order or
      source-inconsistent receipts and discard cancelled requests. This slice
      adds no grant, schema or migration; the existing scoped history primary
      key bounds each legal review to at most three receipts. The normal
      reviewer UI, discovery, selection and protected bytes remain separate gates.

- [x] Add the signed missing-route regression for selected employment-evidence
      bytes. Use the selected document ID under its frozen review binding, recheck current original
      permission/assignment, actor link/epoch, review revision, target source and
      exact released-file lineage across storage I/O. Review authority must not
      release the person finding's identity-file bytes or arbitrary Staff files.
- [ ] Add protected cursor-paginated review history and hospital inbox/discovery;
      scope every read to current reviewed authority and bounded owner projections.
      Retain filtered empty-page continuation and immutable command receipts.
- [ ] Add a preparer-owned selector for current approved person findings and exact
      terms/evidence revisions. Do not require users to copy opaque database IDs,
      infer an identity from a name, or grant general identity-review authority to
      HR intake merely to populate its form.
- [ ] Build required source selection, terms/evidence examination, independent
      decision/ending controls and recorded/current-state history in the UI.
      Pin the original workspace and uncertain command; clear private data on
      cutoff and label approved terms separately from active employment/access.
- [ ] Complete the normal Admin/reviewer/recipient browser journey with actual
      provider/scanner evidence, then the operational-generation and A01–A12
      lifecycle/consumer gates already listed above. These gates remain open even
      when the independent-decision source suites pass.
