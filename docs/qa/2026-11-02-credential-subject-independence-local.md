# Credential subject independence — local verification

## Scope and status

Workforce owns canonical Staff enrollment, credentials, evidence and validity.
This correction is a prerequisite for Pharma professional acceptance. It does not
implement product acceptance, outlet duties, effective postings or operational
lifecycle enforcement. #418 and the parent reusable Staff goal remain open.
No production rollout or migration is included in local evidence.

## Reproduced failure

The regression enrolled a synthetic Staff record, uploaded and scanned synthetic
evidence, proposed a validity review through HTTP and proposed a login association
through the normal membership-checked route. With review permission and a current
hospital-manager grant, the Staff subject could approve someone else's proposal:
the expected 409 returned **200 approved** before the correction.

## Correction

Migration `0071_workforce_credential_subject_independence` retains every existing
review and association. Concurrent index migration
`0072_workforce_credential_subject_index` bounds retained-association lookup by the
exact tenant/hospital/Staff/subject key. The handler returns the existing 409 `self_review` for a
subject associated through any retained login link. The SQL boundary repeats the
check, binds the decision actor, restores its temporary login-link read purpose,
and serializes new associations and decisions on the same Staff row. No new
permission, membership or account is created. Proposer withdrawal and stable
historical decision receipts remain supported.

## Automated evidence

Focused real Postgres tests passed pending/active/revoked and withdrawn/rejected
association refusals; an unrelated permitted reviewer approved; proposer
withdrawal worked; absent review permission stayed 403. Direct SQL could not
approve as the Staff subject, and the purpose setting was restored. The
concurrency test stages an uncommitted association and observes the decision
waiting on its Staff lock before refusing after commit. Refused decisions publish
no credential-validity change. Real OpenFGA membership resolves the normal
login-link candidate.

The final matrix additionally refuses SQL actor spoofing. Both the subject
predicate and Staff lock were independently mutation-tested: their regressions
failed when each guard was removed. Original SQL was restored, then the focused
race matrix passed again (11.632s). The full owner race suite passed (640.319s),
as did Go vet/lint, build, generated credential parity and contractdocs parity.
After adding the concurrent covering-index manifest, the focused race matrix
passed again (10.825s); the local API applied both migrations and read-only
verification found the index ready and valid. Gitleaks found no leaks across all
11 changed files.
No positive Pharma duty enforcement is inferred from these tests.

Commands:

```sh
go test -race ./internal/modules/workforce -run 'TestCredential(SubjectCannot|WithdrawnAndRejected|DecisionWaits)' -count=1
go test -race -timeout=20m ./internal/modules/workforce -count=1
go vet ./internal/modules/workforce
golangci-lint run ./internal/modules/workforce/...
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check
go build -o /tmp/helivanta-credential-subject-api ./cmd/api
```

## Accepted local browser evidence

Origin: `http://helivanta.localhost:4399`. Owner API 8399, Global API 8199 and all
database/identity/queue/scanner/storage dependencies were checked as local.
The prior API runtime was captured privately before refreshing the API.

An existing admin enrolled `SYN-PHARMA-REVIEW-SUBJECT-20261102` as a synthetic
pharmacist, with a clearly synthetic registration and issuer. A pending login
association references the existing admin; it was not activated. An existing
second admin prepares its credential review. No role or hospital grant was
broadened for QA.

The evidence upload correctly remained protected when the local scanner was
stopped and then when loaded signatures exceeded the 48-hour freshness limit.
The existing cached scanner container was restarted, and its updater applied the
normal daily database delta. The configured local TCP scanner was reloaded;
recorded release provenance is engine 1.5.4, definition 28171, dated
2026-11-01T06:27:34Z. The UI then released the same file as **Ready**. No
dependency/image installation or freshness bypass was used. A separate attempt
to start an updater correctly refused because the existing updater held its log
lock; it was not bypassed.

The second admin proposed against this scanned file. The linked Staff subject
attempted both approval and rejection and saw the independence refusal. A
read-only database proof confirmed the review stayed pending, with zero validity
records. The proposer then withdrew it through the UI. The applicant submitted a
new proposal and the unrelated admin inspected its protected download (179 bytes,
SHA-256 matches the uploaded synthetic image), approved it, and opened history.
The browser download event timed out, but the actual downloaded artifact and its
matching bytes were verified; it was not downloaded again.

Both **Approved** and **Withdrawn** receipts are visibly retained. Final read-only
proof confirms one reviewed validity, both receipts and a pending-only account
association. No roles or grants changed. The hospital timezone is unset: the UI
explicitly requests configuration instead of claiming the credential is in date.
The synthetic image is a test fixture, not proof of a real council registration.

Private screenshots:
- `credential-subject-evidence-ready.png`
- `credential-subject-approval-refused.png`
- `credential-subject-rejection-refused.png`
- `credential-subject-independent-approved.png`
- `credential-subject-independent-history.png`

Private database receipts: `credential-subject-ui-persistence-proof.json` captures
the negative pending state; `credential-subject-positive-ui-persistence-proof.json`
captures the final approved/withdrawn state. The private QA credential JSON was
updated with references to these proofs, preserving all credentials.

Private evidence directory:
`~/.helivanta/staff-management-20261027/shared-staff-foundation-20261102/`.
Credentials remain in the existing private mode-600 JSON and are not reproduced
here.

## Final merged-base validation and CI repair

After merging the approved foundation into this branch, the full Workforce race
suite passed in 596.737s. A fresh 21-task web lint/format/typecheck gate passed
with zero cache hits (35.201s). The CI web gate initially refused a stale module
source pin in the operational-access ledger. Source comparison proved that only
the credential SQL embed and migration manifest changed; routes, permissions and
reviewed actor guards were byte-identical. Ten reviewed module-source pins were
refreshed with this rationale and the ledger regenerated. All 15 ledger tests
and its check passed. Counts remain 759 surfaces, 17 classified, 742 unclassified;
no runtime acceptance or lifecycle cutoff classification was added.
