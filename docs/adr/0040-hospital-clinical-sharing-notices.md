# ADR-0040: Hospital clinical sharing notice review

Status: accepted for the notice-review API slice; clinical grant design remains open.

Patientmaster owns hospital identity and reviewed self-authority. Account notice
publication under MediConnect does not establish clinical purpose, hospital review
or consent to release a record. Clinical sharing wording needs immutable versions
and an independent hospital review before it can be offered for consent.

Keep hospital sharing notices in patientmaster with tenant/hospital forced RLS,
a current registrar grant and a separate notice-review permission. A creator
cannot approve their own draft. Preserve exact wording, consequence, locale,
policy and review references across approval and terminal retirement. Audit and
metadata-only outbox intent commit with transitions. Global API is the client
entry point. No legal content or policy is seeded.

A new service would duplicate identity scope and add an unnecessary remote
failure boundary. Extending account notices would conflate account and clinical
processing. A mutable published string would destroy historical consent evidence.
The design envelope, dependencies, forward-only migration and test plan are in the
[notice specification](../superpowers/specs/2026-10-25-clinical-sharing-notices.md).

This decision establishes only notice ownership. It neither changes the existing
requirement for clinical consumers to verify current original claim state nor
introduces a consent evaluator. Consent ledger/signing, proxy authority, immediate
revocation, owner-authorized signed record release and actual approved wording
must be completed before a patient clinical-sharing journey can be enabled.
