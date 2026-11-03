# Scoped posting positive-role UI verification

2026-11-03. Local synthetic QA through `http://helivanta.localhost:4399`, Global
API 8199 and owner API 8399. No production rollout, migration, issue closure or
full Staff acceptance. [Candidate contract](../api/workforce-postings.md),
[delivery plan](../superpowers/plans/2026-11-03-scoped-staff-posting-preparation.md)
and [overall tracker](staff-management-progress.md).

## Fresh reviewed source

The older digital fixture's employment review is revoked. Its independently
active reader assignment and fresh approved link correctly reveal no posting
hospital. That fixture was retained as negative evidence; no eligibility fact was
rewritten to force a positive result.

A new synthetic account was created using the existing local identity fixture.
An initial attempt with a stale seed credential returned 401. The active local
identity container's mounted credential was identified through read-only Docker
inspection. Authorized exact-name reconciliation found no account before creation;
the new account then passed a real password check. Its random password and IDs
remain in the private JSON format requested by the user. No existing password,
role or provider policy was changed.

All application business mutations below used normal browser UI, with actual
account sign-outs/sign-ins and independent decisions:

1. Admin proposed five short-lived officer assignments: HR intake and identity
   preparation for the preparer; identity and employment review for the separate
   reviewer; separate activation authority. A second Admin reviewed each proposal.
   Each reached active version 5 after hospital verification and graph projection.
2. Admin enrolled the new account as an ordinary member. This granted no business
   permissions or hospital access.
3. The HR preparer created `SYN-POSTING-SOURCE-20261103` as Inactive. Empty submission
   required name/code; a full reload preserved one canonical record.
4. Identity and employment test images were uploaded with their distinct purposes,
   passed the real scanner, and received the existing optional synthetic HR label.
   The person finding declared the exact new account and selected released evidence.
5. The preparer submitted the person finding. Its own view offered withdrawal and
   no approval control. The separate reviewer downloaded the protected file,
   examined it and recorded approval with the required independence confirmations.
   Reopening showed a currently approved finding.
6. The preparer saved a fixed employee appointment with explicit start/end,
   reference and reason, bound the released employment file to that exact revision,
   and submitted against the independently approved person finding. The reviewer
   downloaded the employment file and approved the terms. Reopening showed current
   approved terms. Both downloaded 179-byte test images match their uploaded bytes.
7. Local activation initially returned the default-off 503: the saved local API
   config omitted `WORKFORCE_EMPLOYMENT_ACTIVATION_ENABLED`. The authorized local
   fixture config now persists `true`. The same verified owner binary was restarted
   on 8399; no schema or production change was made. Eligibility then passed.
8. Activation refused an empty reason and missing examination confirmation. The
   separately assigned activator supplied both and received the activation receipt.
   Read-only proof found Staff v2/epoch2, one generation and the matching IAM basis,
   with zero login links and zero assignment proposals at that checkpoint.
9. Admin proposed a new login link and reader assignment. A separate Admin approved
   each. The login link reached linked version 2 and reader assignment active
   version 5. Neither proposal alone was treated as current access.

These are synthetic workflow observations, not certification of a real person's
identity, an actual employment decision or an activated regulatory policy.

## Reader and preparer journeys

The new account signed in normally. Reader discovery exposed exactly the approved
hospital with a Reader label and no legacy hospital grant. The reader opened the
existing canonical Pharma Staff target `SYN-SHARED-PHARMA-20261102`, read its draft
and both immutable history revisions, and reopened the same history after a full
reload. Prepare and Edit controls were absent.

Admin then proposed a separate preparer assignment; the independent Admin reviewed
it to active version 5. A fresh normal sign-in showed one Preparer hospital despite
overlapping reader/preparer assignments. The same canonical Pharma Staff target
was reused; no product-specific employee was created.

An empty posting form required a current department, UTC start and reason. The
preparer selected the active canonical Pharma department and saved a new draft
for 4 November 09:00 UTC through 30 November 17:00 UTC. Reopening populated the exact dates.
The preparer extended the proposed end to 2 December 17:00 UTC and entered a new reason.
Normal Staff-directory navigation prompted before discarding the dirty editor;
Keep editing preserved both values. Saving appended revision 2, preserving revision 1.
A full reload retained both versions and the earlier independent draft unchanged.
Every visible receipt/history remains draft-only; no supervisor, effective posting
or operating authority was established.

## Persistence and evidence

Read-only SQL verifies one unchanged Pharma target at Staff v2/epoch1 and Inactive,
one new draft head at version 2, two distinct retained commands/revisions and two
save audits carrying the exact actor Staff/link/generation and assignment metadata.
The prior draft head remains version 2. The actor's two posting assignments contain
only posting read/prepare, Staff actor use and Organization choice read; it has
zero legacy hospital grants.

