# ADR-0003: Docker Compose is the local development stack

- **Amended (2026-08-16) by [ADR-0006](0006-zitadel-not-gip.md) and #838.**
  The stack no longer runs a Firebase/GIP emulator on `:9099`. Identity is
  **Zitadel v4.15.3** — its own Postgres 17, a `zitadel-login` service and a
  reverse proxy, matching the topology production runs. Everything below about
  *why* the dev stack is Compose rather than a cluster still holds; only the
  identity component changed.
- **Status:** Accepted (2026-08-12)
- **Context:** Issue #714 asks for a one-command local stack and names
  "CNPG, Redis, NATS, Keycloak, OpenFGA". Two of those names predate
  decisions already taken. Keycloak was superseded by GIP in ADR-0002.
  CNPG is a Kubernetes operator and has no meaning in a Compose stack.
  Separately, `sandboxctl` (tesserix/sandboxctl) can stand up a local kind
  cluster with Argo CD and Istio, which raised the question of whether the
  local environment should be Kubernetes-shaped instead.
- **Decision:** Compose is the inner loop. Postgres is `postgres:16-alpine`
  running with the same non-superuser `hms_app` role and forced RLS that
  production uses — the property the story actually depends on. Auth is the
  Firebase Auth (GIP) emulator on `:9099`; there are no realms to stand up.
  Redis, NATS with JetStream and OpenFGA are unchanged. Where #714 says
  Keycloak, read GIP; where it says CNPG, read Compose Postgres.
  A Kubernetes-shaped environment is issue #7's, not this one's: sandboxctl
  requires a Dockerfile and a Helm chart, HMS has neither yet, and it ships
  no OpenFGA and no GIP emulator — HMS's two most distinctive dependencies.
- **Consequences:** the edit-to-see cycle stays sub-second (`next dev` HMR
  and `go run`) rather than the build-push-sync minutes a GitOps loop costs.
  Two environments will eventually exist, and the deployment artifacts
  produced under #7 will need their own verification path. OpenFGA runs on
  the in-memory datastore, so tuples are lost on restart and rebuilt from
  Postgres by the boot reconciler — Postgres remains the system of record.
