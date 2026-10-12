# Patient Master and Registration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the platform a real patient — one longitudinal identity per person, with duplicate detection that blocks rather than warns, and a DPDP consent receipt written in the same transaction.

**Architecture:** A Platform-owned `patientmaster` module beside `iam`. Matching is a pure, database-free package so it can be tested against a corpus of real Indian name variants. Registration blocks on a confident duplicate and records an attributable override; that override table is the worklist for merge when merge ships. Other modules learn identity through an additive event field, never by reading the table.

**Tech Stack:** Go 1.26, Gin, GORM, PostgreSQL 16 (forced RLS), NATS JetStream, `github.com/xrash/smetrics`, testify, testcontainers.

**Spec:** `docs/superpowers/specs/2026-08-22-patient-master-registration-design.md`

**Issues:** [#70](https://github.com/tesserix/helivanta/issues/70) (resolves), [#36](https://github.com/tesserix/helivanta/issues/36) (constrains)

## Global Constraints

- Go 1.26. `slog` only — logrus is banned. Errors wrapped with `%w`.
- New modules: `make new-module NAME=<name>`; register in `cmd/api` AND `internal/archtest/arch_test.go`'s `allModules()`.
- Modules never import other modules, except a module's own `contract` package. Cross-module data flows via events only.
- Tenant data only via `WithTenant`. Every tenant table: RLS enabled **and forced**, `USING (hms_tenant_visible(tenant_id))`, `WITH CHECK` pinned to strict equality. **`WITH CHECK` must never call `hms_tenant_visible`** — `LintRLS` fails that shape.
- Migration IDs `NNNN_<module>`, append-only. Never edit a shipped migration.
- Every route declares a permission via `*platform.Router` (`authz.Public` to opt out). Modules declare `Permissions() []authz.Grant` and **never list `RoleTenantAdmin`** — the reconciler adds it.
- Handlers: `authn.TenantPrincipal(c)` for identity; `respond.*` for every response; **404 (never 403) for cross-tenant**; 409 via status-guarded UPDATE.
- Events: subjects `helivanta.<dir>.<module>.<event>.vN`; publish through the outbox inside the business tx. Contract packages are DATA ONLY — consts, types, vars; no funcs, no methods.
- **No PHI in event payloads.** Every contract field must be classified in `internal/archtest/event_payload_test.go`, in `eventPayloadPHIAllowlist` or `eventPayloadReviewedNonPHI`.
- **Aadhaar raw value is never written to disk or logs** — masked last four plus a keyed hash only.
- Before done: `make lint-go` clean, `cd backend && ./scripts/coverage-gate.sh` green (70% floor), `go test -race ./...` green.
- Every new assertion must be proven capable of failing — by mutation, not by inspection.

---

### Task 1: A front-desk role

The spec's actor is a registration clerk. `authz.systemRoles` has five roles and none of them is one; a nurse is not a clerk, and conflating them corrupts the audit trail this feature exists to produce.

Named `receptionist` rather than `registration_clerk` because it names the person, not one feature's view of them — it will still be the right name when appointments and queueing arrive.

**Files:**
- Modify: `backend/pkg/authz/authz.go`
- Modify: `backend/internal/archtest/matrix_test.go`
- Test: `backend/pkg/authz/authz_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `authz.RoleReceptionist authz.Role = "receptionist"`, included in `authz.systemRoles`.

- [ ] **Step 1: Write the failing test**

Append to `backend/pkg/authz/authz_test.go`:

```go
// TestReceptionistIsASystemRole pins the front-desk role into the
// registry. KnownRole is the gate every untrusted role_key passes
// through, so a role missing here cannot be granted at all.
func TestReceptionistIsASystemRole(t *testing.T) {
	require.True(t, authz.KnownRole(authz.RoleReceptionist))
	require.Contains(t, authz.SystemRoles(), authz.RoleReceptionist)
}
```

If the exported accessor is not named `SystemRoles`, use whatever `authz.go`'s exported accessor over `systemRoles` is called (there is one — it returns a defensive copy).

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./pkg/authz/ -run TestReceptionistIsASystemRole -v`
Expected: compile failure — `undefined: authz.RoleReceptionist`.

- [ ] **Step 3: Add the role**

In `backend/pkg/authz/authz.go`, in the role const block:

```go
	// RoleReceptionist is the hospital front desk: registration, and in
	// time appointments and queueing. Named for the person rather than
	// for one feature's view of them, so it stays correct as the front
	// office gains responsibilities. A nurse is not a clerk — conflating
	// the two would put clinical staff in the registration audit trail.
	RoleReceptionist Role = "receptionist"
```

And add it to `systemRoles`.

- [ ] **Step 4: Extend the permission matrix**

In `backend/internal/archtest/matrix_test.go`, add `authz.RoleReceptionist` to `allRoles()`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./pkg/authz/ && go test ./internal/archtest/ -run TestPermissionMatrix`
Expected: PASS. The matrix derives expectations from module declarations, so a role holding no permissions yet is correct.

- [ ] **Step 6: Prove the test can fail**

Remove `RoleReceptionist` from `systemRoles` (keep the const), re-run `TestReceptionistIsASystemRole`, confirm it FAILS on the `Contains` assertion. Revert.

- [ ] **Step 7: Commit**

```bash
git add backend/pkg/authz backend/internal/archtest/matrix_test.go
git commit -m "feat(70): add the receptionist system role"
```

---

### Task 2: Name normalisation — the Indian-specific ruleset

The spec is explicit that this ruleset, not the encoder, is where correctness lives. Pure functions only: no database, no clock, no HTTP.

**Files:**
- Create: `backend/internal/modules/patientmaster/matching/normalize.go`
- Test: `backend/internal/modules/patientmaster/matching/normalize_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func NormalizeName(raw string) string`, `func PhoneticKey(raw string) string`.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/modules/patientmaster/matching/normalize_test.go`:

```go
package matching_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/patientmaster/matching"
)

// TestNormalizeName is the ruleset, one case per rule. Each line is a
// real failure mode from #70: honorifics typed into the name field,
// Md/Mohd abbreviations, case and spacing noise, and punctuation.
func TestNormalizeName(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"lowercases", "SURESH", "suresh"},
		{"collapses whitespace", "  Suresh   Kumar ", "suresh kumar"},
		{"strips punctuation", "S.  Kumar-Rao", "s kumar rao"},
		{"drops the Mr honorific", "Mr Suresh Kumar", "suresh kumar"},
		{"drops the Smt honorific", "Smt. Lakshmi Devi", "lakshmi devi"},
		{"drops the Dr honorific", "Dr. Anil Sharma", "anil sharma"},
		{"expands Md", "Md Ali", "mohammed ali"},
		{"expands Mohd", "Mohd. Ali", "mohammed ali"},
		{"expands Mohammad spelling", "Mohammad Ali", "mohammed ali"},
		{"leaves Mohammed alone", "Mohammed Ali", "mohammed ali"},
		{"keeps an unknown name intact", "Priya Nair", "priya nair"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, matching.NormalizeName(tc.in))
		})
	}
}

// TestNormalizeNameIsIdempotent: normalising an already-normalised name
// must change nothing. Without this a rule that rewrites its own output
// (an expansion that re-matches its result) loops or drifts silently.
func TestNormalizeNameIsIdempotent(t *testing.T) {
	for _, in := range []string{"Mr Md Ali", "Dr. Suresh  Kumar", "Priya Nair"} {
		once := matching.NormalizeName(in)
		require.Equal(t, once, matching.NormalizeName(once), "input %q", in)
	}
}

// TestPhoneticKeyMatchesAcrossSpellingVariants is why normalisation runs
// BEFORE the encoder: Soundex over the raw strings does not reliably
// unify these, over the normalised form it does.
func TestPhoneticKeyMatchesAcrossSpellingVariants(t *testing.T) {
	require.Equal(t, matching.PhoneticKey("Md Ali"), matching.PhoneticKey("Mohammed Ali"))
	require.Equal(t, matching.PhoneticKey("Mohammad Ali"), matching.PhoneticKey("Mohammed Ali"))
}

// TestPhoneticKeySeparatesDifferentPeople is the half that prevents a
// false merge. A phonetic key that collapses everything is worse than
// no key at all.
func TestPhoneticKeySeparatesDifferentPeople(t *testing.T) {
	require.NotEqual(t, matching.PhoneticKey("Suresh Kumar"), matching.PhoneticKey("Priya Nair"))
	require.NotEqual(t, matching.PhoneticKey("Mohammed Ali"), matching.PhoneticKey("Mohammed Akram"))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/modules/patientmaster/matching/ -v`
Expected: package does not exist.

- [ ] **Step 3: Add the dependency**

```bash
cd backend && go get github.com/xrash/smetrics@latest
```

- [ ] **Step 4: Implement**

Create `backend/internal/modules/patientmaster/matching/normalize.go`:

```go
// Package matching decides whether two people are the same person.
//
// It is deliberately pure — no database, no clock, no HTTP — because
// the only way to trust it is to run it over a corpus of real name
// variants, and a package that needs a transaction to answer a question
// cannot be run that way.
package matching

import (
	"strings"
	"unicode"

	"github.com/xrash/smetrics"
)

// honorifics are titles that end up typed into the name field. Stored
// normalised (lowercase, no punctuation) because they are matched after
// the punctuation and case passes have run.
var honorifics = map[string]bool{
	"mr": true, "mrs": true, "ms": true, "miss": true,
	"dr": true, "prof": true,
	"smt": true, "shri": true, "sri": true, "kum": true,
	"master": true, "baby": true,
}

// nameExpansions unify the abbreviations and spelling variants that
// cause duplicate records. Each entry here is one test case in
// normalize_test.go — the map and the corpus are meant to stay in step.
//
// This is a ruleset, not an algorithm: it is enumerable, reviewable, and
// every entry is justified by a failure mode named in #70. Resist adding
// speculative entries; an expansion that fires on a name it should not
// have touched merges two people.
var nameExpansions = map[string]string{
	"md":       "mohammed",
	"mohd":     "mohammed",
	"mohammad": "mohammed",
	"muhammad": "mohammed",
	"mohamed":  "mohammed",
}

// NormalizeName reduces a written name to a comparable form: lowercase,
// punctuation removed, whitespace collapsed, honorifics dropped, known
// abbreviations expanded.
//
// It is idempotent by construction — every rule maps into the normalised
// alphabet, so a second pass finds nothing left to change. TestNormalizeNameIsIdempotent
// is what keeps that true as rules are added.
func NormalizeName(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsSpace(r) || unicode.IsPunct(r):
			b.WriteRune(' ')
		}
	}

	out := make([]string, 0, 4)
	for _, tok := range strings.Fields(b.String()) {
		if honorifics[tok] {
			continue
		}
		if exp, ok := nameExpansions[tok]; ok {
			tok = exp
		}
		out = append(out, tok)
	}
	return strings.Join(out, " ")
}

// PhoneticKey encodes a name for sounds-alike comparison.
//
// Soundex is English-centric and does poorly on raw Indian
// transliterations — which is exactly why NormalizeName runs first. The
// normalisation collapses the Indian-specific variance, so Soundex here
// encodes a regularised form rather than the raw input. The ruleset does
// the domain work; the encoder only has to be stable.
//
// Per-token rather than whole-string so that a differing middle name or
// a reordered given/family name does not change every code.
func PhoneticKey(raw string) string {
	toks := strings.Fields(NormalizeName(raw))
	codes := make([]string, 0, len(toks))
	for _, t := range toks {
		codes = append(codes, smetrics.Soundex(t))
	}
	return strings.Join(codes, " ")
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/modules/patientmaster/matching/ -v`
Expected: PASS.

- [ ] **Step 6: Prove the corpus can fail**

Delete the `"md": "mohammed"` entry from `nameExpansions`, re-run, and confirm both `TestNormalizeName/expands_Md` and `TestPhoneticKeyMatchesAcrossSpellingVariants` FAIL. Revert and confirm green.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/modules/patientmaster/matching backend/go.mod backend/go.sum
git commit -m "feat(70): normalise Indian name variants for patient matching"
```

---

### Task 3: Scoring and the three bands

**Files:**
- Create: `backend/internal/modules/patientmaster/matching/score.go`
- Test: `backend/internal/modules/patientmaster/matching/score_test.go`

**Interfaces:**
- Consumes: `NormalizeName`, `PhoneticKey` (Task 2).
- Produces:

```go
type Band int
const (BandNoMatch Band = iota; BandPossible; BandConfident)
type Candidate struct { PatientID, Name, DOB, Mobile, ABHANumber string }
type Thresholds struct { Possible, Confident float64 }
func DefaultThresholds() Thresholds
type Result struct { Candidate Candidate; Score float64; Band Band; Deterministic bool }
func Score(subject, candidate Candidate, t Thresholds) Result
func Rank(subject Candidate, corpus []Candidate, t Thresholds) []Result
```

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/modules/patientmaster/matching/score_test.go`:

```go
package matching_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/patientmaster/matching"
)

func subject() matching.Candidate {
	return matching.Candidate{Name: "Md Ali", DOB: "1979-04-02", Mobile: "9876543210"}
}

// TestABHAMatchIsConfidentAndDeterministic: a matching ABHA number is
// the strongest anchor there is. It must not depend on demographics
// agreeing — a corrected date of birth on the same ABHA is still the
// same person.
func TestABHAMatchIsConfidentAndDeterministic(t *testing.T) {
	s := subject()
	s.ABHANumber = "12345678901234"
	c := matching.Candidate{PatientID: "p1", Name: "Totally Different", DOB: "1955-01-01",
		ABHANumber: "12345678901234"}

	got := matching.Score(s, c, matching.DefaultThresholds())
	require.Equal(t, matching.BandConfident, got.Band)
	require.True(t, got.Deterministic)
}

// TestSpellingVariantSameDOBAndMobileIsConfident is the case #70 was
// written about: the same human registering twice with the name spelled
// differently.
func TestSpellingVariantSameDOBAndMobileIsConfident(t *testing.T) {
	c := matching.Candidate{PatientID: "p1", Name: "Mohammed Ali",
		DOB: "1979-04-02", Mobile: "9876543210"}

	got := matching.Score(subject(), c, matching.DefaultThresholds())
	require.Equal(t, matching.BandConfident, got.Band)
}

// TestSharedHouseholdMobileWithDifferentNameIsNotConfident: one mobile
// serves a whole family. Mobile alone must never reach the blocking
// band, or a wife is blocked from registering because her husband
// already exists.
func TestSharedHouseholdMobileWithDifferentNameIsNotConfident(t *testing.T) {
	c := matching.Candidate{PatientID: "p1", Name: "Fatima Begum",
		DOB: "1985-11-20", Mobile: "9876543210"}

	got := matching.Score(subject(), c, matching.DefaultThresholds())
	require.NotEqual(t, matching.BandConfident, got.Band)
}

// TestDifferentPersonIsNoMatch guards the false-merge direction, which
// is the one that causes clinical harm.
func TestDifferentPersonIsNoMatch(t *testing.T) {
	c := matching.Candidate{PatientID: "p1", Name: "Priya Nair",
		DOB: "1996-08-14", Mobile: "9000000000"}

	got := matching.Score(subject(), c, matching.DefaultThresholds())
	require.Equal(t, matching.BandNoMatch, got.Band)
}

// TestSameNameDifferentDOBIsPossibleNotConfident: common names collide.
// Blocking on the name alone would stop unrelated people registering.
func TestSameNameDifferentDOBIsPossibleNotConfident(t *testing.T) {
	c := matching.Candidate{PatientID: "p1", Name: "Mohammed Ali",
		DOB: "1962-01-01", Mobile: "9111111111"}

	got := matching.Score(subject(), c, matching.DefaultThresholds())
	require.NotEqual(t, matching.BandConfident, got.Band)
}

// TestRankOrdersByScoreDescending — the clerk sees the best candidate first.
func TestRankOrdersByScoreDescending(t *testing.T) {
	corpus := []matching.Candidate{
		{PatientID: "far", Name: "Priya Nair", DOB: "1996-08-14"},
		{PatientID: "near", Name: "Mohammed Ali", DOB: "1979-04-02", Mobile: "9876543210"},
	}
	got := matching.Rank(subject(), corpus, matching.DefaultThresholds())
	require.Len(t, got, 2)
	require.Equal(t, "near", got[0].Candidate.PatientID)
	require.GreaterOrEqual(t, got[0].Score, got[1].Score)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/modules/patientmaster/matching/ -run 'Test(ABHA|Spelling|Shared|Different|SameName|Rank)' -v`
Expected: compile failure — `undefined: matching.Candidate`.

- [ ] **Step 3: Implement**

Create `backend/internal/modules/patientmaster/matching/score.go`:

```go
package matching

import (
	"sort"

	"github.com/xrash/smetrics"
)

// Band is the outcome of comparing two people, and the input to the
// registration policy: Confident blocks, Possible offers candidates,
// NoMatch proceeds.
type Band int

const (
	BandNoMatch Band = iota
	BandPossible
	BandConfident
)

// Candidate is the comparable projection of a person. Deliberately flat
// strings: this package must be runnable over a corpus fixture with no
// database types in scope.
type Candidate struct {
	PatientID  string
	Name       string
	DOB        string // ISO date; compared for equality only
	Mobile     string
	ABHANumber string
}

// Thresholds are configuration, not constants. They will be tuned
// against real data, and a threshold that needs a code change to tune
// does not get tuned.
type Thresholds struct {
	Possible  float64
	Confident float64
}

// DefaultThresholds are a starting point, not a tuned answer. The spec
// records that no real corpus exists yet; these values are chosen so the
// documented cases land in the right bands and nothing more is claimed.
func DefaultThresholds() Thresholds {
	return Thresholds{Possible: 0.55, Confident: 0.90}
}

// Result is one comparison.
type Result struct {
	Candidate     Candidate
	Score         float64
	Band          Band
	Deterministic bool
}

// jaroWinklerBoost/prefix are smetrics' standard parameters: boost
// strings sharing a prefix of up to 4 once similarity passes 0.7.
const (
	jaroWinklerBoost  = 0.7
	jaroWinklerPrefix = 4
)

// Component weights. Name dominates because it is the only field that is
// both always present and genuinely discriminating: dates of birth are
// approximated to 1 January, and one mobile serves a household — so
// neither can carry the decision alone. See the shared-household test.
const (
	weightName     = 0.60
	weightDOB      = 0.25
	weightMobile   = 0.15
	phoneticCredit = 0.85 // similarity attributed when phonetic keys agree
)

// Score compares two people.
//
// A matching ABHA number short-circuits to Confident regardless of
// demographics: it is a verified national identifier, and a corrected
// date of birth against the same ABHA is the same human. Everything else
// is scored.
func Score(subject, candidate Candidate, t Thresholds) Result {
	if subject.ABHANumber != "" && subject.ABHANumber == candidate.ABHANumber {
		return Result{Candidate: candidate, Score: 1, Band: BandConfident, Deterministic: true}
	}

	nameSim := smetrics.JaroWinkler(
		NormalizeName(subject.Name), NormalizeName(candidate.Name),
		jaroWinklerBoost, jaroWinklerPrefix)
	// Sounds-alike names that survive normalisation still count, but at a
	// discount — a phonetic collision is weaker evidence than a spelling
	// match, and treating them equally is how unrelated people merge.
	if nameSim < phoneticCredit && PhoneticKey(subject.Name) == PhoneticKey(candidate.Name) {
		nameSim = phoneticCredit
	}

	var dobSim float64
	if subject.DOB != "" && subject.DOB == candidate.DOB {
		dobSim = 1
	}
	var mobileSim float64
	if subject.Mobile != "" && subject.Mobile == candidate.Mobile {
		mobileSim = 1
	}

	score := nameSim*weightName + dobSim*weightDOB + mobileSim*weightMobile

	band := BandNoMatch
	switch {
	case score >= t.Confident:
		band = BandConfident
	case score >= t.Possible:
		band = BandPossible
	}
	return Result{Candidate: candidate, Score: score, Band: band}
}

// Rank scores a corpus and returns it best-first, so the clerk sees the
// most likely existing record at the top.
func Rank(subject Candidate, corpus []Candidate, t Thresholds) []Result {
	out := make([]Result, 0, len(corpus))
	for _, c := range corpus {
		out = append(out, Score(subject, c, t))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/modules/patientmaster/matching/ -v`
Expected: PASS. If a documented case lands in the wrong band, adjust the WEIGHTS or thresholds until every case in the file passes — do not change a test to match the implementation. If they cannot all be satisfied at once, stop and report it: that is a real finding about the model, not a tuning problem.

- [ ] **Step 5: Prove the false-merge guard can fail**

Set `weightMobile = 0.60` and `weightName = 0.15`, re-run, and confirm `TestSharedHouseholdMobileWithDifferentNameIsNotConfident` FAILS — proving that test genuinely defends against mobile dominating. Revert and confirm green.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/modules/patientmaster/matching
git commit -m "feat(70): score patient candidates into block, review and no-match bands"
```

---

### Task 4: The module, its tables, and tenant isolation

**Files:**
- Create (via generator): `backend/internal/modules/patientmaster/module.go`
- Modify: `backend/internal/bootstrap/modules.go` (or wherever `bootstrap.NewRegistry` lists modules)
- Modify: `backend/internal/archtest/arch_test.go` (`allModules()`)
- Test: `backend/internal/modules/patientmaster/module_test.go`

**Interfaces:**
- Consumes: `authz.RoleReceptionist` (Task 1).
- Produces: `patientmaster.New() *Module`; permissions `PermPatientRead authz.Permission = "patient.read"`, `PermPatientRegister authz.Permission = "patient.register"`; tables `patients`, `patient_identifiers`, `patient_consents`, `patient_duplicate_overrides`.

- [ ] **Step 1: Generate the module skeleton**

```bash
make new-module NAME=patientmaster
```

Read what it generated before editing — it produces the whole `platform.Module` surface, and the rest of this task fills it in rather than replacing it.

- [ ] **Step 2: Write the failing test**

Create `backend/internal/modules/patientmaster/module_test.go`, mirroring the harness setup used by `internal/modules/lab/module_test.go`:

```go
// TestPatientsAreTenantIsolated is the adversarial RLS check: a row
// written by one tenant must be invisible to another, enforced by the
// database rather than by a WHERE clause a handler might forget.
func TestPatientsAreTenantIsolated(t *testing.T) {
	// ... build the harness exactly as lab/module_test.go does ...
	tenantA, tenantB := uuid.NewString(), uuid.NewString()

	require.NoError(t, h.DB.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patients (tenant_id, mrn, given_name, dob, dob_estimated, mobile)
			VALUES (?, 'MRN-1', 'suresh', '1979-04-02', false, '9876543210')`, tenantA).Error
	}))

	countAs := func(tenant string) int {
		var n int
		require.NoError(t, h.DB.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM patients`).Scan(&n).Error
		}))
		return n
	}
	require.Equal(t, 1, countAs(tenantA))
	require.Equal(t, 0, countAs(tenantB), "tenant B can read tenant A's patients")
}

