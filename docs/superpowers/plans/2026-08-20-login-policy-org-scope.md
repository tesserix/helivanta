# Org-scoped MFA policy read (#913) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The MFA policy that decides whether a password-only session may finalize is read against the **authenticating user's** organization, and an unknown org fails closed instead of falling back to an unscoped read.

**Architecture:** The org id is lifted out of the `GET /v2/sessions/{id}` response `CompleteIfSufficient` already makes, threaded through `classifyEnrolledMethods`, and passed to a policy read that refuses an empty org id. The pre-credential display read stays unscoped under a name that says so, pinned by an archtest.

**Tech Stack:** Go 1.26, Zitadel v4.15.3 (local dev stack), testify.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-08-20-login-policy-org-scope-design.md`. D1–D5 below refer to it.
- **No unscoped fallback, ever** (D2). An empty org id is `ErrUnavailable`, not a request.
- **No new round trip** (D1). If a task adds a second `GET /v2/sessions/{id}` per login, it has missed the design.
- **Do not touch #856 / #901.** KNOWN LIMITATIONS §1 stays exactly as written; only §2 is deleted.
- **Prove every assertion can fail**: mutate the implementation, watch the test fail, revert — *and confirm the mutation actually applied to the file* before trusting what the test then says.
- **Commit messages:** single line, conventional commits, no signature, no `Co-Authored-By`.
- **Before done:** `make lint-go` clean, `cd backend && ./scripts/coverage-gate.sh` green, `go test -race ./...` green.

---

## File Structure

| File | Responsibility |
|---|---|
| `backend/internal/modules/iam/loginclient/client.go` | `requestOption`/`withOrgID`, `do` variadic opts, `LoginPolicyForOrg` + `InstanceLoginPolicyForDisplay`, `sessionSubject` |
| `backend/internal/modules/iam/loginclient/client_test.go` | Header-on-the-wire, empty-org-id refusal, existing `LoginPolicy` tests moved to the new names |
| `backend/internal/modules/iam/loginclient/sufficiency.go` | `classifyEnrolledMethods` returns the subject; `CompleteIfSufficient` scopes the read; KNOWN LIMITATIONS §2 deleted |
| `backend/internal/modules/iam/loginclient/sufficiency_test.go` | Fixtures gain `organizationId`; a missing org id hands off |
| `backend/internal/modules/iam/loginui.go` | Display call site renamed |
| `backend/internal/archtest/arch_test.go` | `InstanceLoginPolicyForDisplay` is unreachable from `sufficiency.go` |
| `backend/internal/modules/iam/loginui_integration_test.go` | The live second-org proof (D5) |

---

### Task 1: `do` takes typed request options; the policy read splits in two

**Files:** `client.go`, `client_test.go`

**Interfaces:**
- Produces: `LoginPolicyForOrg(ctx, orgID)` and `InstanceLoginPolicyForDisplay(ctx)`, both consumed by Tasks 2 and 3. `LoginPolicy` ceases to exist under that name.

**Steps:**
- [ ] Add `requestOptions{orgID string}` / `requestOption func(*requestOptions)` / `withOrgID(string) requestOption` (D4). `do` gains `opts ...requestOption` and sets `x-zitadel-orgid` **itself** from the accumulated `orgID` — an option must not be able to reach `*http.Request` (that shape would let a future option overwrite `Authorization`).
- [ ] Split `LoginPolicy` into an unexported `loginPolicy(ctx, opts ...requestOption)` holding today's entire body (anchor check, `mfaPolicyKeys` loop, rename guard), plus two exported wrappers:
  - `LoginPolicyForOrg(ctx, orgID)` — `orgID == ""` returns `LoginPolicy{}, fmt.Errorf(...: %w, ErrUnavailable)` **before any HTTP call**.
  - `InstanceLoginPolicyForDisplay(ctx)` — no header; doc comment carries D3's residual limitation verbatim in substance (a multi-org instance can render a wrong hint, enforcement is unaffected, follow-up filed).
- [ ] Test: `LoginPolicyForOrg` with a non-empty org id puts `x-zitadel-orgid: <id>` on the wire — asserted by the fake Zitadel reading `r.Header.Get`, not by inspecting the client.
- [ ] Test: `LoginPolicyForOrg(ctx, "")` returns an error matching `ErrUnavailable` **and the fake server's handler was never invoked** (a counter asserted zero). "Returns an error" alone would pass against an implementation that made the unscoped request and then failed for an unrelated reason.
- [ ] Test: `InstanceLoginPolicyForDisplay` sends **no** `x-zitadel-orgid` header at all (asserted absent, not empty).
- [ ] Point the existing `LoginPolicy*` tests at whichever wrapper matches their intent — the body-parsing ones (anchor, rename guard, `mfaPolicyKeys`) at `LoginPolicyForOrg`, since that is the enforcement path.

**Verification:** mutate `withOrgID` to set no header → the wire test fails. Mutate the empty-org guard to fall through → the never-invoked assertion fails. Revert both, confirming each edit landed.

---

### Task 2: the session read yields the org, and `CompleteIfSufficient` scopes with it

**Files:** `sufficiency.go`, `client.go`, `sufficiency_test.go`

**Interfaces:**
- Consumes: Task 1's `LoginPolicyForOrg`.
- Produces: nothing later tasks depend on.

**Steps:**
- [ ] Rename `sessionUserID` → `sessionSubject`, returning `sessionSubject{UserID, OrgID string}` decoded from `factors.user.{id,organizationId}`. Keep today's fail-closed empty-`UserID` check unchanged. **Do not** validate `OrgID` here (D2: one control point, at the policy call).
- [ ] `enrolledMethodTypes` takes a `userID` rather than a session id, so the session read happens exactly once per login. `classifyEnrolledMethods` performs the session read and returns the subject alongside `totpEnrolled, uncollectible`.
- [ ] `CompleteIfSufficient` passes `subject.OrgID` to `LoginPolicyForOrg`. The existing fail-closed branch (`slog.Warn` + `OutcomeHandoff`) already covers a refused empty org id — do not add a second branch.
- [ ] `CompleteAfterFactor` uses the same `classifyEnrolledMethods` and ignores the org id; it reads no policy.
- [ ] Delete KNOWN LIMITATIONS §2 from `CompleteIfSufficient`'s doc comment and renumber nothing else (§1 stays §1, it is a different, still-open gap). Replace with a short statement of what is now true: the read is scoped to the session's org and an absent org id hands off.
- [ ] Add `"organizationId":"o1"` to every fake-Zitadel session fixture in `sufficiency_test.go` and `client_test.go` — the real instance always sends it, and a fixture that omits it is a fixture that lies.
- [ ] Test: a session whose `factors.user` carries **no** `organizationId` produces `OutcomeHandoff` and **finalize is never called** (the existing `finalized.Load()` pattern in this file).
- [ ] Test: the policy request the fake Zitadel receives carries the org id **from the session response**, not a constant — use a distinctive value and assert on it, so a hardcoded id would fail.

**Verification:** mutate `CompleteIfSufficient` back to an unscoped read → the org-id-on-the-wire test fails. Mutate `sessionSubject` to default a missing org to the login client's → the handoff test fails.

---

### Task 3: the display call site, and a structural bar on reaching it from the enforcer

**Files:** `loginui.go`, `arch_test.go`

**Steps:**
- [ ] `LoginUIHandlers.AuthRequest` calls `InstanceLoginPolicyForDisplay`. Update the surrounding doc comment: the value is advisory, may be wrong on a multi-org instance, and is not what enforces anything.
- [ ] Archtest: parse `loginclient/sufficiency.go` and assert `InstanceLoginPolicyForDisplay` is not referenced anywhere in it — modelled on the existing `sufficient{}`-construction test in `arch_test.go`, with a failure message naming #913 and saying what to use instead.
- [ ] Prove the archtest can fail: temporarily add the call to `sufficiency.go`, watch it fail, revert.

---

### Task 4: the live proof — a second-org user is not judged by the login client's org

**Files:** `loginui_integration_test.go`

**Steps:**
- [ ] Add an org-scoped variant of `managementAPICall` (an `orgID` argument that sets `x-zitadel-orgid`, empty meaning "unscoped, as today") rather than a parallel copy of the function.
- [ ] Helper `createOrgWithUser(t, env, seedToken)` → `(orgID, loginName string)`: `POST /management/v1/orgs`, then `createImportedUser` scoped to that org. `t.Cleanup` deletes the org and **reads it back** to prove the delete took.
- [ ] `TestIntegration_ForceMFAPolicyInAnotherOrg_HandsOffInsteadOfCompleting` per D5: second org forces MFA, login client's own org left at default, real `POST /v1/auth/login/password` as the second-org user → `handoff_url` present, `callback_url` absent.
- [ ] **Observe the failure first.** Run this test against `main`'s unscoped read before Tasks 1–2 are merged in the working tree (or with the fix reverted) and record the observed `callback_url` in the PR body. A test that has never been seen red proves nothing here.
- [ ] Second assertion in the same test: with that org's policy at `forceMfa: false`, the same user's login **succeeds** with a real `callback_url`. Without it, a permission refusal on the cross-org read would pass the first assertion via the fail-closed path and the test would be green for the wrong reason (D5).
- [ ] Resolve the two live unknowns and record the answers in the PR body: (a) can the **login-client PAT** read another org's policy with `x-zitadel-orgid`; (b) does the Helivanta project admit a user from a second org, or is an org grant required. If (a) is refused, stop and re-decide — the fix would be fail-closed but non-functional for real multi-org users, which is a different design.

---

### Task 5: close out

- [ ] `make lint-go`, `cd backend && ./scripts/coverage-gate.sh`, `go test -race ./...` — all green, output quoted in the PR body.
- [ ] File the D3 follow-up issue (org-aware display hint after the login name is known) and reference it from `InstanceLoginPolicyForDisplay`'s doc comment.
- [ ] PR body: `Closes #913`, the observed-red evidence from Task 4, the two live answers, and what this slice does **not** cover (multi-org support generally; the display hint).
