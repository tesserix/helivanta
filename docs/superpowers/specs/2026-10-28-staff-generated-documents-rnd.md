# Staff payslips, offers and exit letters

Researched 2026-10-28. Proposed acceptance and renderer evaluation; not implemented
or deployed. Continues [staff lifecycle research](2026-10-27-staff-lifecycle-rnd.md),
[payroll configuration ADR-0029](../../adr/0029-payroll-configuration.md) and stories
#1040, #1041 and #1107. The current leave-review PR does not complete these stories.

## Ownership and current gaps

Workforce owns employment, salary configuration, payroll results and letter
records. IAM owns permission assignments; hospital branding remains owned by its
existing platform owner. Clinical authority is never inferred from a letter.
Apply ORG-AC-01/02/04/05/06/07/08 and the India knowledge index's source-review rule.
State/UT, establishment, employment category, effective date and qualified payroll
or HR review must be recorded before activating legal terms or statutory rules.

Source inspection found protected Workforce evidence scanning/release and
permission-specific downloads in `document_processor.go`, `document_upload.go`
and `document_access.go`. These are not a PDF renderer. The current upload path
accepts inspected PNG/JPEG evidence: generated PDF support needs a separately
defined, validated owner path, not an assumption that uploading arbitrary PDF
bytes to the image path is supported. Payroll configuration and preview likewise
do not establish a finalized run, payment, employee payslip or final settlement.

## Go renderer evaluation

Start with a Go renderer within the existing application/worker boundary, not a
new public microservice or third-party upload of confidential payroll. Evaluate:

| Candidate                                               | Evidence and decision gate                                                                                                                                                                                                                          |
| ------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [signintech/gopdf](https://github.com/signintech/gopdf) | MIT, Go generation and Unicode font embedding documented by the maintainer. First spike candidate; prove table pagination, rupee glyph, long names and Indian-script shaping before selection. Font embedding alone does not prove correct shaping. |
| [Maroto v2](https://github.com/johnfercher/maroto)      | MIT layout components; inspect the exact pinned provider and transitive dependencies. A convenient grid does not resolve font or maintenance risks.                                                                                                 |
| [UniPDF](https://github.com/unidoc/unipdf)              | Pure Go with document-processing features; licensing/production entitlement must be reviewed before adoption. No paid licence has been purchased.                                                                                                   |
| [go-pdf/fpdf](https://github.com/go-pdf/fpdf)           | Repository is archived. Do not select this archived repository as a new maintained default. Review forks individually rather than inferring maintenance.                                                                                            |

Pin the selected version, inspect its licence/dependencies and run security checks.
Bundle licensed fonts and approved logo bytes. Never let a template resolve
arbitrary URLs, local paths, scripts or employee-supplied HTML. Bound page count,
text lengths, image dimensions, memory and execution time. Renderer errors leave
generation failed/retryable, without an available file or false success.

Use ADR-0029's provisional 2,000 staff/hospital and 60 heads/structure envelope.
For evaluation only, 2,000 monthly documents at 100 KiB each imply about 2.3 GiB
per hospital at 12 months and 6.9 GiB at 36 months, before replicas and corrections.
A provisional three-minute batch implies about 12 documents/second. Measure
bounded worker concurrency, p99 generation time and representative worst-case
documents before accepting a latency target. Private storage and authorization
are critical dependencies; rendering may retry but must never bypass either.
No new infrastructure cost or paid renderer is approved by this proposal.

## Payslip content and layout

Produce a readable A4 document with hospital-approved logo, legal name, address,
brand colours, title, payroll period, unique document number, revision, issue date
and page numbering. Freeze the branding/template revision used at issue; later
branding edits must not rewrite old slips. Use a two-column earnings/deductions
table, clear totals, distinct employer contributions and a prominent net-pay area.
Long values wrap, headings repeat across pages and monetary columns align.

Require an approved payroll result, staff/employment period, hospital, currency,
pay period, payable/paid/LOP days, salary source revisions, template revision and
approver before final issue. Show employee name/code, department/designation when
recorded, joining date when required by the selected template, earnings, arrears,
approved reimbursements and each applicable deduction. Display gross earnings,
employee deductions, net pay and approved YTD totals from the same result.
Employer PF/EPS/ESI and other employer costs appear separately; they must not be
deducted from net pay merely because they form part of CTC.

PF/UAN, ESI, professional tax, LWF, salary TDS, tax regime/year and any other
applicable statutory entries resolve reviewed effective revisions and coverage.
Mask bank/account/PAN/UAN identifiers by default and authorize any disclosure.
An unavailable tax computation is not zero tax. Explicit not-applicable states
need attributable policy/coverage evidence. Do not print a diagnosis, background
check detail or unrelated identity evidence on a payslip.

The Income Tax Department's [Form 130 FAQ](https://www.incometaxindia.gov.in/documents/d/guest/form-130-faqs)
identifies an annual TDS certificate and its prescribed TRACES process. A locally
generated monthly payslip is not that statutory certificate. Do not label our
renderer output a valid Form 130 or generate a replacement for an official form.
The official [PIB release of 23 September 2026](https://www.pib.gov.in/PressReleasePage.aspx?PRID=2314111&lang=1&reg=3)
confirms a revised EPFO ceiling and explicitly qualifies application by scheme
rules. This corroborates part of the earlier research; it does not authorize
automatic payroll defaults or every contribution/transition calculation.

## Offer and exit letter workflows

Offer: draft from a hospital-approved versioned template; validate candidate/staff
reference, role/designation, engagement, proposed joining date, location, salary
annexure source, authorized signatory and applicable reviewed terms. A prospective
candidate must not require active employment or receive access merely to receive
an offer. HR edits drafts; an independently authorized approver approves; generate
and review the PDF before an authorized issue/share action. Record delivery and
acceptance separately. Revision, withdrawal, decline, expiry and joining preserve
history. Acceptance or a signed PDF must not silently activate IAM access.

Exit: distinguish resignation acceptance, relieving, service/experience and final
settlement letters. Resolve an approved exit event, actual employment dates,
effective last-working/access-cutoff instants, handover/clearance evidence and
authorized signatory. Do not claim dues are settled when payroll/payment remains
pending. Sensitive disciplinary/background reasons stay out of routine letters.
HR edits a draft; approved or issued documents require a new attributed correction
revision with independent review, never overwriting the issued bytes. A letter
does not itself revoke sessions, settle payroll or acknowledge property return.

Only the authorized owner workflow can issue/share. Staff self-service gets its
own reviewed self-only disclosure purpose; an inactive employment link cannot be
reused to expose documents. No real employee communication is authorized by this
research or synthetic QA; delivery integrations require their own accepted scope.

## Persistence, security and end-to-end acceptance

Persist immutable source/template/font/logo revisions, input snapshot digest,
document identity/revision, actor and approval evidence, generation status,
renderer version, stored-object generation/checksum and supersession links.
The idempotency key must bind the complete intended render inputs. Exact retries
return the same result; changed inputs require a new revision. Use atomic owner
state/outbox updates, durable retry and a visible status after partial storage or
worker failure. No payroll/tax/private identifier payload belongs in events/logs.

Download through Global API and owner authorization, with tenant/hospital/object
scope, sensitive-read audit and private/no-store response headers. No permanent
public URL. Recheck permission at release time; a revoked assignment or stale
session cannot redeem an old download grant. Rollback must retain issued
documents/history and stop new generation safely, not erase payroll evidence.

Acceptance must include synthetic HR maker → independent approver → employee
download, reload/relogin persistence, unauthorized/foreign-scope denial, blank and
conditional required fields, effective-version changes, concurrent edits, exact
retry, renderer/storage failure, revocation and correction/reissue. Render PDFs
to page images and inspect layout, clipped text, logo quality, embedded fonts,
rupee/Indian names, long tables and multi-page offers; verify extracted amounts
against the approved calculation and all totals. HTML preview alone is not PDF QA.
Electronic signatures, mandatory retention, legal wording and jurisdictional wage
slip requirements remain explicit qualified-review decisions before activation.

## Follow-up source and issue review — 2026-10-28

Rechecked open stories #1040 (payroll preparation/locking/processing/correction),
#1041 (versioned salary/tax/payslip policy) and #1107 (employment letters/exits).
The registered Workforce routes in `backend/internal/modules/workforce/module.go`
provide salary-head/structure revisions, statutory rules and independent review,
staff salary assignments, protected identifiers/tax evidence and calculation
previews. Attendance finalisation is a separate owner operation. No registered
payroll-run finalisation, payslip issue, offer issue or exit-letter issue route
was found in that route registry. `payroll_calc.go` and the private evidence
processor are existing foundations, not evidence that these missing workflows
are complete.

Implementation must preserve this dependency order:

1. Prove versioned payroll inputs and attendance lock/exception evidence, then
   implement reproducible preparation, independent finalisation and attributed
   correction/arrears with period-level duplicate protection (#1040/#1041).
2. Evaluate the Go renderer against synthetic immutable results, hospital branding
   and the typography/pagination/amount assertions above. Define protected
   generated-PDF persistence and release separately from image-evidence upload.
3. Connect payslip approval/issue/reissue and employee disclosure to finalized
   result revisions. Prove HR maker, independent approver and employee browser
   journeys, including revoked and foreign-scope downloads.
4. Add versioned offer/exit templates and distinct milestone/clearance handoffs
   under #1107, with the same protected generation and revision model. Letter
   issuance must not stand in for access revocation or payroll settlement.

This sequence records implementation dependencies; it activates no statutory
policy, grants no access and does not close any of these three stories.
