# Current account tenant name

The signed-in header must identify the current hospital or independent pharmacy
by its business name, including when only one account exists. The onboarding
service owns that name; branding, a branch list and tenant identifiers are not
substitutes. IAM retains current subject, membership and permission authority.

Extend the existing private identity-profile projection with an optional
`includeTenantName` request flag. Only profile requests may opt in. Existing
requests and responses remain byte-shape compatible: no unsolicited field is
added for older strict decoders. An opted-in, active native profile includes
the current active customer's validated `tenantName`; status, membership,
and inactive projections disclose no name. Legacy profiles may describe a
subject with a different native home organization; the returned name still
identifies the verified session tenant, never that native home organization.
Preserve the
current product-enrolment, native scope, workload and audit checks.

The Helivanta adapter opts in and requires the name, then adds `tenant_name` to
the existing authenticated `/iam/me/profile` response. The header reuses the
existing profile query. A missing, invalid or failed read shows an explicit
unavailable state, never an ID or a stale name from another session. This name
is display metadata and grants no access. No new endpoint, datastore, identity
credential or permission is introduced.

Retain existing platform assumptions of 1000/3000 peak RPS at months 12/36,
99.95% availability and p99 read target 500 ms. This adds at most one indexed
organization read to a profile request, at most 200 Unicode code points to the
existing bounded 8 KiB projection, and no new polling, writes or persistent
browser storage. The existing ten-second identity-owner deadline and audit
failure refusal remain. No asynchronous recovery or compensation is needed for
this read; retrying reauthorizes and audits again.

Deploy the owner first, then its consumer. Rollback restores the old request
without the flag; the new owner continues to emit the old response shape.
Tests cover compatibility, wrong-purpose opt-in, malformed and Unicode names,
foreign tenant scope, legacy native ownership, inactive state, audit failure, single-account display, unavailable
profile and unchanged switch cancellation/logout behavior. Separate accounts
still require destination authentication; a label never transfers tokens or
silently links equal email addresses. Live multi-account acceptance requires
two independently authorized native accounts and remains separate evidence.
