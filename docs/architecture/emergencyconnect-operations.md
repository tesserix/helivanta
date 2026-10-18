# EmergencyConnect staff intake operations

Programme [#1220](https://github.com/tesserix/helivanta/issues/1220), implementation
[#1227](https://github.com/tesserix/helivanta/issues/1227). This guide covers the
staff intake slice. Public `/ambulance` and the native app remain call-first.
A received case is **not** an ambulance booking, clinical triage, dispatch,
crew acceptance or hospital acceptance.

## Ownership and tenancy

The Helivanta tenant is an independently administered organisation. An ambulance
provider may be its own tenant. A hospital group can operate several bases inside
its tenant, with independent explicit grants per base. A base does not confer
access to a hospital's patient registry. Branches, laboratories and pharmacies
continue to use their own domain scopes; proximity never creates a data-sharing
agreement. Independent hospitals need explicit service agreements and a later
scoped inter-tenant exchange, not a shared tenant or a global dispatcher role.

One Zitadel subject can be a member of several Helivanta tenants. Route permission
is resolved for the active tenant and then intersected with the live base grant.
A grant in tenant A cannot read tenant B. A tenant administrator configures bases
and grants but needs an explicit base grant before seeing a case. A base grant
cannot replace tenant membership or route permission. Case references resolve
within their operating base; reusing a client ID elsewhere creates an independent
record rather than exposing a private case through a global uniqueness conflict.
Future cross-base CRM must retain the base context when presenting a reference.

| Action                                | Tenant permission       | Required base grant                                |
| ------------------------------------- | ----------------------- | -------------------------------------------------- |
| Discover base metadata / own grant    | `emergency.base.read`   | Own-grant lookup requires a live grant             |
| Configure base, grant or revoke scope | `emergency.base.manage` | Tenant administrator; case access remains separate |
| Read case / queue / timeline          | `emergency.case.read`   | reader, dispatcher or supervisor                   |
| Receive report                        | `emergency.case.intake` | dispatcher or supervisor                           |
| Close an unassigned received report   | `emergency.case.close`  | supervisor                                         |

New `emergency_dispatcher` and `emergency_supervisor` roles are additive. No doctor,
nurse or receptionist inherits emergency permissions. Grant an appropriate tenant
role through existing IAM administration, then grant the verified subject at the
base. Use a separate administrator to revoke a subject that should lose access.

## Configure and receive a synthetic report

1. Open **EmergencyConnect → Operating bases & reports** in the staff console.
2. As tenant administrator, enter the operating-base name, real IANA timezone,
   reviewed processing-policy reference and approved caller-contact retention.
   The prefilled 30 days is an editable input, not a legal recommendation.
3. Save the base. It initially shows **No case access**.
4. Verify the staff identity's subject and active tenant membership. Assign its
   emergency tenant role through IAM, then grant the required base access level.
5. Open the base. Readers see metadata only; dispatchers and supervisors also see
   **Receive a report**. Unknown patients and callers need no fabricated identity.
6. Record only necessary caller and incident details. Requested BLS/ALS/etc. is a
   report of the caller's request, not a suitability decision or clinical priority.
   Non-emergency patient/mortuary transport cannot use the emergency processing
   basis. Follow the operator's reviewed policy and notice/consent procedure.
7. Receive the report. The API returns 202 only after case, timeline, audit and
   metadata-only outbox commit together. Status remains `received / untriaged`.
8. A network/503 failure retains the original report and case ID for retry. Do not
   start another report because a response was lost. Definitive 400/413 rejection
   permits correction; a 409 needs investigation rather than an automatic retry.
9. Open the received case for an audited decrypted view. Caller data is omitted
   after contact expiry. The queue and events contain no caller or incident text.
10. A supervisor may close an unassigned intake with its current version, coded
    reason and encrypted explanation, after confirmation. This cannot stand down
    an ambulance or arrange alternative care. Clinical closure needs its own
    approved workflow before the dispatch programme is activated.
11. Revoke base access with confirmation. Subsequent case reads return 404. Other
    tenant/base grants remain independent. Backend isolation is authoritative.

## Encryption and maintenance

OpenBao Transit uses non-exportable derived keys
`helivanta-emergency-<tenant UUID>`. Authenticated context binds tenant, base, case
and field; caller, incident and closure are separate envelopes. Original request
and closure fingerprints use key-version-1 HMAC to preserve retry comparisons
across rotation. No plaintext fallback or keys in the frontend. API identity gets
only encrypt/decrypt/HMAC prefix paths; ESO and operational identities remain
separate. Existing staff-key permissions are preserved.

Caller expiry is frozen at intake from the base policy, independent of later base
edits. Reads mask expired contacts even if maintenance is delayed. Run the
`/app/emergency-retention -batch-size=200 -max-batches=10` binary with existing
OpenBao-projected application and system DB credentials. It uses no owner DSN,
OpenBao credential, NATS connection or patient payload. It has a five-minute
budget, SKIP LOCKED batches and one audit row in the same transaction per purge.
Only caller ciphertext is cleared; incident, version, timeline and outbox remain.
A runtime-only DB cannot execute this cross-tenant job. Do not invoke it through
an HTTP endpoint. The system DB role is the existing operational trust boundary;
its new table grants permit only the contact columns and append-only audit insert.

The owning infrastructure branch contains a disabled, suspended CronJob. Once
reviewed and the image promoted, its intended interval is 15 minutes, at most
2,000 contacts/run, concurrency forbidden and a 330-second job deadline. It uses
the API's promoted **in-region GAR image**, distinct pod labels, no mounted service
account token, no HTTP service and no sidecar that can keep a completed job alive.
Read-only root filesystem and bounded CPU/memory are declared. Job failure and
repeated batch-ceiling log warnings require operational investigation; read-time
masking continues during an outage. Verify fresh `lastSuccessfulTime` and inspect
metadata-only purge totals. The chart includes an overdue-success alert; verify its kube-state-metrics input
and Alertmanager route to the named operational owner before live intake.

Purging the primary caller envelope is not deletion from every backup. CNPG backup
retention, restore-time re-purge, separately approved incident/closure retention,
legal holds and audit preservation must be agreed before real patient data enters
this workflow. This code does not certify SOC 2, HIPAA or DPDP compliance.

Keep PostgreSQL autovacuum and statistics maintenance healthy. Bulk imports and
restores need reviewed database-owner statistics maintenance before judging query
plans; never grant schema-owner rights to the API or disable RLS to improve a plan.
The separate 100,000-case fixture checks the metadata queue and expiry-selector
indexes, not a complete HTTP or clinical response SLA.

## Release and recovery

1. Review the application diff and the separate `tesserix-k8s` OpenBao/GitOps diff.
   Both repositories must stay PUBLIC. Do not put real incidents in issues/tests.
2. Run the checks recorded in the evidence directory. The synthetic browser
   fixture substitutes identity, not domain storage. It does not prove production
   Zitadel login, signed native builds or a real operational response.
3. Obtain specific approval for the production migration and rollout. The additive
   `0001_emergency` migration creates only new tables/functions and grants; never
   edit an already released migration or delete old clinical records to roll back.
4. Promote the application image through existing CI/GitOps and deploy the exact
   emergency Transit policy before enabling case intake. An absent/refusing
   policy returns 503 and does not store plaintext. Do not patch OpenBao by hand.
5. Enable the maintenance CronJob in a reviewed values change after verifying its
   binary is in the promoted image. Unsuspend only with the approved retention
   policy and operational owner. Read-only inspection confirms actual success.
6. Verify new roles reconcile through OpenFGA. Use a dedicated synthetic base and
   test identity, test receive/read/close/revoke, and check for no caller plaintext
   in queue/event/audit/log output. Preserve verification evidence securely.
7. Code rollback leaves new tables intact. Keep compatible Transit permissions and
   retention maintenance for any data already collected; do not revoke decrypt
   keys, drop tables, rotate credentials or revert medical history as rollback.

## Browser integration reproduction

With the local shell running (for example `http://127.0.0.1:4340`) and Colima active:

```sh
PATH="$HOME/.nvm/versions/node/v24.11.1/bin:$PATH" \
HELIVANTA_BROWSER_ORIGIN=http://127.0.0.1:4340 \
DOCKER_HOST="$(docker context inspect --format '{{.Endpoints.docker.Host}}')" \
TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
go -C backend test -tags=emergencybrowser ./internal/modules/emergency \
  -run TestEmergencyConsoleBrowser -count=1 -v
```

The explicit browser tag runs a real mobile-viewport journey with disposable
Postgres, OpenBao and NATS, and synthetic harness identity. It refuses a non-local
shell or API target. It cannot activate a production dispatch service. The
ordinary Go suite exercises tenant/permission rules separately against OpenFGA.

## Fleet foundation

The staff console also includes versioned fleet certification, manually verified
crew credentials, bounded duty, measured readiness and persistent safety
constraints. Follow [the fleet guide](emergencyconnect-fleet.md) for the exact
permissions, setup, replay semantics and limitations. This is not live dispatch
or a completed workforce/asset integration; #126 and #127 remain open.

## Dependent programme work

The following remain separate deliverables in #1220: approved structured triage
(#119/#116/#743), home response (#120), live citizen booking/status (#121/#122/#1221),
provider tenancy (#21), fleet/capability/crew readiness (#126/#127/#420/#134), dispatch
and reservation races (#123), crew app/telemetry (#124/#125), routing and access
(#128/#135), receiving-hospital acceptance (#129), handover/timeline (#130/#131),
offline reconciliation (#132), verified 112 integration readiness (#133), provider
and hospital choice (#1223), CRM/tariffs/resolution (#1224), and clinical pilot safety
case (#1222). There is no assumed government dispatch API. The transport clinical
lead and real operator/hospital owners must approve protocols and operational
readiness before citizen booking can promise a response.
