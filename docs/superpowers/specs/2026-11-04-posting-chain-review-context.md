# Protected current supervisor-chain review

Candidate for #118 and #616; depends on #1569. Workforce owns Staff and postings,
Organization owns departments/ancestry, IAM owns scoped permissions and OpenFGA
projection. Pharma and future departments consume canonical references.

## Scope and capacity

Reuse ADR0056 planning assumptions: 50 peak requests/s,80:20 reads:writes,
99.9% availability,p99 below500ms;500,000/1.5million Staff at12/36 months.
These remain unmeasured targets. At most31 ancestors (32 including the target),
no recursion beyond the existing approval limit; one bounded batch label query.
A page includes only the selected target rationale and minimal canonical ancestor
references, never ancestor rationale, evidence, salary, login or grant snapshots.
No service, cache, table or migration is introduced; marginal cost is bounded
owner reads and existing audit storage. Reject a serialized body above128KiB.

## Contract and authority

Opt into existing protected context GET with `chain_review=current`, alongside
`purpose=review`, exact version/revision and `supervision_context=v1`. Cannot
combine management review; legacy responses stay byte-shape compatible.
Return target context, retained non-root intent, ordered immediate-parent-to-root
nodes (canonical supervisor-choice projection plus retained intent), checked_at
and the minimum source/nominal cutoff across the target and ancestors.

Actor is an independent, currently eligible Staff reviewer with current
hospital-scoped posting.review + posting.read + Staff actor use assignments.
Existing IAM compound guards, current membership/epoch/OpenFGA acknowledgement,
Staff actor guard and target separation-of-duties remain authoritative. Management
attestation cannot substitute for departmental chain review. No template expands.
ORG-AU-05/07,ORG-ST-02/03/05 and lifecycle access rules apply; no ORG-D decision
is settled. No clinical or statutory authority is conferred by a posting.

Use the same bounded source/interval/cycle/ancestry validation as approval and
hold sources/Organization references through audit commit. A view is a checked
snapshot, never a reservation: approval revalidates. No business state/receipt/event
is written by reading it. IAM/Staff/Organization unavailable yields503, invisible
object404, missing capability403, malformed selector400, stale source409. Audit
failure withholds the entire response and rolls back actor-use evidence.

## Consumers and recovery

SDK validates target scope/version, every linked node, uniqueness/depth/root,
interval containment, exact canonical IDs and cutoff. Cancellation withholds data.
UI separately opens current chain review, shows names/kinds/intervals and reference
IDs, and permits approval only after explicit chain/source confirmation. Preserve
root attestation only on root commands. Unknown outcomes retain exact command/body;
receipt recovery remains usable after deadlines, while new commands fail closed.
Base rejection/withdrawal remain available if chain eligibility changes.

Alternative: ancestor protected-detail calls disclose unrelated private rationale
and create unbounded request fanout. Discovery pages are hints and cannot replace
current chain proof. Copying Staff/department masters breaks ownership. Reuse
existing owner guards rather than introducing a second validity interpretation.

Rollout removes the opt-in UI/selector on rollback; no data schema changes.
Specific production revision approval remains required. Browser acceptance,
remaining lifecycle/operating consumers and full Staff acceptance remain open.
