# Engineering principles

Binding on every contributor, human or agent. The backend and frontend
standards say *how* to write code here; this says *what standard of solution is
acceptable at all*. Where a rule below conflicts with expedience, the rule wins.

HMS is a multi-year enterprise build for hospitals. Its failure modes are
clinical and regulatory, not cosmetic: a wrong merge of two patient records is a
patient-safety event, a cross-tenant read is a reportable breach under the DPDP
Act, and an unaudited access is an accreditation finding. Code written to "get
something working" is not cheaper here — it is the expensive option paid later,
by someone with less context, under an incident.

## 1. No minimal, MVP, quick or temporary solutions

**Do not propose, plan or implement a deliberately reduced version of the
correct solution.** This applies to design proposals as much as to code.

Banned as a justification: "for now", "MVP", "quick win", "we can harden it
later", "good enough to unblock", "temporary shim", "TODO: do this properly".

If the correct solution is large, the response is to **decompose it into
correct slices**, not to build an incorrect small one. A slice may do less;
it may not do it wrongly. Concretely:

| Acceptable | Not acceptable |
|---|---|
| Ship deterministic identifier matching now, fuzzy matching in the next slice | Ship a name-equality "matcher" that silently merges records |
| Support one country profile, with the profile seam in place | Hardcode Indian formats and add the seam later |
| Support 100 rows per page behind a cursor contract | Return `LIMIT 100` with no cursor and call it pagination |
| Defer a feature entirely, and say so | Half-build it so it looks present but does not hold |

The test: **would a reviewer who knows this domain accept this as the finished
answer to the narrower question it claims to answer?** If it only survives
review because everyone agrees it is temporary, it does not ship.

## 2. Scope down, never quality down

When work is too big, cut *scope* — the set of cases handled — and keep quality
constant across what remains. Never cut correctness, isolation, error handling,
tests or observability to fit a smaller footprint.

State explicitly what a slice does **not** cover, in the design doc and the PR
body, so the gap is a recorded decision rather than an assumption someone
inherits.

## 3. Fail closed

Every control fails to the safe side, and the safe side is stated in a comment
at the point of the decision. Authorization errors deny (503, never fail open).
Redaction failures mask the whole value rather than emit a partial one. A
tenant predicate that cannot be evaluated returns nothing rather than
everything. An unparseable input is rejected, not guessed at.

Where a control *deliberately* fails open — `LOG_LEVEL` degrading to `info`
rather than refusing to boot — that choice is argued in the code comment, not
merely made.

**What "the safe side" means depends on what the control protects.**

- A control protecting **data, isolation or identity** fails **closed**. If it
  cannot decide, nobody gets in. Authorization, tenant scoping, redaction,
  credential revocation, membership: all of these deny on error, and a 503 is
  the correct answer to "the authorization system cannot answer right now".
- A control protecting **capacity** fails **open**, with an alert. A rate
  limiter whose backing store is unreachable must not take a hospital's API
  down over quota accounting — availability of a clinical system outranks
  enforcing a limit that exists to protect that same availability. Denying
  every request because the limiter is confused causes precisely the outage the
  limiter was installed to prevent.

The distinction is not a licence to argue any control into the second category.
Ask what the failure actually costs: a wrongly-admitted request past a capacity
control costs some load; a wrongly-admitted request past a data control costs a
reportable breach. If a control has both characters, it fails closed.

Either way the direction is stated in a comment at the decision point, with the
reasoning — so the next person cannot "fix" a deliberate fail-open into a
fail-closed, or the reverse, by pattern-matching on this document.

## 4. Enforce structurally, not by convention

A rule that depends on a developer remembering it is not a control. Prefer, in
order:

1. **Impossible to express** — the unexported gin group on `*platform.Router`,
   which makes an undeclared route a compile error.
2. **Fails at boot** — refusing to start when the DB role can bypass RLS.
3. **Fails in CI** — the RLS migration lint, the arch tests, the coverage gate.
4. **Documented convention** — last resort, and only for things the first three
   genuinely cannot reach.

When you add a rule to a standards document, ask what would have caught it
mechanically, and build that instead where you can.

## 5. Verify the claim, not a proxy for it

Two expensive defects in this repo came from screening something adjacent to
the real thing: a log redactor that screened `fmt.Sprint(v)` while `slog`
emitted `encoding/json`, and a health check asserting each zone returned 200
without ever exercising the browser-to-API proxy that was broken.

- Assert on the **bytes, rows or responses actually produced**, never on a
  reconstruction of them.
- **Prove a test can fail.** Break the implementation, watch the assertion go
  red, restore it. An assertion never observed failing is not evidence.
- **Test a claim before recording it as a blocker.** A finding asserted in an
  audit and never reproduced was carried on three issues here before being
  disproved.

## 6. Design before code, and write it down

Non-trivial work goes: issue → design spec in `docs/superpowers/specs/` →
implementation plan in `docs/superpowers/plans/` → implementation. The spec
records the alternatives rejected and *why*, because the reasoning is what
survives to the next decision; the decision alone does not.

When a design is withdrawn or superseded, **correct the documents that assert
otherwise** in the same change. A spec that describes a design the code no
longer has is worse than no spec.

## 7. Build vertical slices

A feature is backend, frontend, migration and tests together, delivered as one
reviewable change. A backend-only PR whose UI arrives three weeks later has not
been proven to work; it has been proven to compile.

## 8. Leave the schema right the first time

Migrations are append-only and hospitals do not take downtime. Decisions that
are cheap now and expensive forever — composite `(tenant_id, id)` foreign keys,
UUIDv7 with a version CHECK, precision rather than boolean estimation flags,
check digits on printed identifiers — are made at first write, not retrofitted.

## 9. Every change is auditable and attributable

Reads of clinical data are as sensitive as writes. Where a change touches PHI
access, the audit consequence is part of the design, not a follow-up.

## 10. Definition of done

A change is done when all of the following hold. "Done except for" is not done.

- [ ] Acceptance criteria from the issue are converted into assertions.
- [ ] `make lint-go` clean; `go test -race ./...` green;
      `backend/scripts/coverage-gate.sh` green.
- [ ] `pnpm turbo lint type-check test build` green.
- [ ] At least one new assertion was observed failing before it passed.
- [ ] Docs that the change makes untrue are corrected in the same PR.
- [ ] The PR body states what is *not* covered and why.

---

**If a request appears to ask for a quick or minimal solution, do not silently
comply.** Say what the correct solution is, propose a decomposition that
reaches it, and let the human decide the scope. Reducing quality is not a
decision an agent makes on its own.
