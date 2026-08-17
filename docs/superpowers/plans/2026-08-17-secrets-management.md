# Secrets Management (#45 — boot secrets) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give HMS's two boot secrets a production home in OpenBao, a way to be provisioned, and a gate that stops secrets entering source control — without adding any secret-store code to HMS.

**Architecture:** ESO reads OpenBao as the `hms-api` ServiceAccount and projects a Kubernetes Secret; the deployment maps it to env; `config.Load` reads env exactly as today. HMS gains a key generator, a runbook, and a gitleaks gate. Nothing in HMS learns what a vault is.

**Tech Stack:** Go 1.26, GitHub Actions, gitleaks, OpenBao (KV v2), External Secrets Operator, Zitadel v4.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-08-17-secrets-management-design.md`. Every decision reference below (D1–D7) is to that file.
- **Two secrets, not three** (D1): `SESSION_SIGNING_KEY` and `ZITADEL_LOGIN_CLIENT_TOKEN`. `HMS_WEB_ORIGIN` is config and must NOT be put in OpenBao.
- **No secret-store code in HMS** (D4): no vendor SDK, no `secrets` package, no resolution layer. Adding one fails this plan's intent even if tests pass.
- **OpenBao paths** (D3), exactly: `kv/data/hms/api/session-signing-key`, `kv/data/hms/api/zitadel-login-client-token`. Policy `read-hms`. Role `app-hms_hms-api`. Namespace `hms`. ServiceAccount `hms-api`. No `{env}` segment.
- **This slice adds no boot enforcement.** `config.SessionSigningKeySeed` and `config.RequireZitadelLoginClientToken` already refuse correctly and already have tests. Do not add duplicate coverage.
- **Prove every assertion can fail** before trusting it, and check the mutation moves a value the assertion reads. A denial produced by a typo'd path is not evidence of a working policy.
- **Commit messages:** single line, conventional commits, no signature, no `Co-Authored-By`.
- **Before done:** `make lint-go` clean, `cd backend && ./scripts/coverage-gate.sh` green, `go test -race ./...` green.

---

## File Structure

| File | Responsibility |
|---|---|
| `.gitleaks.toml` (create) | Scanner config and the value-scoped allowlist for deliberate/benign findings |
| `.github/workflows/ci.yml` (modify) | Add the `secrets` job running gitleaks |
| `backend/internal/config/generate.go` (create) | `GenerateSessionSigningKey()` — mints a seed in the exact format `SessionSigningKeySeed` accepts |
| `backend/internal/config/generate_test.go` (create) | Asserts the generator's output is accepted by the real validator |
| `backend/cmd/session-key/main.go` (create) | Prints a generated key; what the make target runs |
| `Makefile` (modify) | `secret-session-key` target |
| `docs/runbooks/secrets.md` (create) | Secret inventory, OpenBao paths, provisioning runbook, D2's residual risk |

Task 4 touches `tesserix-k8s` (separate repo) and the production cluster; it creates no files here.

---

### Task 1: gitleaks gate

**Files:**
- Create: `.gitleaks.toml`
- Modify: `.github/workflows/ci.yml` (add a `secrets` job alongside `go`, `web`, `scripts`)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: nothing later tasks depend on. Independent.

**Context the implementer needs.** A baseline scan was already run against this repo (2026-08-17, `zricethezav/gitleaks:latest`, 309 commits): **9 findings, all benign**, in two classes:

1. The deliberately committed dev Ed25519 key `X5yoi73f6FRR8XH2ZfRBjanOZLm/bkae0QV7wGJRuf8=` — at `backend/internal/config/signingkey.go:22` and `Makefile:138` (and at historical line positions in older commits). It is committed on purpose; `SessionSigningKeySeed` refuses to honour it outside `HMS_ENV=dev`.
2. PHI-redaction test fixtures flagged on entropy — repeating UUIDs like `11111111-1111-1111-1111-111111111111` and `ref_9876543210_x`, in `backend/pkg/logging/redact_test.go` and quoted in `docs/superpowers/plans/2026-08-13-structured-logging-phi-redaction.md`.

**Allowlist by secret VALUE, never by file path.** A path allowlist on `Makefile` or `signingkey.go` would hide a real secret added to those files later. Scoping the exception to the known literal keeps every other secret in those same files caught. This is D5's whole point: an exception that is broad turns the gate into paperwork.

- [ ] **Step 1: Write the config with the value-scoped allowlist**

Create `.gitleaks.toml`:

```toml
# Secret scanning gate for HMS (#45, spec D5).
#
# Exceptions below are scoped to specific secret VALUES, never to file
# paths. A path allowlist on Makefile or signingkey.go would exempt a
# REAL secret added to those files later; a value allowlist exempts only
# the exact known-benign string and keeps everything else caught.
#
# Baseline: 2026-08-17, 309 commits, 9 findings, all benign.

