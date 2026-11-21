# Supplier return history and draft cancellation

Related: #1687; ADR-0075. Implementation in progress, unreleased.

PharmaConnect remains the owner of return drafts, frozen source lines and cancellations. This slice provides bounded history and a readable workspace view for existing drafts. It does not create dispatch, actual supplier credit, bill allocation or statutory documents.

Use the existing parent accounts workspace and pharmacy-payables release gate. Retained history and draft cancellation do not imply authority to move stock or post finances. Each server request requires verified membership, the return permission, current exact-outlet access and expected tenant/subject preconditions. The platform's existing implicit tenant-admin permission grant remains; tenant administration alone is not an outlet grant.

Add an audited, private `GET /accounts/supplier-returns` collection under the existing pharmacy/outlet root. Platform cursor pagination bounds responses to 200 rows, default 50; the UI requests 25. Only `limit` and `cursor` are accepted. Summaries omit actor subjects, source references and line bodies. Canonical summary models come from contracts/pharmacy.json. Keyset order is descending creation time/id; cancellation state is projected from the immutable owner table in the same read. Audit failure returns 503 without data. Existing indexed tenant/outlet creation and cancellation-parent keys serve the query; no new migration is required.

The workspace shows return number, supplier name, original bill reference, state, expected amount and prepared date. Details show product/generic name, batch, expiry, units and expected amount. Backend identifiers remain transport and React keys, never display labels. Expected credit is explicitly distinguished from accepted supplier credit or settlement.

Draft cancellation uses the existing immutable command API, a bounded reason and current version. Before sending, retain only a command reference in tab-local storage scoped to tenant, subject, pharmacy, outlet and return. No commercial body or reason is persisted. Storage failure prevents sending. An uncertain result blocks another command; recovery queries the original actor-bound reference. A successful verified result clears the hint and refreshes history/details. Reads/commands use the shared account-bound SDK, deadlines, no-store and no automatic retries. Account changes remount the panel and query keys include the full actor scope.

Alternatives rejected: raw-ID entry does not meet the readable UI requirement; a client-side whole-collection scan is unbounded; deriving a dispatched state or credit from a draft fabricates business facts; saving entire commands exposes commercial data. No new service, datastore or background job is needed. Inherit ADR-0075 workload and latency budgets; hosted measurements and live acceptance remain required.

Acceptance: bounded cursor continuation, cancellation-state projection, foreign tenant/outlet denial, permission denial, invalid/repeated query rejection, audit rollback; validated SDK scope/page evidence; readable details, denied/stale-data handling, cancellation response-loss recovery and storage failure. Local pure tests/typechecks/builds run without local applications or containers; native DB/browser acceptance remains hosted.
