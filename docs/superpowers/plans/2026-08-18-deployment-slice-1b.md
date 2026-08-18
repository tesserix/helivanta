# Deployment slice 1b — images, chart, and a real sign-in

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A clinician reaches `https://helivanta.app`, completes an OIDC sign-in
against production Zitadel, and lands in the shell — with `TRUSTED_PROXY_CIDRS`
set from a chart, so IP-keyed rate limits key on the real client rather than one
bucket per ingress.

**Architecture:** Two images built from this repo (`helivanta-api`,
`helivanta-shell`), published to `ghcr.io` and pulled through the GAR mirror.
Charts and ArgoCD Applications live in `tesserix-k8s` per ADR-0001. Istio owns
path routing; the shell never proxies a sibling zone. Promotion is Kargo via a
`deploy` branch. Design: `docs/superpowers/specs/2026-08-17-deployment-artefacts-design.md`.

**Tech Stack:** Go 1.26.6, Node 22, pnpm 10.17.1, Docker buildx, GitHub Actions,
Helm, ArgoCD, Kargo, Istio, cloudflared, Cosign, Syft, Trivy.

## Prerequisites — human-executed, and they gate different tasks

Neither is agent work. Both are one-time acts against shared production.

- **P1 — Zitadel app and login-client machine user on production.** The rebrand
  spec observed production Zitadel has no Helivanta app; the dev one is created
  by `scripts/lib/zitadel.mjs` at first boot. This mints
  `NEXT_PUBLIC_ZITADEL_CLIENT_ID`, which Next **inlines at build time**, so it
  gates **Task 3** (the shell image) — not the whole plan. Carries #45's
  unresolved question of whether a machine user on the shared instance needs
  platform sign-off; confirm before executing, not after.
- **P2 — #45 Task 4, the two boot secrets**, written to
  `kv/data/helivanta/helivanta-api/{session-signing-key,zitadel-login-client-token}`
  via the `secret-service` console. Gates **Task 5** (the API pod refuses to boot
  without them). The runbook is now correct; follow it rather than memory.

Tasks 1, 2 and 4 need neither and can start immediately.

## Global Constraints

- **Repository split:** images, Dockerfiles, CI and Go code in `helivanta`;
  every chart and ArgoCD manifest in `tesserix-k8s` (ADR-0001).
- **Cluster:** `gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`.
  No dev cluster exists. Everything applies to PRODUCTION; be additive and
  separately reversible.
- **Slice 1a is live and healthy** — do not modify it. `helivanta-postgres`
  (database `helivanta`, roles `helivanta`/`hms_app`/`openfga`),
  `helivanta-nats` (JetStream on), `helivanta-openfga` (Postgres datastore),
  seven NetworkPolicies, a ResourceQuota and LimitRange, all under an
  app-of-apps at `argocd/prod/apps/helivanta/`.
- **Service DNS the API must use, verbatim:**
  `postgres://…@helivanta-postgres-rw.helivanta.svc.cluster.local:5432/helivanta`,
  `nats://helivanta-nats.helivanta.svc.cluster.local:4222`,
  `http://helivanta-openfga.helivanta.svc.cluster.local:8080`.
- **The application DB role is `hms_app`** and the RLS predicate is
  `hms_tenant_visible()`. Neither renames — migrations are append-only and every
  policy names them.
- **OpenBao readable prefix is only `kv/data/helivanta/helivanta-api/*`.** The
  chart generates app policies as `kv/data/<ns>/<app-name>/*`; the app segment
  cannot be chosen. Never add a hand-written broad policy.
- **`remoteRef` needs `property:` as well as `key:`.** Without it ESO returns
  the whole KV payload — non-empty, `SecretSynced`, and unusable. The values are
  `session_signing_key` and `zitadel_login_client_token` — **underscores**,
  where the path segment uses hyphens. Not `password`: that is
  `helivanta-postgres`'s field, and reusing it here fails at deploy time, not
  write time.
- **No secret value passes through an agent session.**
- **Every control is proven by mutation.** Each task names the mutation that
  must make it fail.
- **Never trust ArgoCD's `Synced`/`Healthy` alone** — compare
  `.status.sync.revision` against `origin/main`. A cached manifest error read
  `Synced` for four apps while a fifth had never been created.