[extend]
useDefault = true

[[rules]]
id = "generic-api-key"
[rules.allowlist]
regexes = [
  # The dev Ed25519 seed, committed on purpose as
  # config.DevSessionSigningKey and mirrored in the Makefile's dev-api
  # target. It is not a leak: SessionSigningKeySeed REFUSES to honour it
  # outside HMS_ENV=dev, so a production process cannot be tricked into
  # signing with it. Removing it would not improve security and would
  # break every developer's stack.
  '''X5yoi73f6FRR8XH2ZfRBjanOZLm/bkae0QV7wGJRuf8=''',

  # PHI-redaction test fixtures. These are deliberately structured
  # strings that pkg/logging's redactor must leave ALONE (a UUID contains
  # a twelve-digit run, so a careless Aadhaar pattern would mask every
  # tenant_id). They carry no credential; gitleaks flags them on entropy.
  '''1{8}-1{4}-1{4}-1{4}-1{12}''',
  '''ref_9876543210_x''',
]
```

- [ ] **Step 2: Run the scanner and verify the known findings are now clean**

Run:
```bash
docker run --rm -v "$PWD:/repo" zricethezav/gitleaks:latest detect \
  --source=/repo --config=/repo/.gitleaks.toml --no-banner --redact
```
Expected: `no leaks found`. If any of the 9 baseline findings still fire, the allowlist regex does not match the value the scanner actually extracted — fix the regex, do not widen it to a path.

- [ ] **Step 3: Prove the gate can fail**

This is the step that makes the gate real. Create a throwaway file with a credential the default ruleset detects:

```bash
printf 'aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"\n' > /tmp/leak-probe.txt
cp /tmp/leak-probe.txt ./leak-probe.txt
docker run --rm -v "$PWD:/repo" zricethezav/gitleaks:latest detect \
  --source=/repo --config=/repo/.gitleaks.toml --no-banner --redact --no-git
```
Expected: **`leaks found: 1`** naming `leak-probe.txt`.

If it reports zero, the gate is inert and the allowlist is too broad — stop and fix it before continuing. A gate nobody has watched reject anything is not known to be a gate.

Then remove the probe:
```bash
rm ./leak-probe.txt
```

- [ ] **Step 4: Add the CI job**

In `.github/workflows/ci.yml`, add as a sibling of the existing `go`, `web` and `scripts` jobs:

```yaml
  secrets:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          # Full history: the gate scans every commit, so a secret added
          # and then removed in a later commit is still caught. With the
          # default shallow clone it would not be.
          fetch-depth: 0
      - name: gitleaks
        uses: gitleaks/gitleaks-action@v2
        env:
          GITLEAKS_CONFIG: .gitleaks.toml
```

- [ ] **Step 5: Verify the workflow file is valid YAML**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml')); print('ok')"`
Expected: `ok`

Note: GitHub Actions is billing-blocked on this repo, so the job will not actually run in CI. Local verification in Steps 2–3 is the real evidence; do not treat a red or absent check as information.

