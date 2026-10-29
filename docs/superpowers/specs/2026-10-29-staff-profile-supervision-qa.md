# Staff profiles and supervision: scoped delivery and QA

Status: implementation in progress, not full area acceptance. User priority is now profiles/supervision; Access/RBAC provisioning and employment-lifecycle gaps remain open in the [delivery tracker](../../qa/staff-management-progress.md).

## Existing owner and boundaries

Workforce owns hospital-local staff/enrollment, profile classifications and the recorded supervisor relationship. The existing private GET/PUT requires `workforce.enrollment.manage` plus an explicit hospital manager grant, with forced tenant/hospital RLS and locked authority. Directory-only managers retain their existing basic-directory capabilities; readers gain no editing. A profile supervisor is HR metadata and grants neither application permission nor clinical supervision authority. IAM retains login, membership and assignment ownership. Department master selection is not a dated transfer/posting workflow; platform organization structure remains the department owner under ADR-0044.

Current implementation has optimistic staff versions, exact unchanged retries, mandatory field/child-row validation, same-hospital supervisor references, acyclic reporting-chain constraints, and retained staff records instead of DELETE. Private profile editing currently lacks the actor/workspace preconditions already used by identity drafts. Cross-tab invalidation improves presentation but cannot bind an in-flight command to the account that opened the form.

## First correct slice: bind private profile editing to its account

Add an opt-in `request_context=v1` enrollment projection and a canonical `request_context_version` acknowledgment. The projection requires exactly one expected-tenant and expected-subject header matching the verified principal; headers are assertions, never authority. Enrollment, directory lists, HR masters and identity routes honor optional supplied context assertions; existing document/account-link 404 mismatch contracts remain unchanged so private profile choices cannot come from a changed context. Legacy enrollment requests without the projection keep their response shape and optional-header compatibility. Current mandatory identity-case header requirements remain intact.

A strict request-body version marker accompanies projected PUTs: an older owner rejects this unknown field before writes, even when a GET in a mixed deployment reached a new owner. The default input remains compatible when no projection/marker is supplied.

The Staff workspace resolves and captures the account before fetching hospitals or records, keys private queries/forms and choices by tenant/subject, uses no-store/zero-retention queries, and pins both assertions on reads/writes. Missing acknowledgment from an older API leaves editing unavailable. A wrong staff ID in an enrollment response must not open an editor. An account/workspace conflict removes the private form and requires it to be reopened; it must not retry under the new account. Ordinary version conflicts retain drafts and keep the explicit reload confirmation.

Assets are confidential staff profiles and attributable changes. Relevant actors include authorized HR administrators, another hospital/tenant, directory-only managers and a later user on a shared browser. Boundaries are the browser session, Global API forwarding and the Workforce owner transaction. No cache, header, job title or supervisor selection supplies authority. Outages fail closed before private editing; successful writes still commit profile/version/audit/outbox atomically.

Reuse the existing modular monolith, projection generator, API client and PostgreSQL model. No endpoint, permission, table or service is added. Existing 32 KiB write and 50/200 pagination bounds remain. Planning assumptions and supervision budgets remain those in the existing supervision design; one bounded own-permission read is added on workspace entry, without polling or new infrastructure. No new production SLO or legal policy is asserted.

## Subsequent area gates

1. Exercise supervisor assignment, replacement, clear, self/cycle rejection, reload persistence, stale versions and related-role denial through the real local UI. Label inactive choices explicitly; do not invent an employment/clinical-authority policy from a profile field.
2. Audit transfers separately: department classification edits do not implement approved/effective-dated postings, cross-hospital handover, retained history or access cutoffs. Record owner/dependency gaps before designing that workflow.
3. Verify safe removal means the existing retention/inactivation contract, not erased history or global identity deletion. Full suspension/notice/resignation/termination/rehire and cross-owner access cleanup remain Access/lifecycle work.

## Verification and compatibility

Reproduce stale/missing/duplicate context assertions against actual owner HTTP/PostgreSQL before changing code; assert no profile, version, audit or outbox side effects. Test matching assertions, legacy response omission, unsupported projection, tenant/hospital denial and old API behavior. Observe UI regressions fail before implementation, including request headers, private-form removal on account conflict, retained ordinary conflict drafts and object/ack checks. Keep historical-API tests as denial coverage and update only current-contract fixtures to include the new acknowledgment and actual account identity.

Run canonical generation/check, contractdocs generation/check, affected race suites, Go vet/build/lint, full frontend checks and real UI QA. No production rollout or migration is authorized by this design. The current slice has no schema migration; release scope must still enumerate inherited main changes.
