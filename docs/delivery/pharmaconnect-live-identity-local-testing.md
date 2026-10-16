# PharmaConnect live identity prerequisite

Related stories: #1135, #1136; parent #1133. No story is closed by this foundation.

The onboarding authority's source contract is prepared in
[onboarding-service PR #11](https://github.com/tesserix/onboarding-service/pull/11).
Helivanta's `backend/pkg/organizationidentity` client consumes the minimized
versioned response. It deliberately does not use a local Supply profile as proof
of a partner's current platform identity.

## Commands actually run

From `backend/`, with Go 1.27.2:

```sh
go test -race -cover ./pkg/organizationidentity -count=1
golangci-lint run ./pkg/organizationidentity/...
```

Before implementation, the client tests failed to compile because `New`,
`ErrUnavailable` and the other required client symbols did not exist. After
implementation, the full package race suite passes with 97.9% statement coverage.
The unresponsive-authority test proves the request returns within its own
deadline; source failures and replays never produce cached eligibility. TLS
redirect fixtures confirm the machine credential does not reach the redirect
target. Privacy checks confirm additional provider fields are not retained.

Source-service local verification: full Go race/coverage suite against isolated
PostgreSQL (48.6% total; existing floor 47%), vet, build, four delivery contracts,
zero golangci-lint findings, zero reachable govulncheck vulnerabilities, and zero
gitleaks findings across its 30-commit history. Live local Zitadel verification
passed, with owned TEST organisation/project/grant fixtures cleaned up. Source
scope tests cover missing/wrong product, human callers, revocation during I/O,
source uncertainty, and private-field minimization.

Hosted source CI initially failed to resolve an unmerged shared-workflow revision;
the PR now pins merged `3ac75a0647d89812954ac01e20bfb78117b2dcec`. GitHub then
refused to start jobs because recent account payments failed or the spending
limit needs increasing. This is not a passing CI run; no merge/deployment is
claimed.

## Remaining acceptance

This package has no production caller yet. Dedicated credential delivery,
runtime configuration, bilateral consent/history, tenant-isolated persistence,
NATS/Temporal integration, licence/policy composition, full UI journeys and
post-release acceptance remain under the owning stories. There is no new UI in
this change to browser-test. No real customer data was copied or seeded, no live
access was broadened and no production migration was performed.