- **Verify against a live working object, not documentation.** Every defect in
  slice 1a was found this way; none came from reading. `homechef-api` and
  `dwellm8-api` are the live references for a Go service chart.

---

### Task 1: `TRUSTED_PROXY_CIDRS` refuses boot outside dev

Pure Go, no infrastructure. Start here — it is the control this whole critical
path exists to enable, and it is independently testable.

**Files:**
- Modify: `backend/internal/config/config.go`
- Create: `backend/internal/config/trustedproxy.go`
- Create: `backend/internal/config/trustedproxy_test.go`
- Modify: `backend/cmd/api/main.go` (call the guard next to `RequireDistinctHostedLoginOrigin`, `main.go:111`)
- Modify: `.env.example`

**Interfaces:**
- Produces: `func (c Config) RequireTrustedProxyCIDRs() error`, called from
  `main.go` and aborting boot on error. Task 5 sets the value from the chart.

**The behaviour, exactly:**

| `TRUSTED_PROXY_CIDRS` | `HELIVANTA_ENV=dev` | Otherwise |
|---|---|---|
| unset or empty | empty list, raw TCP peer | **refuse boot** |
| `none` | raw TCP peer | raw TCP peer |
| valid CIDR list | trusted | trusted |
| every entry malformed | empty list | **refuse boot**, message distinct from unset |

- [ ] **Step 1: Write the failing tests**

Six cases, each asserting the *specific* failure, not merely that an error
occurred:

```go
func TestRequireTrustedProxyCIDRs(t *testing.T) {
	for _, tc := range []struct {
		name, env, value string
		wantErr          bool
		wantMsgContains  string
	}{
		{"unset outside dev refuses", "production", "", true, "TRUSTED_PROXY_CIDRS is not set"},
		{"empty outside dev refuses", "production", "  ", true, "TRUSTED_PROXY_CIDRS is not set"},
		{"none outside dev is allowed", "production", "none", false, ""},
		{"valid list outside dev is allowed", "production", "10.20.0.0/16", false, ""},
		{"all-malformed outside dev refuses with a DISTINCT message", "production", "not-a-cidr, also-bad", true, "no valid CIDR"},
		{"unset in dev is allowed", "dev", "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HELIVANTA_ENV", tc.env)
			t.Setenv("TRUSTED_PROXY_CIDRS", tc.value)
			err := config.Load().RequireTrustedProxyCIDRs()
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantMsgContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
```

The all-malformed case must assert a message **different** from the unset case.
If both say "is not set", an operator who mistyped their only CIDR is sent
looking for a missing variable.

- [ ] **Step 2: Run them and watch them fail**

`cd backend && go test ./internal/config/ -run TestRequireTrustedProxyCIDRs -v`
Expected: FAIL, `RequireTrustedProxyCIDRs` undefined.

- [ ] **Step 3: Implement the guard**

It must read `os.Getenv("TRUSTED_PROXY_CIDRS")` **itself**. `Load()` has already
discarded the raw string via `getenvCIDRList`, so the parsed slice cannot
distinguish "unset" from "the only entry was a typo".

Treat `none` case-insensitively after trimming, and make it the *only* accepted
sentinel — not `off`, `false` or an empty-looking value.

- [ ] **Step 4: Run them and watch them pass**

- [ ] **Step 5: Prove the guard is reachable, not just correct**

A passing unit test does not prove `main.go` calls it. Temporarily delete the
call from `main.go`, run the API with `HELIVANTA_ENV=production` and no
`TRUSTED_PROXY_CIDRS`, and observe it **boot** — then restore the call and
observe it **refuse**. Record both. This is the mutation that matters: the
existing `TrustedProxyCIDRs` tests all passed while nothing enforced anything.

- [ ] **Step 6: Do NOT change the four existing tests**

`TestTrustedProxyCIDRsAllMalformedYieldsEmptyNotError` and its three siblings
assert `Load()`'s parsing, which is unchanged. But that test's **comment** says
the all-garbage case lands on the same answer as unset "rather than a boot
failure" — which becomes false the moment this guard exists. Correct the comment
in this change; `CLAUDE.md` requires superseded claims be fixed in the same PR.

- [ ] **Step 7: Update `.env.example`**

Document `none` alongside the production value, and say the variable is
mandatory outside dev.