Private evidence is under
`~/.helivanta/staff-management-20261027/shared-staff-foundation-20261102/`:

- `fresh-posting-source-account.json` — credentials and canonical actor references;
- `fresh-posting-source-ui-proof.json` — pre-activation checkpoint;
- `fresh-posting-source-activation-proof.json` — activated source with no implicit grants;
- `scoped-posting-positive-ui-proof.json` — target, draft, command/audit and permission proof;
- `fresh-posting-source-positive-ui-checkpoint.json` — current acceptance state;
- `fresh-source-*.png` and `scoped-posting-positive-*.png` — browser captures.

The existing private `local-staff-ui-qa.json` was updated atomically with credentials
preserved. New artifacts use mode 600. No secrets are repository artifacts.
The identity download observer timed out and detached its browser handle; owner
audits and the downloaded file proved completion. A fresh controlled tab reopened
the same pending review without repeating the download or decision. The employment
download subsequently completed without interrupting browser control.

No application code changed in this acceptance turn. Exact implementation head
`829de653` passed API/shell builds, Trivy and GitGuardian; deployment advancement
was skipped. Earlier local tests remain recorded in the tracker and were not
rerun merely to document browser observations.

## Remaining gates

The remaining account/scope/negative browser matrix, including a retained editor
across authority withdrawal, remains open. The earlier
intermittent evidence-admission failure is still recorded, not claimed fixed.
PR #1532 remains draft. Effective posting approval, transfers/supervision, contractor
and exit/rehire lifecycle enforcement, Pharma duties/operating consumers, the
standalone-employer migration and the remaining Staff areas remain parent work.
No full E2E tag, production migration or rollout is approved by this QA record.

## Follow-up: lost response, downgrade and unrelated reviewer

The same normal preparer edited the new draft from version 2, proposing an end of
3 December 17:00 UTC. A temporary loopback relay between Global API's Workforce
destination and owner API forwarded that exact synthetic PUT. Only after the
owner returned 200 did it replace the response with 503. Browser traffic still
used Global API; authentication, source checks and owner persistence were unchanged.
The relay matched the exact synthetic draft and fired once. It logged the command
path, owner status and response hash, without tokens or response payloads.

The UI displayed an unconfirmed outcome, disabled editing of the retained command,
and offered exact retry and receipt recovery. Selecting **Recover exact posting
receipt** displayed version 3 and all three immutable revisions. Read-only SQL
matched the relay's exact command to one version-3 revision. There are three
distinct commands/revisions and three attributable save audits in total: recovery
created no duplicate revision or save audit. The older separate draft remains v2;
the canonical Pharma Staff remains Inactive at v2/epoch1. Original Global API
routing was restored and the relay stopped after the test.

Admin then withdrew only this actor's preparer assignment through normal Staff
access UI, recording a reason. It reached withdrawn version 6; its history retains
versions 1–6 and the separate proposer's/reviewer's decisions. The independent
reader assignment remains active v5. After sign-out and a fresh normal recipient
login, discovery showed exactly one hospital labelled Reader. Prepare/Edit controls
were absent; draft v3 and immutable v1/v2/v3 history remained readable, including
after a full reload. Read-only proof confirms the active assignment's permissions
are only posting read and Staff actor use, with zero legacy hospital grants.
This verifies fresh-session downgrade; it does not claim a retained stale editor
was exercised across that withdrawal.

A normal login as the independent person/employment reviewer then opened Posting
preparation and received **No approved posting hospitals on this page.** Its review
roles do not create posting authority. No new assignment or membership was granted
in this follow-up.

Additional private evidence in the same directory:

- `scoped-posting-response-loss-relay-proof.json` and
  `scoped-posting-response-loss-persistence-proof.json`;
- `scoped-posting-reader-downgrade-proof.json` and
  `scoped-posting-followup-ui-checkpoint.json`;
- `scoped-posting-response-loss-{unknown,recovered}-ui.png`,
  `scoped-posting-reader-downgrade-reload-ui.png` and
  `scoped-posting-unrelated-reviewer-denial-ui.png`.

The credential JSON was updated atomically with this checkpoint, preserving its
credentials. The local response-loss proof and downgrade proof were run with
`python3 /tmp/helivanta-scoped-posting-response-loss-proof.py` and
`python3 /tmp/helivanta-scoped-posting-reader-downgrade-proof.py`. The first downgrade
proof reused an active-assignment snapshot join and omitted the withdrawn row;
the read-only query was corrected to retain the latest immutable snapshot for
both current states before its assertions passed. This was a QA-query correction,
not an application persistence fix.

No application code changed. Exact head `9b70f71b` passed API/shell builds,
GitGuardian and Trivy; deployment advancement was skipped. PR #1532 remains draft
pending the remaining browser and source/architecture acceptance gates.
