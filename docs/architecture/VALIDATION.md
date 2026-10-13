# Design coverage and artifact validation

**Checked:** 2026-10-13. This record verifies documentation assets and design coverage, not implementation or production compliance.

## Requested coverage

| Requirement                                          | Design location                                                                                                         |
| ---------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| One UI and separate product APIs                     | [System design](01-system-design.md), diagram 01                                                                        |
| Full HMS portfolio and legacy ownership              | [System design](01-system-design.md), [evidence mapping](06-evidence-and-sources.md), diagram 05                        |
| Multiple hospitals per tenant                        | [Tenancy model](02-tenancy-and-identity.md), diagram 02                                                                 |
| Isolated, selective and group sharing                | [Sharing policies](02-tenancy-and-identity.md#three-sharing-modes-independent-data-categories), diagrams 02–03          |
| CRM, referrals, contacts and outreach                | [CRM design](02-tenancy-and-identity.md#crm-within-the-hms), [workflow ownership](03-data-and-workflows.md), diagram 05 |
| Zitadel onboarding, authentication and authorization | [Identity and lifecycle](02-tenancy-and-identity.md), diagrams 03–04                                                    |
| PostgreSQL, Valkey and future Qdrant                 | [Data design](03-data-and-workflows.md), diagrams 01 and 07                                                             |
| Istio and service-to-service trust                   | [Deployment](05-deployment-and-transition.md), diagrams 03 and 06                                                       |
| HIPAA and SOC 2 readiness                            | [Control/evidence matrix](04-security-and-compliance.md), diagram 06                                                    |
| Availability, backup, recovery and transition        | [Deployment and transition](05-deployment-and-transition.md), diagrams 04–06                                            |
| Markdown and editable draw.io                        | [Index and all diagram assets](README.md#editable-diagrams)                                                             |

## File-storage follow-up

The [GCS and ClamAV chapter](07-file-storage-and-scanning.md) and diagram 08 add generation-bound scanning, quarantine, promotion, hospital-sharing enforcement and recovery. NATS/JetStream and Temporal are confirmed. The new diagram was rendered and inspected, and link/XML/image checks were rerun.

## Artifact checks performed

- Generated eight SVG previews and rendered eight **2400 × 1620 PNGs** with CairoSVG. Markdown embeds PNGs so the previews do not depend on SVG support.
- Inspected the original seven-page visual contact sheet and individually reviewed the tenant, deployment and AI pages. Corrected a text-height overflow and deployment connector routing.
- Parsed the native draw.io XML: eight pages, 82 content nodes and 70 connectors; unique cell IDs and valid source/target references. The file contains native editable shapes, not embedded screenshot pages.
- Checked diagram text dimensions, SVG XML, PNG decoding and every local link/image reference in the new Markdown package, including section anchors. All passed on the final run.
- Ran `pnpm dlx prettier@3.4.2 --check` over the new Markdown files and `git diff --check`.
- Checked new document text for known private source markers; none found. This targeted check supplements sanitized authoring and is not a general proof of anonymization.

The temporary artifact validator was run with `/usr/local/bin/python3 /tmp/helivanta-architecture-tools/validate_artifacts.py` and its isolated rendering dependencies. PNGs are rendered from the SVG assets; the draw.io file was structurally checked, not opened in the draw.io desktop application during this task. No application test suite was run because no application code changed.

## What is not verified

The legacy review is read-only and selective. No new clinical transaction, calculation, integration, export or production workflow was tested. Capacity, cost and recovery numbers are planning assumptions. Cloud configuration, installed Istio policies, deployed identity behavior, cross-hospital RLS, migrations and recovery must be proven during implementation. HIPAA applicability, retention and SOC 2 examination scope require the responsible organizational reviewers listed in the design.

To edit diagrams, use the draw.io file as the editable source and export each changed page to its matching SVG and PNG filenames. Recheck Markdown previews after saving; do not leave updated diagram links pointing at missing assets.
