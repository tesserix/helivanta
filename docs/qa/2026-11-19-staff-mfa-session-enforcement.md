# Staff MFA recovery session enforcement candidate

For #1703 / #1749. This is not a released reset or enrollment journey.

The application session signer now takes an explicit recovery generation argument.
Every production mint caller and test fixture was updated deliberately; no
variadic/default argument can silently discard a generation. The signed claim is
an optional canonical UUIDv7. Empty means unadmitted, not an exemption from recovery.
The verifier checks its shape and carries it through the private principal and
expired-cookie renewal evidence. It never becomes native signed-session proof.
Ordinary principal JSON omits both recovery generation and native session ID.

The private identity credential projection now opts into current account-wide
recovery evidence. Login, renewal and protected request admission fail closed if
that evidence is missing or invalid. Explicit `none` requires no admitted
generation; `completed` requires the exact matching signed generation. Pending,
uncertain and enrollment-required states refuse access. Existing password cutoff,
global revocation, membership, cookie and idle controls remain in place. Renewal,
activity and tenant switching preserve the generation already verified.

Existing positive fixtures now explicitly declare no recovery, or the matching
completed generation when testing preservation. Their original assertions remain.
New negative cases cover missing evidence, malformed evidence, pending/enrollment
states and stale/legacy cookies. Neither production adapter supplies a default
`none` when the owner cannot answer. Both hosted owner-schema fixture pins now
match infrastructure `7bd817e6fdfec553ee95499faf6076f155a4907d` and SHA256
`844877964546ef1c936298837123c6f528543d8ca609effb4aa7fe566ab6b0a2`.
No live migration has been applied.

## Evidence and source review

Observed red before green: missing signer/claim support, missing principal mapping,
renewal/activity dropping the generation, and renewal returning 200 for pending,
missing or stale recovery evidence. New HTTP projection tests first failed on
missing recovery support. These assertions now pass with actual signed application
cookies and actual handlers; native/owner HTTP dependencies are simulated locally.
A correctly signed `null` recovery claim was also observed being accepted, then
fixed with strict scalar decoding; absence remains distinct from malformed input.

Local commands executed:

- Full race suites: `go test -race ./pkg/session ./pkg/authn ./internal/modules/iam/loginclient -count=1`.
- Scoped IAM race tests: renewal, generation-preserving activity, credential freshness,
  password-recovery replay and password-only refusal for retained MFA recovery.
- Scoped customer-onboarding race tests: exact identity projection, recovery evidence
  and minimal assignment-candidate purpose.
- `go test ./... -run '^$'` compiled every backend test package without running
  database/native integration. It is not runtime test evidence.
- `go vet ./...`, `golangci-lint run ./...`, `go build ./...` and
  `go run ./cmd/contractdocs -check` passed.
- The operational inventory checker and its 15 tests, shellcheck of both fixture
  scripts and the three identity-owner fixture-control tests passed.
- The canonical remote fixture schema was fetched and its pinned digest checked.

Re-reviewed the changed authentication principal, renewal, activity and private
projection diffs before refreshing seven source pins across five existing review
records. Registration, authority classifications and outstanding acceptance
obligations are unchanged. Credential-only recovery opt-in does not expand the
assignment-candidate/profile/membership purposes or disclose employer source data.
The regenerated ledger remains source classification, not runtime acceptance.

New real database tests for generation-preserving tenant switch and protected
admission in two legacy tenant contexts run in hosted CI, not locally. No local
application, PostgreSQL or container was started. No live identity or factor was
changed. The new JWT claim and source checks are not proof of a deployed feature.

## Remaining release gates

Native recovered-login admission is still missing: the login boundary must verify
the exact signed native session, independent fresh primary/factor timestamps and
new enrollment against the latest generation before setting one. Current login
refuses retained recovery rather than inferring admission from password time or
completion status. Recovery generation also needs to participate in the existing
expired-cookie idle-window decision once that admission is wired, so an older
generation cannot constrain a genuinely fresh recovered login.

Workforce reviewed reset-target authority, IAM fresh administrator/ordinary-target
checks, durable command-idempotent pre-native revocation, forced enrollment UI and
the full hospital/independent-employer native-browser journey remain unfinished.
Obtain named migration/QA reset approval after those reviewable gates, then normal
merge/Kargo release and deployed browser verification. Keep #1703 open.
