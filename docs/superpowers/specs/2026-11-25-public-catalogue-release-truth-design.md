# Public platform catalogue: statuses follow the release — design

Issue: #1839 (prerequisite for #1838; follows #1837)
Date: 2026-11-25

## Why

`apps/shell/app/platform-catalog.ts` labels each public module `Available`,
`Foundation available`, `Planned` or `Future`, and since #1837 the home page
lists only the first two. The issue asked for the labels to be checked
against the code. The check found the catalogue out of step with the
release, not behind the code:

- Every capability in `contracts/release-capabilities.json` is created
  disabled. `eligible` means an operator may enable it in Console after the
  release gates; nothing beyond `baseline` is on by default
  (`docs/runbooks/feature-flags.md`).
- `clinical` (patients, OPD, IPD) and `lab` are not eligible. Neither are the
  pharmacy catalogue, stock, counter, prescriptions, returns, payables and
  reports capabilities. Their routes exist, but no customer can be given
  them in this release.
- Self-service signup (`onboarding`) is eligible, while the catalogue still
  calls it planned.

On 2026-11-25 the user chose to have the public catalogue describe what a
customer could be given in this release, not what is built.

## Decisions

### D1 What each label means

A module is `Available` when everything its description names is on a page
whose capability is eligible in this release. It is `Foundation available`
when some of it is, and the description says which part. Otherwise it is
`Planned`, or `Future` for the AI capability. A route, a menu entry or
merged code is not enough (AGENTS.md: no claim beyond evidence).

### D2 The rows that change

| Module                      | Was                  | Now                            | Evidence (eligible pages)                                                                   |
| --------------------------- | -------------------- | ------------------------------ | ------------------------------------------------------------------------------------------- |
| Patient management          | Foundation available | Planned                        | `/patients` is `clinical` (not eligible)                                                    |
| Outpatient & inpatient care | Foundation available | Planned                        | `/medicore/*` is `clinical` (not eligible)                                                  |
| Laboratory management       | Foundation available | Planned                        | `/lab` is `lab` (not eligible)                                                              |
| Pharmacy management         | Foundation available | Foundation available, reworded | `/pharmacy/setup`, `/pharmacy/staff`, `/pharmacy/low-stock`, `/pharmacy/working-stocktakes` |
| Tenant access & branding    | Foundation available | Available                      | `/settings/branding`, `/workforce/access`, signup (`onboarding`)                            |

Staff & hospital management stays `Available`, with evidence from
`/workforce`, `/workforce/setup` and `/workforce/access`. Every `Planned` row
already has no eligible page. Descriptions of rows that become `Planned`
keep their scope words and lose only the split between available and
planned. The new copy is limited to naming the eligible pharmacy pages and
signup.

### D3 Evidence is data, and CI checks it

Each `Available` or `Foundation available` row carries `evidence`: the shell
page paths that back it. A unit test asserts three things:

- every such row has evidence;
- every evidence path is a page in `release-capabilities.json`;
- every such page's capability is eligible, and so are its dependencies.

A capability that is withdrawn, or a page that is reclassified, then fails
CI until the label changes. It does not quietly go stale.

### D4 What follows

- The home page's platform section (#1837 D3) shrinks to the three modules
  that qualify. The patient-care group has none, so it drops from `/` and
  stays on `/roadmap`.
- #1838 can only show screens of eligible capabilities. A counter-sale
  screenshot waits for `pharmacy_counter`.
- The boards' own notes in chapters 1 to 4 (for example "Available today in
  the pharmacy: receipts with batch and expiry…") are story copy, not
  catalogue rows. They describe non-eligible capabilities as available
  today. They are filed as a follow-up rather than rewritten here, because
  #1837's non-goal forbids rewriting chapter copy without its own review.

## Not covered

- Making any capability eligible. That is the release process.
- `docs/product/public-platform-catalog.md` beyond recording the D1 rule.
