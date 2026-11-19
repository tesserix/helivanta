# Drug master operations

Design: [drug master spec](superpowers/specs/2026-10-27-drug-master-design.md) (#476).

The drug master is one catalogue for every tenant: molecules, formulations
(strength, dose form, route, Drugs Rules class, NDPS and teleconsult flags)
and branded packs (GTIN, HSN, status). The application can only read it, and
only what a published release contains.

## Who may change it

Content changes only through a **release**: one JSON file from one named,
licensed source, staged by one person and decided by another.

| Step    | Database login            | Can                                                                    | Cannot                                   |
| ------- | ------------------------- | ---------------------------------------------------------------------- | ---------------------------------------- |
| Stage   | `helivanta_drug_stager`   | Insert a `staged` release and its rows                                 | Publish, reject or change anything       |
| Decide  | `helivanta_drug_approver` | Move a `staged` release to `published` or `rejected`, write its events | Insert content, decide a decided release |
| Request | `hms_app`                 | Read rows of published releases                                        | Write any drug master table              |

The database refuses a decision whose named decider matches the named
stager (case and spacing ignored). A release is **sealed** once its
staging transaction commits: no row can join it afterwards. Publishing
takes the file the approver reviewed, and refuses unless the release's rows
are exactly what that file says, so the approver never publishes something
they did not see. Decisions run one at a time, and the database, not the
caller, records when and in what order. **Custody is an operations control**: the
two logins' credentials must be held by different people. One person holding
both defeats the two-person rule, and the database cannot detect it. Both
roles are `NOLOGIN` in production until that custody is reviewed, the same
as the document worker.

## Running a release

The command ships in the API image as `/app/drugmaster-release`.

```sh
# as the stager
DRUGMASTER_DATABASE_URL=postgres://helivanta_drug_stager@… \
  drugmaster-release stage -file release.json -by "Full Name"
# prints the release id

# as the approver, after checking the file against its source
DRUGMASTER_DATABASE_URL=postgres://helivanta_drug_approver@… \
  drugmaster-release decide -release <id> -file release.json -by "Other Name" -publish -reason "checked against <source> <version>"
# or, without a file: -reject -reason "…"
```

Publishing emits `catalogue_published.v1`, one `formulation_reclassified.v1`
per changed class or NDPS flag, and one `product_status_changed.v1` per
recall, discontinuation or return to active. All three are tenant-less.

## Release file

A release is a delta. Each entry is compared with what is published, and
only what differs becomes a new version.

```json
{
  "country": "IN",
  "source": { "name": "…", "licence_ref": "…", "version": "…" },
  "molecules": [{ "inn_name": "Amoxicillin", "atc_code": "J01CA04" }],
  "formulations": [
    {
      "code": "<source's stable id>",
      "valid_from": "YYYY-MM-DD",
      "label": "Amoxicillin 500 mg capsule",
      "dose_form": "capsule",
      "route": "oral",
      "regulatory_class": "H",
      "ndps": false,
      "teleconsult_prescribable": true,
      "ingredients": [
        {
          "molecule": "Amoxicillin",
          "strength_value": 500,
          "strength_unit": "mg"
        }
      ]
    }
  ],
  "products": [
    {
      "code": "<source's stable id>",
      "formulation": "<formulation code>",
      "valid_from": "YYYY-MM-DD",
      "brand_name": "…",
      "manufacturer": "…",
      "pack_size": 10,
      "unit_of_issue": "capsule",
      "gtin": "…",
      "hsn_code": "30041010",
      "status": "active"
    }
  ]
}
```

Rules the command or the database enforces:

- **Fields:** unknown fields are refused, and so are missing `ndps` or
  `teleconsult_prescribable` (never defaulted). GTINs must carry a valid GS1
  check digit. Dose forms, routes, strength units and classes are closed
  sets in `drugmaster.sql`.
- **Dates:** a new version of something already published must start today
  (India) or later. History is never re-answered. A version for a date
  that already has one supersedes it, in release order. So a recall dated
  today takes effect even when a later-dated version is already published.
- **Packs need rules:** a product version is refused unless its formulation
  has a published version in force from the product's first day.
- **Identities:** once published, a product code stays bound to its
  formulation (a different composition needs a new code), and a molecule's
  ATC code is fixed. A rejected release binds nothing. Molecule names are
  plain ASCII.
- **GTINs** are stored as GTIN-14, so the 8-, 12- and 13-digit forms of one
  item resolve to the same pack.
- **Duplicates and test data:** the same file (by SHA-256) is staged only
  once. Source name `synthetic-test-fixture` is refused outside test
  databases.

## Rollout

1. Merge the tesserix-k8s change that registers both roles as managed,
   `NOLOGIN` roles. The `0001_drugmaster` migration grants to them and fails
   if they are missing.
2. Deploy. The catalogue is empty until a licensed release is published.
3. Enable logins and project credentials only after custody review.

There is no rollback that removes published content. A wrong release is
corrected by a later release.

## Licensed feed synchronization (#1683)

`drugmaster-release sync` retrieves an operator-configured **canonical release JSON**
over HTTPS, validates it with the same strict release parser, checks the configured
source/licence, retains the exact bytes in a private durable archive, and stages
it. This is a working canonical feed transport, not a claim that a vendor API has
been selected or mapped. Proprietary vendor responses require a reviewed conversion
into `ReleaseFile`; arbitrary metadata is refused rather than guessed or dropped.

```sh
# Supply environment values through the approved secret reader; never paste them
# into shell history. DATABASE_URL must use only helivanta_drug_stager authority.
# DRUGMASTER_DATABASE_URL, DRUGMASTER_FEED_URL, DRUGMASTER_FEED_TOKEN
/app/drugmaster-release sync -source "$DRUGMASTER_SOURCE_NAME" \
  -licence "$DRUGMASTER_LICENCE_REF" -by scheduled-licensed-feed \
  -archive /releases
```

The command refuses private/link-local destinations, credential-bearing URLs,
query/fragment URLs, redirects, non-200 responses, files over 256 MiB, invalid or
partial releases, and mismatched provenance. Requests have a two-minute deadline.
The token is sent only in an Authorization header. A failed fetch stages nothing;
an archive failure stages nothing. A staging failure retains the review file for
investigation/retry. The private archive is content-addressed and mode 0600. It
must be backed up and retained under the vendor's approved retention policy.
A checksum replay returns the existing release, including a rejected release;
it never republishes or reopens it. Changed source bytes are a new review.

The [six-hour CronJob template](deployment/examples/drugmaster-sync.yaml.tmpl)
is **suspended**. Place it in the owning tesserix-k8s chart before activation;
this repository does not own live Kubernetes desired state. Activation needs:

- a licensed Indian source with explicit automated access, local retention and
  consumer/redistribution rights, stable identifiers and update semantics;
- a reviewed adapter supplying all required canonical fields without inferred
  schedule, NDPS, teleconsult, HSN or composition values;
- the exact approved API image digest, private archive PVC/backups, DB/network
  policy and restricted OpenBao/ESO secret named `helivanta-drugmaster-sync`;
- secret keys `DRUGMASTER_DATABASE_URL`, `DRUGMASTER_FEED_URL`, optional
  `DRUGMASTER_FEED_TOKEN`, `DRUGMASTER_SOURCE_NAME`, `DRUGMASTER_LICENCE_REF`;
- independent custody of the stager and approver logins; the scheduled job never
  receives the approver credentials;
- an operator alert for a failed Job or no successful fetch in 12 hours, a vendor
  freshness SLA and archive/storage budget. Polling every six hours does not
  make a monthly vendor dataset current every six hours.

After each new staged release, the independent reviewer uses the archived file
with the existing `decide -publish` command. No cron publishes medicine changes.
External outages preserve the last approved catalogue; existing prescriptions
and outlet links keep their recorded source versions. To stop fetching, suspend
the job through GitOps; preserve releases and review history.

No licensed Indian dataset, API credentials or approved production release were
supplied with #1683. RxNorm and openFDA are useful terminology/listing sources,
not complete Indian branded pack/safety catalogues. See the
[source assessment and design](superpowers/specs/2026-11-19-drug-catalogue-integration.md).

## Outlet catalogue consumer

Outlet-scoped `GET /v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}/catalogue/drugmaster?q=…`
returns up to 20 current active packs by brand/ingredient prefix or exact GTIN.
It requires `pharmacy.catalogue.read` and current outlet access. The corresponding
`PUT …/catalogue/products/{uuidv7}/from-drugmaster` requires
`pharmacy.catalogue.write` and editor access and accepts only
`{version_id,currency:"INR",mrp_minor}`. The selected version is revalidated under
the publication lock until the outlet transaction commits. The same product ID
and payload replay the saved result; a second outlet product ID for the same
master identity conflicts. Unknown client identity fields are rejected.

Outlet products retain `drug_master` with exact source product/formulation
versions, release, provenance, ingredients, pack, schedule and HSN. These are
retained identity evidence, not an independently editable master. Linked identity
cannot be edited or detached. Existing local commercial/status edits and manual
product entry remain available. Local schedule/tax/category/price-control reviews
are still required; import never approves stock, tax, prescribing or dispensing.
Live recall propagation and automatic migration of existing outlet identity to a
new vendor version are not implemented by this catalogue-add command.
