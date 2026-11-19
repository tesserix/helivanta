# Outlet catalogue and licensed source synchronization (#1683)

Drugmaster owns the global Indian medicine identity. Pharmacy owns outlet products,
prices, discounts, rack locations and stock. Consumers retain source product,
product-version, formulation-version and release references with an immutable
source projection; manual products remain supported. IN-PH-01, IN-PH-02 and
IN-PH-06 apply. No vendor dataset or inferred Indian safety policy is bundled.

Read-only production discovery on 2026-11-19 used the active
`unidevidp@gmail.com` account, project `tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, namespace
`helivanta`. Queries against `helivanta.drugmaster_releases` returned no releases,
and the published product-version count was zero. No catalogue content was changed.

## Source decision and trust boundaries

No verified freely reusable API is known to contain every Indian branded medicine
and all required metadata. RxNorm provides normalized terminology and monthly/weekly
releases; it is not a complete Indian brand/pack/schedule source. openFDA NDC is a
US drug-listing source. Both require field and license review before any reuse.
MIMS/CIMS or a contracted Indian supplier are procurement candidates, not selected
or approved integrations. A public retail webpage is not a bulk reuse licence.

Primary references inspected on 2026-11-19:

- https://www.nlm.nih.gov/research/umls/rxnorm/docs/rxnormfiles.html
- https://open.fda.gov/apis/drug/ndc/
  MIMS India integration page returned 403; its coverage/API/retention rights are
  unverified. A vendor must demonstrate stable IDs, Indian brand/pack coverage,
  composition including denominators, provenance/effective dates, withdrawal/recall
  semantics, update SLA and commercial storage/redistribution rights. HSN and Indian
  schedule/NDPS/teleconsult policy require accountable qualified review. Missing
  safety values cannot default to false or OTC.

Vendor responses are untrusted inputs. Only a configured HTTPS public endpoint
with a bounded request and body may supply the existing strict ReleaseFile.
Redirects are refused. Source identity and licence must match operator configuration.
A six-hour scheduler fetches and stages only; independent publication remains the
existing drugmaster-release decide command. Failed/partial responses preserve the
last published release. SHA-256 deduplicates retries, including concurrent fetches.
The original release bytes and provenance remain the approval evidence. Credentials
are environment supplied, never URL arguments, logs or committed files.

## Outlet behavior

Scoped search returns at most 20 current active packs by brand or ingredient prefix,
or exact validated GTIN. The UI calls Global API's existing pharmacy destination.
Both search and add check verified tenant, pharmacy and current outlet access.
An editor selects an exact product version and enters local MRP; SKU is generated.
Add reauthorizes and holds the owner's publication lock until the outlet transaction
commits. Recalled, discontinued, future, unpublished or stale versions are refused.
The client cannot submit composition or legal classifications for this command.
An identical command replays the saved result; another product ID for the same
source product conflicts. Replacing linked identity is forbidden. Commercial fields
remain editable through the existing product command; manual creation is retained.

Source schedule and HSN are displayed as provenance. Existing local schedule,
category, tax, price-control reviews and release checks remain required. Import
itself does not release stock or grant prescribing/dispensing authority. A source
refresh never rewrites historical outlet projections or silently reclassifies
existing stock. Full live recall propagation remains an explicit separate workflow.

## Capacity, failures and rollout

Planning assumption: 60,000 products (existing tested catalogue size), 100 peak
search requests/s, <=200 byte search input, <=20 results (~80 KB maximum),
99.9% read availability and p99 <300 ms targets, subject to load validation.
At 4 KB/product, one full feed is ~240 MB; four pulls/day transfer ~960 MB/day
before conditional delivery. At 12/36 months with 2x/4x growth this is 120k/240k
products. A bounded 256 MiB feed fails rather than truncates; larger sources need
a reviewed partitioned protocol. Delta releases are preferred. Existing indexed
Postgres serves reads independent of vendor availability. No new service/cache.
Stored versions grow with actual changes; vendor polling frequency is not a claim
of vendor freshness. Retention and source freshness SLA remain procurement inputs.

Additive migration adds a nullable source projection and a scoped uniqueness index;
existing products are unchanged. Historical source links cannot be erased. Rolling
back application code keeps the additive columns, but does not remove accepted
history. The user authorized deployment to production, including this additive application migration. Loading catalogue content still requires the licensed dataset and independent stewardship.
Production deployment follows the existing approved GitOps release flow after validation; the user confirmed production as the target.

Acceptance needing external input: licensed Indian data, independent steward
custody/review and an approved production release. Keep #1683 open until these
are evidenced. Do not claim a configured adapter/schedule is a live vendor feed.
