# Patient Master and Registration — Design

**Date:** 2026-08-22
**Status:** Draft (brainstorming session with Mahesh)
**Resolves:** [#70](https://github.com/tesserix/helivanta/issues/70) (patient registration)
**Constrains:** [#36](https://github.com/tesserix/helivanta/issues/36) (patient identity), [#473](https://github.com/tesserix/helivanta/issues/473) (patient master MDM)
**Related:** #74 (ABHA linking), #71 (patient history), #75 (self-registration), #61/#62 (consent and data sharing), #83 (prescriptions)

## Context

There is no patient on this platform. Identity is a free-text string, duplicated per module:

```sql
medicore_visits    ... patient_name text NOT NULL
pharmacy_dispenses ... patient_name text NOT NULL
```

Every clinical record that matters — prescriptions (#83), lab results, dispensing history —
must attach to one longitudinal person, and none of them can be built correctly until that
person exists. This is the foundation the prescribing epic was blocked on.

The hard part is not the table. It is matching. In Indian practice, duplicate creation is the
default outcome, not an edge case: names transliterate inconsistently (Md / Mohammed /
Mohammad), dates of birth are approximated to 1 January, one mobile serves a household, and
the same person attends several facilities of a group. A missed match fragments history — the
clinician sees no allergies and no prior creatinine trend. A **false merge** is worse, grafting
one person's diagnoses onto another, and is extremely hard to unwind once orders and bills
reference it.

Registration is also the first point of processing under the DPDP Act 2023, so a consent
notice and receipt are part of creating a patient, not a later addition.

## Decisions

### D1. The patient master is Platform-owned, not MediCore's

A new `patientmaster` module beside `iam`, with the standard `platform.Module` surface.

Patients are not a MediCore concept. Pharmacy, lab and the future patient app all resolve to
the same person, and a pharmacy-only tenant — which the entitlement model explicitly allows —
has patients without ever buying MediCore. Putting the master inside MediCore would make every
such tenant depend on a product it did not purchase.

Modules never import one another, so nothing reads `patients` directly. Other modules learn
identity through the event contract and the HTTP API.

*Rejected: `patients` inside `medicore` (#70's framing).* Simpler today, because registration
is a front-desk flow and MediCore is the first consumer. It fails the moment a tenant buys
PharmaConnect alone, and moving it later is a migration across every module holding a reference.

### D2. Data model, and three deliberate deviations from #70

`patients` and `patient_identifiers`, both tenant-scoped with RLS enabled **and forced**,
`USING (hms_tenant_visible(tenant_id))`, `WITH CHECK` pinned to a single tenant. The ordinary
shape — this design needs no policy exception.

Identifiers live in their own table (type, value, `verified_at`) rather than as columns on
`patients`, so ABHA linkage (#74) adds rows, not a migration.

Three deviations from #70, recorded so they read as decisions rather than oversights:

- **UUIDv4, not UUIDv7.** #70 specifies v7. PostgreSQL 16 has no native `uuidv7()`, every
  other table here uses `gen_random_uuid()`, and keyset pagination already orders on
  `(created_at, id)` — so v7's index locality buys little while the inconsistency costs
  every reader. Revisit if the platform moves to PostgreSQL 18.
- **MRN is tenant-scoped, not facility-scoped.** #36 correctly says an MRN is a per-facility
  identifier and not the identity. No facility concept exists. MRN is unique per tenant, in a
  shape where a facility dimension is additive when facilities arrive.
- **Aadhaar is never stored.** Masked last four plus a keyed hash used only for matching. The
  raw value is never written to disk or logs; the existing PHI-redaction logger covers the log
  path. ABHA is the preferred anchor wherever present.

### D3. Matching is a pure package with three bands

`internal/modules/patientmaster/matching` takes a candidate and a corpus and returns scored
bands. No database, no HTTP, no clock. That isolation is what makes it testable against a
corpus of real Indian name variants rather than through the API.

In slice 1:

- **Deterministic:** ABHA number; verified mobile + normalised name + date of birth.
- **Probabilistic:** Jaro-Winkler over transliteration-normalised names, an Indic-aware
  phonetic key, date-of-birth proximity, mobile.

Address locality is deliberately excluded — it is the weakest signal in #70's list and needs a
structured address model and locality reference data that do not exist. Adding it later changes
scores, not the interface.

Thresholds are configuration, not constants. They will be tuned against real data, and a
threshold that requires a code change to tune will not be tuned.

### D4. A confident match blocks; the override is attributable and becomes the merge worklist

| Band | Behaviour |
|---|---|
| Confident | **Block.** The clerk selects the existing record, or overrides with a recorded reason. |
| Possible | Candidates shown, ranked. The clerk chooses existing or new. No block. |
| No match | Proceed. |

Merge and unmerge are out of slice (#36). That is only honest if slice 1 avoids creating the
duplicates it cannot remedy — hence blocking rather than warning. The override exists because a
false positive must never stop a registration during the 09:00 OPD rush; a clerk who cannot
register the patient in front of them will work around the system entirely.

Every override writes `patient_duplicate_overrides` — actor, reason, both patient ids,
timestamp. When merge ships, that table is its worklist. This is what converts a deferral into
a queue instead of into debt.

*Rejected: warn but always allow.* Fastest at the counter, and duplicates accumulate from day
one with no remedy. *Rejected: pull merge into slice 1.* Reversible merge with full lineage is
the hardest correctness problem in the epic and deserves its own design pass.

### D5. The consent receipt is written in the same transaction as the patient

Notice version, timestamp, acting user, and — where the patient is under 18 — guardian name,
relationship and the consenting party, all committed with the patient row.

Registration is the first point of processing under DPDP. A patient row that exists without a
consent receipt is a compliance defect, and one transaction makes that state unreachable rather
than merely discouraged.

Consent *management* — withdrawal, purpose-scoping, the correction right — is #61/#62.

### D6. Events carry `patient_id` and nothing identifying

Subjects `helivanta.in.patientmaster.registered.v1` and
`helivanta.in.patientmaster.demographics_updated.v1`, published through the outbox inside the
business transaction.

The payload carries the patient id, and no name, date of birth, mobile or address. #835 was
precisely "PHI leaves the RLS boundary in event payloads", and `internal/archtest`'s payload
check already forces every contract field to be classified. A consumer needing demographics
calls the API under its own permission, where authorization is enforced per request.

This keeps clinical data out of the outbox by construction rather than by review.

### D7. `visit_created` gains `patient_id` additively

`medicorecontract.VisitCreatedData` gains `patient_id` **alongside** the existing
`patient_name`. Nothing breaks; pharmacy and lab adopt when they next touch their own slices;
`patient_name` is removed in a later contract version once nothing reads it.

The contract package exists so that renaming a field breaks consumers' builds on purpose.
Additive-then-retire is the sanctioned way through that, and it lets three modules migrate on
three schedules instead of one coordinated cutover.

### D8. `smetrics` for the metrics, a bounded ruleset for the Indian-specific normalisation

The survey the repo's reuse rule requires has run. Its result changed this decision, so it is
recorded here rather than in the plan.

**Jaro-Winkler is solved.** `github.com/xrash/smetrics` (MIT, ~235 stars, long-standing) provides
Jaro-Winkler, Soundex and Metaphone in one focused library. Use it; write none of that.

**Indic phonetic matching is not solved in Go.** What exists:

| Candidate | Verdict |
|---|---|
| Go phonetic packages (`smetrics`, `f1monkey/phonetic`, `gofuzz`) | Soundex / Metaphone / NYSIIS / Caverphone — English-centric; Soundex performs poorly on Indian names |
| Indic Soundex ports (PHP, Python — all of Santhosh Thottingal's SILPA algorithm) | No Go implementation; none production-grade (0-3 stars) |
| `knadh/knphone` | Kannada-only, **and GPL-3.0** — a licensing problem for this platform |

There is also a mismatch of problem: Indic Soundex algorithms encode Devanagari/Indic script,
while this platform stores Latin-script transliterations. "Md / Mohammed / Mohammad" is
transliteration variance in Latin, not phonetic encoding of Indic script — so porting an Indic
Soundex would be applying the wrong tool competently.

**Decision:** take Jaro-Winkler and Soundex from `smetrics`, and write a small, explicitly
bounded normalisation layer for the rules this data actually exhibits — honorific stripping,
`Md`/`Mohd` → `Mohammed` style expansions, vowel collapse, and name-order tolerance.

`smetrics` offers `Jaro`, `JaroWinkler`, `Soundex`, `Hamming`, `Ukkonen` and `WagnerFischer` —
verified against the package, **not** Metaphone. An earlier draft of this decision named Double
Metaphone from `smetrics`; that function does not exist, and the correction is recorded here
rather than silently applied.

The ordering is what makes Soundex acceptable despite the criticism above: normalisation runs
FIRST and collapses the Indian-specific variance, so Soundex encodes a regularised form rather
than raw transliteration. The normalisation ruleset, not the encoder, is where correctness
lives — and it is the part covered by the corpus tests.

That ruleset is not an algorithm. Each rule is one corpus test case, and the set is enumerable
and reviewable — which is what separates it from hand-rolling a phonetic encoder, the thing
this decision exists to avoid.

*Rejected: adding `f1monkey/phonetic` for Metaphone or Beider-Morse.* BMPM is the best technical
fit for transliterated multi-lingual names. At ~20 stars it is too thin a dependency to place on
the patient-identity path of a hospital platform, where it would become ours to maintain the day
it goes quiet.

*Rejected: porting SILPA's Indic Soundex to Go.* Faithful to #70's wording and useful to the
ecosystem, but it targets the wrong script, and porting plus validating an algorithm nobody on
this team can eyeball is a slice of its own.

*Rejected: Jaro-Winkler alone, no phonetic key.* Smallest and fully library-backed, but it
misses names that differ in letters while sounding identical — a real share of the duplicates
#70 was written about.

## Error handling

| Condition | Behaviour |
|---|---|
| Cross-tenant patient lookup | 404, never 403 — existence is not leaked |
| Matcher returns an error | **Registration refused.** Proceeding unmatched is what creates the duplicate, so this fails closed |
| Confident match, no override | 409 with the candidate, not a silent create |
| Override without a reason | 400 — the reason is the control, not a courtesy |
| Consent receipt write fails | Whole transaction rolls back; no orphan patient |

## Testing

- **Matching corpus tests**, unit-level and offline: transliteration variants (Md / Mohammed /
  Mohammad), 1-January approximated dates of birth, shared household mobiles, genuine
  same-name-different-person pairs. Both directions matter — the false-merge cases are the ones
  that cause clinical harm, so the corpus must contain pairs that must NOT match.
- **Band behaviour**: block, candidate list, and override each asserted end to end, including
  that an override without a reason is refused.
- **Consent atomicity**: proven by forcing the consent write to fail and asserting no patient row survives.
- **Adversarial RLS**: a second tenant reads zero rows; a write pinned to another tenant is rejected by `WITH CHECK`.
- **No PHI on the bus**: assert the published payload contains only the id — a test that fails if a future field adds a name.
- Every new assertion proven capable of failing by mutation, not inspection.

## What this slice does not cover

- **Merge and unmerge**, and the stewardship review queue (#36). Slice 1 detects and blocks; it does not remedy.
- **ABHA linkage** (#74). The identifier table is shaped for it; the ABDM HIP flow is not built.
- **Patient self-registration** from the patient app (#75) — it will call the same matching package.
- **Longitudinal history and timeline** (#71).
- **Address locality matching**, and any structured address model.
- **Migrating pharmacy and lab off `patient_name`** — additive event field only (D7).
- **Console screens.** API-first; the UI follows once the API is settled.
- **FHIR R4 `Patient` projection.** The column set is chosen to map cleanly, but the projection
  itself belongs with the ABDM epic that consumes it.

## Limitations and what is not verified

- **NOT VERIFIED: matching thresholds.** No real corpus exists yet. The bands are configurable
  precisely because the initial values are a starting point, and the spec makes no claim about
  their precision or recall. First tuning pass needs de-identified data from a pilot hospital.
- **NOT VERIFIED: the normalisation ruleset's coverage.** D8's survey ran and settled the
  approach, but the ruleset itself is derived from described failure modes, not from observed
  data. Which expansions and collapses actually matter is a question only a real corpus answers,
  and the set will need revisiting once one exists.
- **A blocked registration is a real operational cost.** If the confident band is set too wide,
  clerks override routinely and the reason field fills with noise — which looks like compliance
  while providing none. The override rate is the metric that tells you the thresholds are wrong,
  and nothing in this slice reports it. Worth adding when the stewardship queue lands.
- **Guardian consent is recorded, not verified.** DPDP requires *verifiable* guardian consent
  for a child. Slice 1 records who consented and their stated relationship; it does not verify
  that relationship. Closing that gap depends on identity infrastructure this platform does not
  have, and it must not be presented to a hospital as compliance with the verifiability
  requirement.
