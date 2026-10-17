# PharmaConnect live identity prerequisite

Related stories: #1135, #1136; parent #1133. No broader B2B story is closed by
this prerequisite.

## Released authority

[Onboarding-service PR #11](https://github.com/tesserix/onboarding-service/pull/11)
merged as `6346f0fb58316f373845f7ac7eb444df08133416`.

- PR CI `38927376853`, attempt 2: all required jobs passed after the user approved
  making onboarding-service public. Helivanta was also restored to public.
- Main CI `38991308321`: Go/PostgreSQL, web, contracts, secret scan, both container
  gates, image publication/signing and deploy-branch advancement passed.
- Kargo promotion `prod.01m53r6qpcsn0c35g8ayzjb4ry.2eea9c5` succeeded.
- API and console each rolled out two updated/ready replicas on the exact
  release. Argo reported Synced/Healthy; recent API logs had no WARN/ERROR events.
- The public identity route rejects unauthenticated requests with generic 401.
  Positive production machine acceptance remains dependent on canonical product
  registration and a dedicated credential; it is not claimed here.

Upstream local validation passed: full Go race/coverage against isolated
PostgreSQL (48.6%, floor 47%), vet/build/lint, four delivery contracts, zero
reachable govulncheck findings and zero gitleaks findings across its 30-commit
history. Owned live local Zitadel TEST fixtures exercised metadata changes and
organisation/project/grant suspension/revocation and were cleaned up. The console
also passed format, lint, 21 tests, type checks, production audit and build.

## Consumer implementation

The client performs a fresh exact product/tenant read with lossless source
revisions, bounded time/body size, no browser cookies and no redirects. Its
dedicated machine-token adapter signs one-minute assertions, requests only the
configured onboarding audience, retains only expiring access credentials and
redacts diagnostic formatting. It never borrows the login credential.

The private owned-organisation route derives tenant identity from the verified
session, checks object access before source I/O and rechecks access/profile
version afterwards. Successful reads append an audit event. It returns no
identity proof for an inactive, malformed, inaccessible or changed source and
never reports trading approval. No database transaction spans provider I/O.

The API-backed panel appears only inside an owned organisation's private
profile. It checks on request, removes old confirmation during refresh/error or
scope changes, and supports keyboard retry. Wrong tenant, party, profile revision,
inactive state or a purported trading approval cannot be displayed as a valid
identity observation. Go/browser DTOs are generated from one consumer contract.

## Observed verification

With Go 1.27.2, initial protocol/configuration tests failed to compile and the
real PostgreSQL owned-organisation route test reproduced 404 before
implementation. The initial UI test failed because the component did not exist.
The corresponding implemented paths pass. A credential-formatting regression
test also reproduced disclosure of a synthetic cached token before redaction.

The local Docker engine was healthy inside its VM but the host socket was
unavailable. A task-only SSH-forwarded Unix socket restored access without a VM
restart or changing the active context. Testcontainers used the existing guest
`/var/run/docker.sock`; Ryuk remained enabled. All test data is synthetic.

Commands run from `backend/`, with Go 1.27.2 and the task Docker endpoint/socket
override:

```sh
go test -race ./pkg/organizationidentity -count=1
go test -race ./internal/modules/supply -run TestOwnedOrganisationIdentity -count=1 -timeout=3m
go test -race ./cmd/api -run TestOrganisationIdentity -count=1
go test -race -p 1 -coverprofile=/tmp/helivanta-b2b-identity-local-coverage.out ./...
GOFLAGS=-p=1 bash scripts/coverage-gate.sh
golangci-lint run ./...
go vet ./...
go build ./...
govulncheck ./...
```

The first full race run found one missing singleton-route declaration in the
architecture pagination inventory. The route returns one bounded observation,
so an exact entry with its reason was added; the pagination test passed. The final
standard `GOFLAGS=-p=1 bash scripts/coverage-gate.sh` completed successfully after
that correction and restoration of a persistent task-only Docker forward.
Every gated package exceeded 70%; Supply measured 78.2% and the identity package
91.3%. The earlier run with the disconnected socket failed and is not counted
as a pass.

Affected frontend suites passed 538 tests (212 API, 326 shell), including eight
new panel cases. Type checks, generated-contract checks and ESLint passed.
The production build uses CI's explicit public issuer/client fixture values;
these build values do not establish a working login configuration. The initial
build without those required values failed as expected. Native Chrome control completed a normal local OIDC sign-in through an
already-registered test client on port 4398, with this worktree's API on 8093.
The separate 4302 client attempt was correctly rejected for its unregistered
callback; no shared identity configuration was changed. A synthetic owned
pharmacy was saved, the identity panel appeared, and both Check and Try again
rendered the real API's unavailable state while configuration was absent. The
initial form validation also focused a missing required field. Screenshot:
`/tmp/helivanta-b2b-identity-local-unavailable.png`. No positive identity was
fabricated; active identity browser acceptance remains pending activation.
Generated files were formatted and the working tree secret scan found no leaks.

## Remaining activation and B2B work

Read-only production discovery confirmed that onboarding-api points to
`onboarding_db` and that its canonical product registry has zero rows. No secret
values, customer data or environment dump were printed or committed. Registration
must occur through the authorised owner service/operator, not ad hoc SQL.
[Onboarding-service #12](https://github.com/tesserix/onboarding-service/issues/12)
is assigned to Sam123ben and is a sub-issue of #1135; it covers stable existing
tenant enrolment, actual product registration, dedicated credential delivery and
positive scoped-machine production acceptance.

The consumer changes are not activated in production. Canonical registration,
OpenBao/ESO/runtime delivery, real local/production browser journeys, bilateral
consent/history, licence/country composition, destination-owned NATS projections,
Temporal coordination and the remaining business UI remain with their owning
stories. No new regulated trade, clinical sharing, production data migration or
blanket compliance certification is claimed.

## Continuation on 17 October

The source prerequisite now includes an existing-tenant enrolment API and console
form being developed under onboarding-service #12. Local tests preserve the
existing UUID, reject revoked/conflicting bindings and prove concurrent
idempotency and atomic audit rollback. A live local source check also found a
stale grant-search result after deletion; defect onboarding-service #13 now has
an exact-grant lookup regression and local fix. Those source changes are not yet
released. Existing bootstrap credentials received HTTP 403 during read-only
production project discovery; no new IAM, registry or credential changes followed.
