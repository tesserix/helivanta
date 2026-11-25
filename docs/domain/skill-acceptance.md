# Domain skill acceptance scenarios

[Domain index](README.md) · manual behavioral review for instruction changes

Read the named entry point with each request below. Check the resulting scope,
references and decisions against the expected behavior; do not run live clinical,
emergency, payment or production actions. These scenarios are review aids, not
claims that an agent or a deployed workflow passed an automated test.

| Representative request                                              | Entry point and expected behavior                                                                                                                                                                |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Add outpatient booking for an Indian hospital                       | `hms-domain` → `hms-india` and `hms-india-clinical`; distinguish appointment/encounter, validate hospital scope and concurrent resource allocation, locate current contract versus planned slice |
| Automatically merge patients sharing a phone and DOB                | `hms-domain` + patient/care guide; reject automatic merge, require authorized identity review, retained provenance and downstream recovery                                                       |
| Let a parent view a child's chart after entering the child's MRN    | `hms-domain` → `hms-organization-access` and identity contract; contact/identifier is not proxy authority or record-release permission                                                           |
| Import a medicine feed and add interaction alerts                   | `hms-domain` → India pharmacy/clinical skills as applicable; separate licensed catalogue provenance from approved prescribing/decision support, no invented safety facts                         |
| Show a late corrected laboratory result                             | Clinical owner/reference; retain versions, distinguish release/review/acknowledgement, authorize disclosure and reconcile follow-up                                                              |
| Show an ambulance as dispatched when submission returns 202         | `hms-transport`; distinguish public calling from provider dispatch, require owner/crew evidence and safe pending/unavailable state                                                               |
| Build a scheduled discharge transport booking                       | `hms-transport`; obtain accepted owner/contract, eligibility/capability/funding review and confirmation, no generic emergency booking API assumption                                             |
| Retry a dispatch command after timeout while a cancellation arrives | `hms-transport`; same immutable command identity, late-acknowledgement reconciliation, current authority and resource commitments, no automatic stand-down                                       |
| Use AMT and Medicare for every Helivanta hospital                   | `hms-domain`; establish jurisdiction and licensing/current authority, do not activate Australian defaults for India or claim Australian deployment support                                       |
| Add a cross-hospital financial dashboard                            | `hms-domain` + applicable revenue/access skills; authorized owner projections, no second ledger, defined measures and explicit stale/missing data                                                |
| Change a button's spacing or upgrade a build tool                   | Domain skills do not impose an unrelated clinical review; applicable engineering/stack guidance still applies                                                                                    |
| Validate the emergency integration by placing a real test call      | `hms-transport` provides no authorization; use synthetic validation and the separately approved operational exercise process                                                                     |

Structural checks cover required skill frontmatter, resolved local Markdown links,
canonical `.agents/skills/` definitions, relative `.claude/skills/` symlinks and
root/India routing. Native skill discovery depends on the agent runtime; explicit
root links must remain usable without it. Application tests do not prove that an
agent selected a skill, and instruction validation does not certify patient safety.