- [ ] **Step 6: Commit**

```bash
git add .gitleaks.toml .github/workflows/ci.yml
git commit -m "ci: add gitleaks secret scanning with value-scoped allowlist (#45)"
```

---

### Task 2: Session signing key generator

**Files:**
- Create: `backend/internal/config/generate.go`
- Create: `backend/internal/config/generate_test.go`
- Create: `backend/cmd/session-key/main.go`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `config.Config.SessionSigningKeySeed() ([]byte, error)` — the existing validator in `backend/internal/config/signingkey.go`.
- Produces: `config.GenerateSessionSigningKey() (string, error)` — returns a base64-standard-encoded 32-byte Ed25519 seed.

**Why a generator rather than a runbook line** (D6): an operator told to "run openssl" produces a PEM block or a 31-byte value, and that failure surfaces as a boot refusal at deploy time against the shared production instance.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/config/generate_test.go`:

```go
package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	// NOTE the module path: it is hms/internal/config, NOT
	// hms/backend/internal/config — the go.mod lives in backend/ and the
	// module is named without that segment. Match signingkey_test.go.
	"github.com/tesserix/hms/internal/config"
)

// The generator's output must be accepted by the REAL validator, not by
// a reimplementation of its rules. A test that independently re-derived
// "base64, 32 bytes" would keep passing after SessionSigningKeySeed
// diverged from it — testing a replica of the wiring instead of the
// wiring.
func TestGenerateSessionSigningKey_IsAcceptedByTheRealValidator(t *testing.T) {
	raw, err := config.GenerateSessionSigningKey()
	require.NoError(t, err)

	cfg := config.Config{Env: "production", SessionSigningKey: raw}
	seed, err := cfg.SessionSigningKeySeed()
	require.NoError(t, err, "generated key must be accepted outside dev")
	require.Len(t, seed, 32)
}

// A generator that returned a constant would pass the test above.
func TestGenerateSessionSigningKey_IsNotConstant(t *testing.T) {
	a, err := config.GenerateSessionSigningKey()
	require.NoError(t, err)
	b, err := config.GenerateSessionSigningKey()
	require.NoError(t, err)
	require.NotEqual(t, a, b, "each call must produce fresh entropy")
}