// TestPatientWriteIsPinnedToOneTenant proves WITH CHECK rejects a row
// stamped for a different tenant than the transaction's.
func TestPatientWriteIsPinnedToOneTenant(t *testing.T) {
	// ... same harness ...
	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	err := h.DB.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patients (tenant_id, mrn, given_name, dob, dob_estimated)
			VALUES (?, 'MRN-2', 'priya', '1996-08-14', false)`, tenantB).Error
	})
	require.Error(t, err)
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd backend && go test ./internal/modules/patientmaster/ -v`
Expected: FAIL — `relation "patients" does not exist`.

- [ ] **Step 4: Write the migration**

In `backend/internal/modules/patientmaster/module.go`, `Migrations()`:

```go
	return []tenantdb.Migration{{
		ID: "0001_patientmaster",
		SQL: `
			CREATE TABLE patients (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  mrn text NOT NULL,
			  given_name text NOT NULL,
			  family_name text NOT NULL DEFAULT '',
			  -- Normalised at write time so matching never re-derives it
			  -- per query, and so the index below is over the compared form.
			  normalized_name text NOT NULL DEFAULT '',
			  phonetic_key text NOT NULL DEFAULT '',
			  sex text NOT NULL DEFAULT '' CHECK (sex IN ('', 'male', 'female', 'other')),
			  dob date NOT NULL,
			  -- Dates of birth are routinely approximated to 1 January.
			  -- Recording that the date is an estimate is what stops the
			  -- matcher treating a guess as evidence.
			  dob_estimated boolean NOT NULL DEFAULT false,
			  mobile text NOT NULL DEFAULT '',
			  address_line text NOT NULL DEFAULT '',
			  -- Aadhaar: masked tail and a keyed hash ONLY. The raw value is
			  -- never written here, to logs, or to any event (spec D2).
			  aadhaar_last4 char(4),
			  aadhaar_hash text,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  updated_at timestamptz NOT NULL DEFAULT now(),
			  UNIQUE (tenant_id, mrn)
			);
			ALTER TABLE patients ENABLE ROW LEVEL SECURITY;
			ALTER TABLE patients FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON patients
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON patients (tenant_id, created_at DESC);
			CREATE INDEX ON patients (tenant_id, phonetic_key);
			CREATE INDEX ON patients (tenant_id, mobile);

			-- Identifiers get their own table so ABHA linkage (#74) adds
			-- rows rather than a migration on patients.
			CREATE TABLE patient_identifiers (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  patient_id uuid NOT NULL REFERENCES patients(id),
			  kind text NOT NULL CHECK (kind IN ('abha_number','abha_address','mrn_external')),
			  value text NOT NULL,
			  verified_at timestamptz,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  UNIQUE (tenant_id, kind, value)
			);
			ALTER TABLE patient_identifiers ENABLE ROW LEVEL SECURITY;
			ALTER TABLE patient_identifiers FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON patient_identifiers
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON patient_identifiers (tenant_id, patient_id);

			-- Registration is the first point of processing under DPDP, so
			-- this row is written in the SAME transaction as the patient
			-- (spec D5). A patient without a receipt is unreachable, not
			-- merely discouraged.
			CREATE TABLE patient_consents (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  patient_id uuid NOT NULL REFERENCES patients(id),
			  notice_version text NOT NULL,
			  consented_by text NOT NULL CHECK (consented_by IN ('patient','guardian')),
			  guardian_name text NOT NULL DEFAULT '',
			  guardian_relationship text NOT NULL DEFAULT '',
			  recorded_by_subject text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE patient_consents ENABLE ROW LEVEL SECURITY;
			ALTER TABLE patient_consents FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON patient_consents
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON patient_consents (tenant_id, patient_id);

			-- Merge is out of slice. This table is what makes that honest:
			-- every duplicate deliberately created is recorded with who did
			-- it and why, and it becomes merge's worklist when merge ships.
			CREATE TABLE patient_duplicate_overrides (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  created_patient_id uuid NOT NULL REFERENCES patients(id),
			  matched_patient_id uuid NOT NULL REFERENCES patients(id),
			  score numeric(4,3) NOT NULL,
			  reason text NOT NULL,
			  actor_subject text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE patient_duplicate_overrides ENABLE ROW LEVEL SECURITY;
			ALTER TABLE patient_duplicate_overrides FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON patient_duplicate_overrides
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON patient_duplicate_overrides (tenant_id, created_at DESC);`,
	}}
```

- [ ] **Step 5: Declare permissions**

```go
const (
	PermPatientRead     authz.Permission = "patient.read"
	PermPatientRegister authz.Permission = "patient.register"
)

// Permissions: the receptionist registers and reads; clinical staff read.
// RoleTenantAdmin is never listed — the reconciler grants it everything.
func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermPatientRead, Roles: []authz.Role{
			authz.RoleReceptionist, authz.RoleDoctor, authz.RoleNurse,
			authz.RolePharmacist, authz.RoleLabTech}},
		{Permission: PermPatientRegister, Roles: []authz.Role{authz.RoleReceptionist}},
	}
}
```

- [ ] **Step 6: Register the module**

Add `patientmaster.New()` to the module list in `backend/internal/bootstrap/modules.go` and to `allModules()` in `backend/internal/archtest/arch_test.go`.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/modules/patientmaster/ -v && go test ./internal/archtest/`
Expected: PASS. `LintRLS` runs in the harness, so a policy of the wrong shape fails here.

- [ ] **Step 8: Prove the isolation test can fail**

In the `0001_patientmaster` migration, temporarily change `patients`' `USING` clause to `USING (true)`, re-run `TestPatientsAreTenantIsolated` against a fresh database, and confirm it FAILS on the tenant-B count. Revert.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/modules/patientmaster backend/internal/bootstrap backend/internal/archtest
git commit -m "feat(70): add the patientmaster module with tenant-isolated patient tables"
```

---

### Task 5: Registration — block, offer candidates, or override

**Files:**
- Create: `backend/internal/modules/patientmaster/patients.go`
- Modify: `backend/internal/modules/patientmaster/module.go` (`Routes`)
- Test: `backend/internal/modules/patientmaster/patients_test.go`

**Interfaces:**
- Consumes: `matching.Candidate`, `matching.Rank`, `matching.BandConfident`, `matching.DefaultThresholds`, `NormalizeName`, `PhoneticKey` (Tasks 2-3); `PermPatientRegister`, `PermPatientRead` (Task 4).
- Produces: `POST /v1/patients`, `GET /v1/patients/:id`, `GET /v1/patients` (keyset list).

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/modules/patientmaster/patients_test.go`, using the same harness as Task 2's module test:

```go
// TestRegisterCreatesPatientAndConsentInOneTransaction — spec D5. The
// two rows are inseparable; a patient without a consent receipt is a
// compliance defect, so this asserts both exist after one call.
func TestRegisterCreatesPatientAndConsentInOneTransaction(t *testing.T) {
	// POST a valid registration, expect 201, then assert exactly one
	// patients row AND exactly one patient_consents row for it.
}

// TestConfidentDuplicateIsBlocked: registering the same human twice with
// the name spelled differently must be refused with 409 and the existing
// candidate returned — not silently created.
func TestConfidentDuplicateIsBlocked(t *testing.T) {
	// Register "Mohammed Ali", 1979-04-02, 9876543210 -> 201.
	// Register "Md Ali", same DOB and mobile, no override -> 409,
	// body names the existing patient id.
	// Assert the patients table still holds exactly one row.
}

// TestOverrideCreatesTheDuplicateAndRecordsWhy: the escape hatch, and
// the audit row that makes deferring merge honest.
func TestOverrideCreatesTheDuplicateAndRecordsWhy(t *testing.T) {
	// Same as above but with override_reason set -> 201.
	// Assert two patients rows, and exactly one patient_duplicate_overrides
	// row naming both ids, the score, the reason and the actor subject.
}

// TestOverrideWithoutAReasonIsRefused: the reason IS the control. An
// override with an empty reason produces an audit row that proves
// nothing.
func TestOverrideWithoutAReasonIsRefused(t *testing.T) {
	// override=true, override_reason="" -> 400, no patient created.
}

// TestPossibleMatchDoesNotBlock: a same-name different-DOB pair must
// register freely. Blocking here stops unrelated people with common
// names from being registered at all.
func TestPossibleMatchDoesNotBlock(t *testing.T) {
	// Register "Mohammed Ali" 1979-04-02; then "Mohammed Ali" 1962-01-01
	// with no override -> 201, two rows.
}

// TestGetPatientFromAnotherTenantIs404: cross-tenant probes must not
// distinguish "not yours" from "does not exist".
func TestGetPatientFromAnotherTenantIs404(t *testing.T) {
	// Register in tenant A, GET the id as tenant B -> 404.
}
```

Fill each body using the HTTP-level helpers `internal/modules/lab/module_test.go` already uses for authenticated requests — do not invent a new request helper.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/modules/patientmaster/ -run TestRegister -v`
Expected: FAIL — route not mounted, 404.

- [ ] **Step 3: Implement the handlers**

Create `backend/internal/modules/patientmaster/patients.go`. Shape:

```go
type registerRequest struct {
	GivenName      string `json:"given_name" binding:"required,max=100"`
	FamilyName     string `json:"family_name" binding:"max=100"`
	Sex            string `json:"sex" binding:"omitempty,oneof=male female other"`
	DOB            string `json:"dob" binding:"required,datetime=2006-01-02"`
	DOBEstimated   bool   `json:"dob_estimated"`
	Mobile         string `json:"mobile" binding:"omitempty,len=10,numeric"`
	AddressLine    string `json:"address_line" binding:"max=300"`
	NoticeVersion  string `json:"notice_version" binding:"required,max=40"`
	ConsentedBy    string `json:"consented_by" binding:"required,oneof=patient guardian"`
	GuardianName   string `json:"guardian_name" binding:"max=100"`
	GuardianRel    string `json:"guardian_relationship" binding:"max=40"`
	Override       bool   `json:"override"`
	OverrideReason string `json:"override_reason" binding:"max=300"`
}
```

`register` does, all inside ONE `db.WithTenant` transaction:

1. Bind and validate. If `Override` is true and `OverrideReason` is blank → `respond.BadRequest`. The reason is the control; an override without one produces an audit row that proves nothing.
2. Load the candidate corpus for this tenant — patients sharing the phonetic key OR the mobile. Both indexes exist for this query. **Never load the whole table**; that is a full scan on the busiest endpoint in the hospital.
3. `matching.Rank(subject, corpus, matching.DefaultThresholds())`.
4. If the top result is `BandConfident` and `!Override` → 409 carrying the matched patient id and score. No row is written.
5. Insert the patient with `normalized_name` and `phonetic_key` computed at write time.
6. Insert the consent row **in the same transaction** (D5).
7. If overriding a confident match, insert the `patient_duplicate_overrides` row with both ids, the score, the reason and `principal.Subject`.
8. Publish `patient.registered` — Task 6 adds this; leave the publish out until then rather than stubbing it.

`get` loads by id under `WithTenant` and maps `gorm.ErrRecordNotFound` to `respond.NotFound` — RLS filters cross-tenant rows, so the 404 is produced by the same path that produces a genuine miss.

Mount in `Routes`:

```go
	g := r.Group("/patients")
	patients := &patientHandlers{db: deps.DB, bus: deps.Bus}
	g.POST("", PermPatientRegister, patients.register)
	g.GET("/:id", PermPatientRead, patients.get)
	platform.ListRoute(g, "", PermPatientRead, patients.list)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/modules/patientmaster/ -v`
Expected: PASS.

- [ ] **Step 5: Prove consent atomicity by mutation**

Temporarily make the consent insert fail (an invalid `consented_by` value), re-run `TestRegisterCreatesPatientAndConsentInOneTransaction`, and confirm **no patient row survives** — not merely that the call errored. If a patient row remains, the two writes are not in one transaction and that is the bug this step exists to catch. Revert.

- [ ] **Step 6: Prove the block can fail**

Change the confident check to `if false && ...`, re-run `TestConfidentDuplicateIsBlocked`, and confirm it FAILS. Revert.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/modules/patientmaster
git commit -m "feat(70): register patients, blocking confident duplicates with an attributable override"
```

---

### Task 6: Events, carrying no PHI

**Files:**
- Create: `backend/internal/modules/patientmaster/contract/events.go`
- Modify: `backend/internal/modules/patientmaster/module.go` (`Publishes`), `patients.go` (publish)
- Modify: `backend/internal/archtest/event_payload_test.go`
- Test: `backend/internal/modules/patientmaster/patients_test.go`

**Interfaces:**
- Consumes: Task 5's `register`.
- Produces: `patientmastercontract.SubjectPatientRegistered = "helivanta.in.patientmaster.registered.v1"`, `PatientRegisteredData{PatientID string}`.

- [ ] **Step 1: Write the failing test**

Append to `patients_test.go`:

```go
// TestRegisteredEventCarriesOnlyTheID is spec D6 asserted on the wire.
// #835 was PHI leaving the RLS boundary in event payloads; this fails
// the day someone adds a name "just for the work queue".
func TestRegisteredEventCarriesOnlyTheID(t *testing.T) {
	// Register a patient whose given_name is a distinctive sentinel.
	// Read the outbox_events payload for the registered subject and
	// assert it contains the patient id and does NOT contain the sentinel.
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./internal/modules/patientmaster/ -run TestRegisteredEvent -v`
Expected: FAIL — no such subject in the outbox.

- [ ] **Step 3: Write the contract**

Create `backend/internal/modules/patientmaster/contract/events.go`:

```go
// Package contract is patientmaster's published event interface. See
// medicore/contract for why this package exists and why it is data only.
package contract

// SubjectPatientRegistered is published when a patient is registered.
const SubjectPatientRegistered = "helivanta.in.patientmaster.registered.v1"

// PatientRegisteredData is the v1 payload.
//
// It carries the id and NOTHING identifying — no name, date of birth,
// mobile or address. #835 was PHI leaving the RLS boundary in event
// payloads; a consumer that needs demographics calls the API under its
// own permission, where authorization is enforced per request. Adding a
// demographic field here would put clinical data in every replica's
// outbox and in the JetStream retention window.
type PatientRegisteredData struct {
	PatientID string `json:"patient_id"`
}
```

- [ ] **Step 4: Publish it, and declare it**

In `patients.go`'s `register`, inside the same transaction, after the consent insert:

```go
		data, err := json.Marshal(patientmastercontract.PatientRegisteredData{
			PatientID: row.ID.String(),
		})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, patientmastercontract.SubjectPatientRegistered, events.Event{
			Type: "PatientRegistered", Version: 1, TenantID: p.TenantID, Data: data,
		})
```

In `module.go`:

```go
func (m *Module) Publishes() []string {
	return []string{patientmastercontract.SubjectPatientRegistered}
}
```

- [ ] **Step 5: Classify the field**

In `backend/internal/archtest/event_payload_test.go`, add to `eventPayloadReviewedNonPHI`:

```go
	"patientmaster.PatientRegisteredData.PatientID": "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/modules/patientmaster/ ./internal/archtest/ -v`
Expected: PASS.

- [ ] **Step 7: Prove the PHI test can fail**

Temporarily add `GivenName string \`json:"given_name"\`` to `PatientRegisteredData` and populate it. Confirm TWO things fail: `TestRegisteredEventCarriesOnlyTheID`, and the archtest payload classifier (the new field is unclassified). Revert both.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/modules/patientmaster backend/internal/archtest
git commit -m "feat(70): publish patient.registered carrying only the patient id"
```

---

### Task 7: `visit_created` gains `patient_id`, additively

**Files:**
- Modify: `backend/internal/modules/medicore/contract/events.go`
- Modify: `backend/internal/modules/medicore/visits.go`
- Modify: `backend/internal/archtest/event_payload_test.go`
- Test: `backend/internal/modules/medicore/module_test.go`

**Interfaces:**
- Consumes: nothing from patientmaster (modules never import modules) — the visit carries an id the caller supplies.
- Produces: `medicorecontract.VisitCreatedData.PatientID string`.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/modules/medicore/module_test.go`:

```go
// TestVisitCreatedCarriesPatientID: the additive half of spec D7.
// patient_name stays for now so pharmacy and lab keep working; this
// proves the new field is populated and on the wire.
func TestVisitCreatedCarriesPatientID(t *testing.T) {
	// Create a visit with patient_id set; read the outbox payload for
	// SubjectVisitCreated and assert patient_id round-trips.
}

// TestVisitCreatedStillCarriesPatientName is the non-breaking half. It
// deletes itself when patient_name retires in a later contract version —
// until then, removing the field must fail here rather than in pharmacy.
func TestVisitCreatedStillCarriesPatientName(t *testing.T) {
	// Same visit; assert patient_name is unchanged.
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd backend && go test ./internal/modules/medicore/ -run TestVisitCreated -v`
Expected: `TestVisitCreatedCarriesPatientID` fails — no such field.

- [ ] **Step 3: Extend the contract additively**

In `backend/internal/modules/medicore/contract/events.go`, add to `VisitCreatedData`:

```go
	// PatientID references the patientmaster record. Added ALONGSIDE
	// PatientName rather than replacing it (spec D7): renaming or
	// removing a field here breaks every consumer's build on purpose, so
	// pharmacy and lab adopt this on their own schedules and PatientName
	// retires in a later version once nothing reads it.
	PatientID string `json:"patient_id"`
```

- [ ] **Step 4: Populate it**

In `backend/internal/modules/medicore/visits.go`: add `PatientID string \`json:"patient_id" binding:"omitempty,uuid"\`` to `createVisitRequest`, store it on the visit row (migration `0003_medicore` adding a nullable `patient_id uuid` column — append-only, do not edit `0001` or `0002`), and set it on the published payload.

`omitempty` because visits opened before patients exist must keep working; this is the transition D7 describes, not a hard cutover.

- [ ] **Step 5: Classify the field**

Add to `eventPayloadReviewedNonPHI` in `backend/internal/archtest/event_payload_test.go`:

```go
	"medicore.VisitCreatedData.PatientID": "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/modules/medicore/ ./internal/modules/pharmacy/ ./internal/modules/lab/ ./internal/archtest/`
Expected: PASS. Pharmacy and lab must pass **unchanged** — that is the proof the change is additive.

- [ ] **Step 7: Prove additivity**

Confirm no file under `internal/modules/pharmacy/` or `internal/modules/lab/` appears in `git diff --stat` for this task. If either changed, the change was not additive.

- [ ] **Step 8: Run the full gate**

```bash
cd backend && go build ./... && go test -race ./...
cd .. && make lint-go
cd backend && ./scripts/coverage-gate.sh
```

- [ ] **Step 9: Commit and push**

```bash
git add backend/internal/modules/medicore backend/internal/archtest
git commit -m "feat(70): carry patient_id on visit_created alongside patient_name"
git push -u origin feat/70-patient-master
```

PR body must include `Closes #70`, reference #36, and state what the slice does not cover: merge/unmerge and the stewardship queue, ABHA linkage, self-registration, longitudinal history, address-locality matching, console screens, the FHIR projection, and pharmacy/lab still reading `patient_name`.

---

## Self-Review

**Spec coverage:** D1 → Task 4 (module beside iam, registered in bootstrap and archtest). D2 → Task 4's migration (UUIDv4 via `gen_random_uuid()`, tenant-scoped `UNIQUE (tenant_id, mrn)`, `aadhaar_last4`/`aadhaar_hash` with no raw column, separate `patient_identifiers`). D3 → Tasks 2 and 3 (pure package, deterministic ABHA short-circuit, Jaro-Winkler + phonetic, no address locality, thresholds as config). D4 → Task 5 (409 block, candidate ranking, override with mandatory reason, `patient_duplicate_overrides`). D5 → Task 4's table and Task 5's one-transaction test plus its atomicity mutation. D6 → Task 6 (id-only payload, PHI classification, mutation proof). D7 → Task 7 (additive field, pharmacy/lab unchanged). D8 → Task 2 (smetrics dependency; `Soundex` over the normalised form, not Metaphone).

**A spec gap this plan closes:** the spec's actor is a registration clerk and no such role existed. Task 1 adds `RoleReceptionist`. This was not in the approved spec — flagged in the handoff.

**Testing-section coverage:** corpus tests both directions → Tasks 2 and 3 (including the must-NOT-match cases and the shared-household-mobile case). Band behaviour → Task 5. Consent atomicity by forced failure → Task 5 Step 5. Adversarial RLS → Task 4 Steps 2 and 8. No PHI on the bus → Task 6 Step 7.

**Type consistency:** `NormalizeName`, `PhoneticKey`, `Candidate`, `Thresholds`, `DefaultThresholds`, `Result`, `Band`/`BandNoMatch`/`BandPossible`/`BandConfident`, `Score`, `Rank`, `PermPatientRead`, `PermPatientRegister`, `RoleReceptionist`, `SubjectPatientRegistered`, `PatientRegisteredData.PatientID`, `VisitCreatedData.PatientID` — each spelled identically at every appearance.

**Known plan-time uncertainty, to resolve in execution rather than guess now:** the exact name of the exported accessor over `authz.systemRoles` (Task 1 Step 1 says to use whatever it is), the exact file listing modules for `bootstrap.NewRegistry` (Task 4 Step 6), and the harness helpers in `lab/module_test.go` that Tasks 4-5 mirror rather than reinvent. Thresholds in Task 3 are a starting point: Step 4 says to tune weights until every documented case passes, and to stop and report if they cannot all be satisfied at once.