- [ ] **Step 8: Commit**

---

### Task 2: The backend image

**Files:**
- Create: `Dockerfile.api`
- Create: `.dockerignore`
- Modify: `Makefile` (add `image-api`)

**Interfaces:**
- Produces: an image running `/app/api` as a non-root user, honouring SIGTERM,
  stamping version metadata. Task 4 builds it; Task 5 runs it.

- [ ] **Step 1: Write the multi-stage Dockerfile**

Builder on `golang:1.26.6` pinned by digest; final stage
`gcr.io/distroless/static-debian12:nonroot`. `CGO_ENABLED=0`, and
`-ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT"`.

**Corrected 2026-08-18.** This step originally said to ship only `cmd/api` and
to exclude `cmd/migrate`. That was wrong and left a hole: **Task 5's Helm
pre-upgrade hook needs a migrate binary, and no task in this plan produced
one.** It was found only when the chart author went looking for an image to run
and discovered none existed.

Ship **both** `cmd/api` and `cmd/migrate` into the same image
(helivanta#876). One image makes version skew structurally impossible — the
migrations and the API that expects them are the same artifact, so an operator
cannot run migrations from a different build than the app. For a database
holding patient records that is a correctness property, not a convenience.

`cmd/session-key` stays excluded: it mints signing keys and has no business in
a container serving production traffic.

The image grows from 44.5MB to 80.1MB as a result, which is larger than it
sounds worth stating — the migrate binary statically embeds every module's
migration set.

Distroless `nonroot` runs as UID 65532 and has no shell — which is the point,
and also means you cannot `exec` into it to debug. Say so in a comment.

- [ ] **Step 2: Build it and prove it runs as non-root**

```bash
docker build -f Dockerfile.api -t helivanta-api:local .
docker run --rm --entrypoint="" helivanta-api:local id 2>&1 || echo "no shell — expected for distroless"
docker inspect helivanta-api:local --format '{{.Config.User}}'
```
Expected: `65532:65532`. A distroless image with `User` empty is running as root
and the base tag is wrong.

- [ ] **Step 3: Prove SIGTERM is honoured — the mutation**

`main.go:136` installs the handler and `:419` calls `Shutdown`, but nothing has
confirmed the *container* delivers the signal to PID 1 rather than to a shell
wrapper. Start the container, send SIGTERM, and assert it exits **0** promptly:

```bash
docker run -d --name sigterm-probe helivanta-api:local
docker stop -t 30 sigterm-probe
docker inspect sigterm-probe --format '{{.State.ExitCode}} {{.State.OOMKilled}}'
```
Expected exit code `0`. An exit code of `137` means it was SIGKILLed after the
grace period — the handler never ran, and the migration batch worker (#788) and
outbox dispatcher would be cut mid-work.

- [ ] **Step 4: Confirm the image has no shell and no package manager**

```bash
docker run --rm --entrypoint=/bin/sh helivanta-api:local -c 'echo reachable' && echo "FAIL: shell present" || echo "OK: no shell"
```

- [ ] **Step 5: Commit**

---

### Task 3: The shell image

**Blocked on P1** — `NEXT_PUBLIC_ZITADEL_CLIENT_ID` is inlined at build time and
`apps/shell/lib/env.ts` refuses to build without it.

**Files:**
- Create: `Dockerfile.shell`
- Modify: `.dockerignore`, `Makefile` (add `image-shell`)

**Interfaces:**
- Produces: an image serving the shell on 4301 from Next standalone output.

- [ ] **Step 1: Write the Dockerfile**

pnpm 10.17.1 via corepack, Node 22 base. `output: "standalone"` is already set
in `apps/shell/next.config.ts`, so the runtime stage copies
`.next/standalone`, `.next/static` and `public` only. Run as a non-root user.

Build args, all required: `NEXT_PUBLIC_ZITADEL_ISSUER_URL`,
`NEXT_PUBLIC_ZITADEL_CLIENT_ID`, plus `API_URL`, `MEDICORE_URL`, `PHARMACY_URL`,
`LAB_URL` set to in-cluster Service DNS.

- [ ] **Step 2: Prove the values were actually baked — the mutation**

Building with a value and *rendering* it are different. Build with a sentinel
client ID and grep the built bundle for it:

```bash
docker build -f Dockerfile.shell --build-arg NEXT_PUBLIC_ZITADEL_CLIENT_ID=SENTINEL-123 ... -t shell:probe .
docker run --rm --entrypoint="" shell:probe sh -c 'grep -rl SENTINEL-123 .next/static | head -3'
```
Expected: at least one match. No match means the build arg never reached the
bundle and the deployed app will fail OIDC at runtime with a green build — the
exact failure D6 of the rebrand spec warns about.

- [ ] **Step 3: Prove the zone URLs are frozen, and are not localhost**

```bash
docker run --rm --entrypoint="" shell:probe cat .next/routes-manifest.json | grep -o 'http://[^"]*' | sort -u
```
Expected: in-cluster Service DNS. Any `localhost` means the build args did not
apply, and in production those rewrites become unreachable-but-wrong config —
spec D3's recorded trap.

- [ ] **Step 4: Confirm it serves**

Run it, `curl -I localhost:4301`, expect 200 or a redirect to `/login`.

- [ ] **Step 5: Commit**

---

### Task 4: CI — build, scan, sign, publish

**Files:**
- Create: `.github/workflows/images.yml`
- Modify: `.github/workflows/ci.yml` only if needed (do not duplicate its jobs)

**Interfaces:**
- Produces: `ghcr.io/tesserix/helivanta/{helivanta-api,helivanta-shell}` tagged
  `main-<sha7>`, with an SBOM and a Cosign signature. Task 8 promotes them.

- [ ] **Step 1: Model it on the fleet's mature workflow**

Read `~/personal/tesserix-new/Home-Chef-App/.github/workflows/homechef-api-build.yml`
in full and follow it: buildx, registry cache, `provenance: false`, and a Trivy
gate that `load`s the image on pull requests and fails on a fixable
CRITICAL/HIGH, uploading SARIF only on pushes.

**Deliberately NOT adopted: path filters.** Build **both** images on **every**
`main` commit. HomeChef path-filters and therefore needs `ensure-image-tags` to
carry unchanged images forward; that workflow's own header documents three ways
it has wedged production. Building both deletes the failure class. Affordable
here — ADR-0001 says one PR spans frontend and backend.

**No `:latest`.** `charts/apps/console/values.yaml` pins it and is the
anti-pattern HomeChef's comment names.

- [ ] **Step 2: Add SBOM and signing**

Syft SBOM and Cosign keyless (GitHub OIDC) per image, attached by digest.

**Comment at the signing step that NOTHING verifies this yet** — the production
cluster runs three TIG governance policies and no `verifyImages` policy, and
`grep -rli cosign` across `tesserix-k8s` returns nothing. Helivanta is the
fleet's first signed image. The justification is asymmetry, not rigour: a
signature binds to a digest, so an image promoted unsigned must be *rebuilt* to
be signed, and a rebuilt digest is a different artifact than the one tested.

- [x] **Step 3: Prove the Trivy gate fails — the mutation**

Open a throwaway PR pinning a base image with a known fixable CRITICAL and
observe the check **fail**. A gate nobody has watched reject anything is not
known to be a gate.

**Satisfied without the mutation, by the real thing.** The first run after the
CI billing restore failed the gate on eight HIGH Go stdlib CVEs
(CVE-2026-56862, -56860, -56859, -56858, -56853, -46600, -39821, -33818), all
fixed in 1.26.6 — which is why the tech stack above says 1.26.6 and not the
1.26.5 this plan was written against. Rebuilding on the bumped toolchain
returned the scan to zero findings across debian, `app/api` and `app/migrate`.
A throwaway PR would now prove strictly less than what was observed in
production CI, so it is not worth opening.

- [ ] **Step 4: Prove the signature exists**

`cosign verify --certificate-identity-regexp ... ghcr.io/tesserix/helivanta/helivanta-api@<digest>`
Expected: verification succeeds. Record that this proves the signature is
*present and valid*, not that anything enforces it.

- [ ] **Step 5: Commit**

---

### Task 5: The API chart and Application

**Blocked on P2** — the pod refuses to boot without both secrets.
Work in `tesserix-k8s`.

**Files:**
- Create: `charts/apps/helivanta-api/{Chart.yaml,values.yaml}`
- Create: `charts/apps/helivanta-api/templates/{deployment,service,externalsecret,serviceaccount,pdb}.yaml`
- Create: `argocd/prod/apps/helivanta/helivanta-api.yaml`
- Modify: `argocd/prod/apps/helivanta/kustomization.yaml`

**Interfaces:**
- Consumes: slice 1a's Services and the two OpenBao boot secrets.
- Produces: Service `helivanta-api.helivanta.svc.cluster.local:8080`. Task 6
  routes `/api/*` to it.

- [ ] **Step 1: Read the live references**

`charts/apps/homechef-api/` and `charts/apps/dwellm8-api/`. Follow their shape.

- [ ] **Step 2: Wire config and secrets**

Env from the Service DNS in Global Constraints. Two ExternalSecrets reading
`kv/data/helivanta/helivanta-api/{session-signing-key,zitadel-login-client-token}`
via `SecretStore/openbao-helivanta-api`, **each with `property:` set** —
`session_signing_key` and `zitadel_login_client_token` respectively. Already
authored in `tesserix-k8s:charts/apps/helivanta-api/templates/externalsecret.yaml`
(PR #411, deliberately not activated); this step activates it rather than
writing it. The secrets written in P2 must carry those exact field names.

`HELIVANTA_WEB_ORIGIN=https://helivanta.app`.
**`TRUSTED_PROXY_CIDRS=10.20.0.0/16`** — the pod CIDR the Istio ingress gateway
runs in. Task 9 confirms or corrects it against a live request; until then it is
an inference.

- [ ] **Step 3: Probes and shutdown**

`livenessProbe` → `/healthz`, `readinessProbe` → `/readyz`. `/readyz` already
iterates real checks and 503s naming the failure (`internal/httpserver/server.go:75-84`).

`terminationGracePeriodSeconds` **above** the server's shutdown timeout. A grace
period shorter than the drain makes Task 2's SIGTERM handling decorative.

- [ ] **Step 4: Migrations as a pre-upgrade hook**

`backend/cmd/migrate` runs as a Helm `pre-upgrade`/`pre-install` hook Job, not an
initContainer: a failed migration then fails the release and the previous
ReplicaSet keeps serving, rather than crash-looping a new pod with the error
visible only in container logs.

- [ ] **Step 5: Prove readiness discriminates — the mutation**

Scale `helivanta-nats` to zero, then confirm `/readyz` returns **503 naming
`nats`** and the pod leaves the Service endpoints. Restore. A readiness probe
that returns 200 while a dependency is down is worse than none — it routes
traffic into a broken pod.

- [ ] **Step 6: Prove the boot guard fires in the real deployment**

Remove `TRUSTED_PROXY_CIDRS` from the chart, deploy, and watch the pod refuse to
start with the message naming the variable. Then set `none` and watch it start.
Then set the real CIDR. All three, observed — the middle one is what
distinguishes a working sentinel from a guard that refuses everything.

- [ ] **Step 7: Commit**

---

### Task 6: The shell chart, and Istio routing

**Files:**
- Create: `charts/apps/helivanta-shell/` (chart + templates)
- Create: `charts/apps/helivanta-istio/templates/{gateway,virtualservice}.yaml`
- Create: `argocd/prod/apps/helivanta/{helivanta-shell,helivanta-istio}.yaml`
- Modify: `argocd/prod/apps/helivanta/kustomization.yaml`

- [ ] **Step 1: Route by path at the ingress, not in the shell**

`/api/*` → `helivanta-api:8080` **stripping the `/api` prefix** (the backend
serves `/v1/*`); everything else → `helivanta-shell:4301`.

The shell must never proxy a sibling zone in production: it would add a Next.js
hop to every zone request and make a shell rollout take pharmacy and lab down
with it (spec D3).

- [ ] **Step 2: Prove the prefix strip — the mutation**

A route that forwards `/api/v1/...` unstripped reaches the backend as
`/api/v1/...` and 404s. After deploying, `curl https://helivanta.app/api/v1/...`
and confirm a real API response, then confirm the backend log shows the path as
`/v1/...`. Rendering the VirtualService proves nothing about what Envoy does.

- [ ] **Step 3: Commit**

---

### Task 7: `helivanta.app` reaches the cluster

**Files:**
- Modify: `k8s/cluster/cloudflared/configmap.yaml` (in `tesserix-k8s`)

- [ ] **Step 1: Add the hostnames**

`helivanta.app` and `*.helivanta.app` → `istio-ingressgateway`, added **above**
the `http_status:404` fallback, alongside the existing `tesserix.app` and
`mark8ly.com` entries. Purely additive; edit no existing rule.

- [ ] **Step 2: Add the DNS record** pointing at the tunnel. The zone already
exists in the same Cloudflare account — same nameservers as `tesserix.app`.

- [ ] **Step 3: Prove you broke nothing — the mutation that matters here**

This configmap is **fleet-wide**. After the change, confirm `tesserix.app` and
`mark8ly.com` **still resolve and still serve**, not merely that
`helivanta.app` does. A regression here affects other products.

- [ ] **Step 4: Commit**

---

### Task 8: Kargo promotion

**Files:**
- Create: Kargo `Warehouse` + `Stage` for helivanta (follow `kargo-manifests`)
- Modify: `argocd/prod/apps/helivanta-app-of-apps.yaml` if needed

- [ ] **Step 1: Promote from the git commit graph, not tag discovery**

HomeChef records why: Artifact Registry paginates tag lists at 100, so
`NewestBuild` promoted a stale page and **flapped production backwards**. Use
the `deploy`-branch model.

- [ ] **Step 2: Confirm `ignoreDifferences` is already in place**

The app-of-apps carries it on `/spec/source/helm/parameters` (added in slice
1a). Without it, Kargo's writes and git fight, and a stale `image.tag` becomes
unremovable — dwellm8's recorded failure.

- [ ] **Step 3: Prove a promotion moves the running digest**

Merge a trivial change, watch the tag advance, and confirm the **running pod's
image digest** changed. A `Synced` Application is not evidence.

- [ ] **Step 4: Commit**

---

### Task 9: A real sign-in, and the XFF confirmation

The point of the entire critical path.

- [ ] **Step 1: Sign in, in a browser**

Against the deployed stack at `https://helivanta.app`. A wrong Zitadel client ID
passes every build and fails only here (rebrand spec D6). Not a curl — a real
OIDC round trip.

- [ ] **Step 2: Read the actual `X-Forwarded-For` at an app pod**

The verified request path is `browser → Cloudflare edge → cloudflared POD
(10.20.x) → istio-ingressgateway → app pod` — **two in-cluster hops**, because
cloudflared runs inside the cluster. Log or echo the header as received.

- [ ] **Step 3: Confirm or CORRECT `10.20.0.0/16`**

If the observed header disagrees with the inference, change the chart. The
inference is not the deliverable; the observation is.

- [ ] **Step 4: Prove the limiter is no longer bypassable — the mutation**

Send a burst with a **forged `X-Forwarded-For`** and confirm it does **not** get
a fresh token bucket. Then send a burst without one and confirm it does hit the
limit. Both halves: the first alone could pass because the limiter is broken
entirely.

This is what #870 fixed in code and what has never been confirmed against a
running pod.

- [ ] **Step 5: Record the result in the spec and close #824**

---

## Definition of done

- A browser sign-in completes at `https://helivanta.app`.
- The API pod is Ready, `/readyz` proven to 503 when a dependency is down.
- `TRUSTED_PROXY_CIDRS` proven to refuse boot when unset, and `none` proven to
  start.
- The real `X-Forwarded-For` observed at a pod, and `10.20.0.0/16` confirmed or
  corrected.
- A forged `X-Forwarded-For` proven **not** to buy a fresh rate-limit budget.
- `tesserix.app` and `mark8ly.com` proven unaffected by the cloudflared change.
- A Kargo promotion proven to move the running digest.
- Trivy gate proven to fail a deliberately vulnerable base image. **Done** — it
  rejected eight real HIGH stdlib CVEs before any image reached a cluster, and
  passed again once the Go toolchain moved to 1.26.6.

## Out of scope

- The three product zones (`medicore`, `pharmacy`, `lab`) — slice 2.
- Kyverno image-signature admission — #7.
- Preview environments — #715, blocked by D4's build-time `NEXT_PUBLIC_*`.
- **Postgres HA and backups.** `instances: 1` with `backup.enabled: false` is
  acceptable only while the database is empty. It must be raised before
  Helivanta holds patient data — tracked, and not closed by this slice.