// It must never emit the committed dev key, which SessionSigningKeySeed
// refuses outside dev — a generator that did would produce a value that
// works on a developer machine and refuses to boot in production.
func TestGenerateSessionSigningKey_IsNotTheDevKey(t *testing.T) {
	raw, err := config.GenerateSessionSigningKey()
	require.NoError(t, err)
	require.NotEqual(t, config.DevSessionSigningKey, raw)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/config/ -run TestGenerateSessionSigningKey -v`
Expected: FAIL — `undefined: config.GenerateSessionSigningKey`

- [ ] **Step 3: Write the implementation**

Create `backend/internal/config/generate.go`:

```go
package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateSessionSigningKey mints a fresh SESSION_SIGNING_KEY value in
// exactly the format SessionSigningKeySeed accepts: a base64-standard
// encoded 32-byte Ed25519 seed (#45, spec D6).
//
// It exists so that provisioning a key is a command rather than a
// runbook instruction. An operator following prose reaches for `openssl`
// and produces a PEM block or a wrong-length value; SessionSigningKeySeed
// refuses both, but it refuses them at BOOT, against the shared
// production instance, at the moment an operator is least equipped to
// debug a base64 length mismatch.
//
// crypto/rand only — never math/rand, and never a passphrase derivation.
// This value is the entropy behind every HMS session; anything
// predictable here is session forgery for every subject in every tenant.
func GenerateSessionSigningKey() (string, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return "", fmt.Errorf("config: generating session signing key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(seed), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/config/ -run TestGenerateSessionSigningKey -v`
Expected: all three PASS

- [ ] **Step 5: Prove the first test can fail, and that the mutation moves a value it reads**

Temporarily change `ed25519.SeedSize` to `31` in `generate.go`, then rerun:

Run: `cd backend && go test ./internal/config/ -run TestGenerateSessionSigningKey_IsAcceptedByTheRealValidator -v`
Expected: **FAIL** with `SESSION_SIGNING_KEY decodes to 31 bytes, want exactly 32`

That message comes from `SessionSigningKeySeed` itself, which confirms the test is exercising the real validator rather than a copy of its rules. Revert the mutation and confirm the test passes again.

- [ ] **Step 6: Add the CLI and make target**

Create `backend/cmd/session-key/main.go`:

```go
// Command session-key prints a fresh SESSION_SIGNING_KEY value.
//
// Deliberately prints the key and nothing else — no label, no
// newline-delimited preamble — so it can be piped straight into a
// secret store without a human copying it out of decorated output and
// introducing whitespace SessionSigningKeySeed would then trim or
// refuse.
package main

import (
	"fmt"
	"os"

	"github.com/tesserix/hms/internal/config"
)

func main() {
	key, err := config.GenerateSessionSigningKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(key)
}
```

Add to `Makefile` (near the other developer targets):

```makefile
# Mints a SESSION_SIGNING_KEY in the exact format the API accepts (#45,
# spec D6). Prints ONLY the key, so it can be piped into a secret store.
# See docs/runbooks/secrets.md for where it goes.
secret-session-key:
	@cd backend && go run ./cmd/session-key
```

- [ ] **Step 7: Verify the target end to end**

Run: `make secret-session-key`
Expected: a single 44-character line ending in `=`, and nothing else.

Verify the shape of that exact output — one line, decoding to 32 bytes:
```bash
KEY=$(make -s secret-session-key)
[ "$(printf '%s' "$KEY" | wc -l)" -eq 0 ] || { echo "FAIL: multi-line output"; exit 1; }
printf '%s' "$KEY" | base64 -d | wc -c    # expect: 32
```
Expected: `32`, with no "multi-line output" failure.

Step 5 already proved the validator accepts generator output; this step is only checking the *target* emits one clean line, because a label or stray newline would be trimmed or refused downstream. Do not re-assert validator acceptance here by running the generator again — running the generator twice proves nothing about the validator.

- [ ] **Step 8: Confirm nothing else broke**

Run from the repository root (these targets `cd backend` themselves — do not prefix them):
```bash
make lint-go && make test-go && make coverage-go
```
Expected: all green. The coverage gate is flaky under container load — re-run once before investigating a failure.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/config/generate.go backend/internal/config/generate_test.go backend/cmd/session-key/main.go Makefile
git commit -m "feat: mint session signing keys in the format the validator accepts (#45)"
```

---

### Task 3: Secret inventory and provisioning runbook

**Files:**
- Create: `docs/runbooks/secrets.md`

**Interfaces:**
- Consumes: `make secret-session-key` from Task 2.
- Produces: the document Task 4 executes against.

- [ ] **Step 1: Write the runbook**

Create `docs/runbooks/secrets.md`. It must contain, at minimum:

**Inventory** — a table of the two secrets with, for each: the env var, the OpenBao path, what it authorises, and the blast radius if leaked. Plus an explicit row stating `HMS_WEB_ORIGIN` is **config, not a secret**, and belongs in the deployment env (D1) — recorded here because it refuses boot like a secret does and will otherwise be filed as one.

**Delivery chain** (D4), verbatim:
```
OpenBao → ESO (as the hms-api ServiceAccount) → k8s Secret → env → config.Load
```
with the note that nothing after ESO knows what a vault is, and that switching stores is a `ClusterSecretStore` edit rather than an HMS change.

**Provisioning: SESSION_SIGNING_KEY**
```bash
make secret-session-key            # prints the key, nothing else
# then write it to kv/data/hms/api/session-signing-key (Task 4)
```

**Provisioning: ZITADEL_LOGIN_CLIENT_TOKEN** — steps to create an HMS-specific machine user with `IAM_LOGIN_CLIENT` on the production Zitadel and mint a PAT.

This section MUST carry D2's residual risk in the operator's own words:

> The `IAM_LOGIN_CLIENT` role is **instance-level**. A holder can finalise an
> OIDC auth request for any app on this Zitadel instance, including other
> Tesserix products. HMS having its own machine user buys independent
> revocation, attribution, and independent rotation — it does **not** narrow
> what the credential can do once read. Zitadel offers no narrower role.
> Storing it under an HMS path bounds who can read it, not what it can do.

**Rotation** — state plainly that rotation currently requires a pod restart, because env is read once at boot (D4), and that this is accepted for boot secrets and not for the per-tenant credentials #45 also describes.

**What this runbook does not cover** — per-tenant credentials, scheduled rotation, the revocation runbook. Name the owning issues (#45's remaining scope, #54 for the audit half).

- [ ] **Step 2: Verify every command in the runbook actually runs**

Execute each shell command the runbook contains, in order, on a clean checkout. A runbook whose commands have never been run is prose. Fix anything that does not work as written.

- [ ] **Step 3: Correct the issue text**

The naming convention in #45 (`hms-in/{env}/{scope}/{name}`) contradicts D3 and would send the next reader down a GSM-shaped path incompatible with OpenBao's policy model.

```bash
gh issue comment 45 --body "$(cat <<'EOF'
The naming convention in this issue's Recommended Solution (`hms-in/{env}/{scope}/{name}`) is superseded. It was written assuming GCP Secret Manager, where a name is a flat string. Under the OpenBao store the fleet actually runs, the leading path segment is what a policy grants on, so HMS follows the live fleet convention instead:

```
kv/data/hms/api/session-signing-key
kv/data/hms/api/zitadel-login-client-token
policy read-hms → kv/data/hms/*
```

No `{env}` segment: environments are separated by cluster and namespace. See `docs/superpowers/specs/2026-08-17-secrets-management-design.md` D3 for the full reasoning, and D1 for why `HMS_WEB_ORIGIN` is config rather than a secret.
EOF
)"
```

- [ ] **Step 4: Commit**

```bash
git add docs/runbooks/secrets.md
git commit -m "docs: secret inventory and provisioning runbook for HMS boot secrets (#45)"
```

---

### Task 4: Prove the OpenBao path against the production store

**Files:** none in this repo. Changes land in `tesserix-k8s` and in the production cluster.

**Interfaces:**
- Consumes: `docs/runbooks/secrets.md` (Task 3), `make secret-session-key` (Task 2).
- Produces: a verified grant; no code artefact.

> **STOP — this task mutates production.** There is no dev cluster; the
> `tesseract-devtest-gke` context is unreachable. Every step below is additive
> and separately reversible, but **each must be confirmed with the user before
> execution**. Do not batch them. If confirmation is not given, stop and report
> which assertions therefore remain unproven — do not substitute a local
> simulation and describe it as evidence.

**Unresolved before starting** (D2's open item): whether creating a machine user on the shared production Zitadel needs platform-team sign-off. Confirm with the user first.

- [ ] **Step 1: Confirm the current state has not drifted**

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
kubectl --context $K get pods -n openbao
kubectl --context $K get clustersecretstore,secretstore -A
```
Expected: `openbao-0/1/2` `1/1 Running`; `openbao-secret-store` and the four existing per-app stores `Valid`/`Ready`. If this differs from the spec's observed-state table, stop and re-establish the facts before changing anything.

- [ ] **Step 2: Create the namespace and ServiceAccount**

```bash
kubectl --context $K create namespace hms
kubectl --context $K create serviceaccount hms-api -n hms
```
Both are additive and are needed by #824 regardless. Reversal: `kubectl delete namespace hms`.

- [ ] **Step 3: Add the OpenBao policy, auth role and whitelist entry**

Open a pull request against `tesserix-k8s` adding to `charts/thirdparty/openbao/values.yaml`:

```yaml
    - name: read-hms
      hcl: |
        path "kv/data/hms/*"     { capabilities = ["read"] }
        path "kv/metadata/hms/*" { capabilities = ["read", "list"] }
```
under `bootstrap.policies`, and:
```yaml
    - name: read-hms
      serviceAccounts: ["hms-api"]
      namespaces: ["hms"]
      policies: ["read-hms"]
      ttl: 1h
```
under `bootstrap.kubernetesRoles`, and:
```yaml
  - namespace: hms
    apps:
      - name: hms-api
        serviceAccount: hms-api
```
under `namespaceWhitelist`.

Per that file's own comment, a new namespace also needs a `destinations` entry in `argocd/prod/projects/security.yaml`, or ArgoCD refuses to render its store. Add it in the same PR.

- [ ] **Step 4: Write the two secret values**

Generate the signing key with `make secret-session-key` and mint the Zitadel PAT per the runbook, then write both to `kv/data/hms/api/session-signing-key` and `kv/data/hms/api/zitadel-login-client-token`.

Neither value may be echoed into a shell history, a log, or this session's transcript. Pipe them; do not print them.

- [ ] **Step 5: Assertion 1 — a read under HMS's grant succeeds**

Authenticate as the `hms-api` ServiceAccount and read `kv/data/hms/api/session-signing-key`.
Expected: the value is returned.

- [ ] **Step 6: Assertion 2 — a cross-namespace read is denied, and the denial is real**

This is **the assertion that matters**: an over-broad policy behaves identically to a correct one on every positive test, so only the denial distinguishes them.

Under the same `hms-api` grant, read `kv/data/homechef/*`.
Expected: **permission denied**.

Then prove the denial is the policy's doing and not an artefact:
1. Confirm the same path **is** readable under a grant that legitimately has it — otherwise a typo'd path produces an identical-looking denial.
2. Confirm the read succeeds under HMS's own prefix (Step 5 already shows this), so the credential itself is working.

Only both together establish that the policy is what refused. Record which of the two checks was run; if either was skipped, say so rather than claiming the assertion passed.

- [ ] **Step 7: Assertion 3 — the console cannot read values**

Confirm the `secret-service` policy has no `read` on `kv/data/*` — it holds `create`/`update`/`delete` and metadata access only, deliberately, so compromising the console yields no secret value.

Expected: a read attempt under that policy is denied.

- [ ] **Step 8: Report**

Write the three assertion outcomes into `docs/runbooks/secrets.md` under a "Verified" heading, dated, stating for each what was run and what was observed. Mark explicitly anything not exercised. Per this repo's practice: state what was checked and mark the rest unchecked.

- [ ] **Step 9: Commit**

```bash
git add docs/runbooks/secrets.md
git commit -m "docs: record verified OpenBao grant scoping for HMS boot secrets (#45)"
```

---

## Definition of done

- `make lint-go` clean; `cd backend && ./scripts/coverage-gate.sh` green; `go test -race ./...` green.
- gitleaks reports no leaks with `.gitleaks.toml`, and was **observed rejecting** a synthetic credential.
- `make secret-session-key` emits a key the real validator accepts, proven by a test observed failing under a 31-byte mutation.
- `docs/runbooks/secrets.md` exists, every command in it has been run, and it carries D2's residual risk in operator-facing words.
- #45 carries a comment correcting its naming convention.
- Task 4's three assertions are either verified and recorded, or explicitly reported as unproven with the reason.

## Out of scope (do not build these)

Per-tenant credential API and metadata table; scheduled/dual-credential rotation and Temporal orchestration; the manifest lint (nothing to lint until #824); SAST/dependency/container scanning (#713); the container image, chart and ArgoCD app (#824); emergency revocation audit trail (#54).
