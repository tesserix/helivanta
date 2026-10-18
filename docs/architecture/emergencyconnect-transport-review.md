# Clinical review of a transport requirement

This draft slice runs in the production web application's `apps/shell` workspace.
It records a reviewed transport requirement for an existing intake. It does not
reserve a vehicle, dispatch a crew, change clinical priority, confirm a booking or
obtain receiving-hospital acceptance. Those programme steps remain open.

## Establish access

1. An administrator configures an operating base at `/emergency`. Confirm the
   provider's reviewed policy and the identity subject from the trusted identity
   directory. Do not use a patient's identity or an unverified email as a subject.
2. Give the reviewer an explicit reader grant on that base. A tenant role alone
   grants no case access. A base grant alone grants no clinical-review permission.
3. The reviewer must hold the doctor role with `emergency.case.assess` and an
   independently verified, unexpired clinical delegation for that base.
4. In the base configuration, use **Load clinical review access** with the subject.
   A confirmed `404` permits a new version-zero grant; a network, authorization or
   server error permits no editor. Review the displayed current version.
5. Enter a traceable approval reference, an explicit expiry and enable access.
   **Save clinical review access** records the authenticated verifier, not a
   verifier supplied by the browser. Enabling or renewing one's own access is
   rejected by the API and database. Grants last at most 366 days.

Delegation is an approval to perform this software action. It does not establish
professional registration or current employment. The canonical workforce register
introduced in #1231 remains the authority for reviewed qualification evidence.
Live reservation requires a verified staff-to-identity link, current canonical
qualification and approved dispatch policy; that integration is not implemented
by this slice. Do not treat manually entered fleet credential references or a
clinical delegation as substitutes.

## Record the requirement

1. Open a received case through its operating base. Protected detail is fetched
   with no-store semantics and removed when the detail component unmounts.
2. The server checks the reviewer's current delegation. An absent, revoked,
   expired or inaccessible grant offers no review form. The server repeats the
   checks inside the write transaction regardless of what the browser shows.
3. Select a transport class and enter the approved policy reference and an
   explicit validity deadline. No class or deadline is preselected. Datetime
   controls use local time and reject nonexistent or ambiguous daylight-saving
   times rather than silently moving the deadline.
4. Save the requirement. It is attributed to the authenticated reviewer and exact
   grant and case version. Validity must end within 24 hours and no later than
   the delegation's expiry. Closed cases and unsupported neonatal, mortuary and
   home-response requests cannot acquire a dispatch requirement here.
5. Read the paginated history. Its display order is not an eligibility decision.
   A future reservation must select the current sequence and revalidate grant,
   qualification, case, vehicle, duty and safety constraints while holding the
   appropriate locks. A historical review must never silently authorize dispatch.

The case remains `received` and `untriaged`. The success message explicitly states
that no vehicle has been reserved or dispatched.

## Recover and revoke

- An ambiguous write failure freezes its path, ID and complete body. Retry the
  original command. Do not open another intake to work around a delayed response.
  Only a definitive `400` or `413` releases the original form for correction.
- A stale version is a conflict. Reload the current object and review the change;
  do not blindly increment versions or assert that the previous command failed.
- A second verifier cannot claim another verifier's successful command as an
  identical retry. Grants, reviews and audit/outbox writes remain atomic.
- An administrator may revoke delegation; a holder with the management permission
  may revoke their own. **Revoke clinical review access** copies the original
  expiry instant, which avoids blocking revocation on a daylight-saving fold.
  Refreshing the review panel removes the form after revocation while preserving
  history. Backend checks block further writes even if a tab still shows old UI.
- Revocation does not erase evidence. Reviewer versions and assessment history are
  append-only, protected by forced tenant/base/subject RLS. Event payloads contain
  identifiers and versions, without clinical narrative, approval references or
  identity subjects.

## API routes

These authenticated Go routes are prefixed `/v1`; the browser uses `/api/v1`.

| Route                                                        | Permission and additional scope                                                      |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `GET/PUT /emergency/bases/:base/clinical-reviewers/:subject` | `emergency.clinical.manage`; current tenant and base metadata                        |
| `GET /emergency/bases/:base/clinical-reviewer`               | `emergency.case.assess`; explicit base access and own active grant                   |
| `PUT /emergency/bases/:base/cases/:case/assessments/:id`     | `emergency.case.assess`; base access, independent active delegation and current case |
| `GET /emergency/bases/:base/cases/:case/assessments`         | `emergency.case.read`; explicit base access; cursor pagination                       |

`0003_emergency` is additive. This draft has not been migrated or activated in
production. Keep the public call-first emergency page available independently of
staff workflow availability.
