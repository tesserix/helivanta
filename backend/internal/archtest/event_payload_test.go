package archtest

import (
	"go/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// eventPayloadPHIAllowlist is the positive allowlist from
// docs/superpowers/specs/2026-08-14-event-transport-isolation-design.md
// (D4): every payload field, across every contract package, that is
// known to carry clinical or patient-identifying data, keyed
// "<module>.<Type>.<Field>" with the reason it has to be on the wire.
//
// This is deliberately a positive allowlist, not a name-pattern
// denylist. A denylist guesses at names ("Patient*", "*Name"), which
// fails on exactly the field the product needs (this one) and misses
// anything it didn't think to guess — "notes", "reason_for_visit", a
// DOB stored as a timestamp. Every field this test finds is presumed
// PHI-shaped until a human puts it in one of the two maps below; adding
// an entry is therefore a diff a reviewer sees and must justify, not a
// pattern match that can silently start admitting a new field.
var eventPayloadPHIAllowlist = map[string]string{
	"medicore.VisitCreatedData.PatientName": "pharmacy and lab display it on their work queues; modules may not import modules, so there is no cross-module read path to fetch it instead",
}

// eventPayloadReviewedNonPHI is the second half of the same decision:
// fields a human has confirmed do NOT carry PHI or identifying data,
// despite not being auto-recognised as an identifier (see
// isAutoSafeIdentifierField). It exists so the classifier does not have
// to guess at "enum-shaped" or "operational" strings by type or name
// pattern — Go's type system cannot tell RoleKey from PatientName, both
// are plain `string` fields, so the distinction is recorded here
// instead of inferred.
var eventPayloadReviewedNonPHI = map[string]string{
	"iam.MemberChangedData.Subject":        "the iam subject identifier (e.g. GIP subject claim), already known to every module via authn.Principal.Subject; a reference, not identifying content",
	"iam.CredentialRevokedData.Subject":    "same iam subject identifier as MemberChangedData.Subject",
	"iam.MemberChangedData.RoleKey":        "a key into the fixed role registry (docs/standards/backend.md), not clinical or identifying data",
	"medicore.VisitCreatedData.Department": "the ward/department the visit is opened in; operational routing metadata, not patient-identifying",
}

// isAutoSafeIdentifierField is the ONLY automatic (non-pinned)
// classification this test performs, and it is narrow on purpose: a
// field whose name ends in "ID" or "IDs" is, by this codebase's naming
// convention (VisitID, OrderID, DispenseID, PingID, ...), an opaque
// reference to a row elsewhere — not identifying content itself. It
// applies regardless of the field's Go type (string, uuid.UUID, a
// future int64) because the argument is about what the value MEANS
// (a pointer requiring its own authorized lookup), not how it is
// encoded.
//
// Nothing else is auto-classified. In particular this test does NOT
// treat time.Time, numeric, or bool fields as automatically safe: a
// date of birth is exactly as identifying stored as time.Time as it is
// stored as a string, and a medical record number is exactly as
// identifying as an int as it is as a string. Classifying by Go kind
// would silently wave through a differently-typed PHI field the day
// someone adds one — proved by mutation in the Task 3 report for #835
// (a time.Time DateOfBirth field, which a type-based classifier would
// have waved through as "just a timestamp").
func isAutoSafeIdentifierField(name string) bool {
	return strings.HasSuffix(name, "ID") || strings.HasSuffix(name, "IDs")
}

// loadContractPackagesWithTypes is loadContractPackages (contract_test.go)
// plus type information: this test needs to walk struct fields, which
// the AST-only mode the other contract tests use does not provide.
func loadContractPackagesWithTypes(t *testing.T) []*packages.Package {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps |
			packages.NeedSyntax | packages.NeedFiles | packages.NeedTypes | packages.NeedTypesInfo,
	}, modulesPrefix+"...")
	require.NoError(t, err)

	var out []*packages.Package
	for _, p := range pkgs {
		if strings.HasSuffix(p.PkgPath, "/contract") {
			out = append(out, p)
		}
	}
	require.NotEmpty(t, out, "no contract packages found — this test would pass vacuously")
	return out
}

// payloadField is one exported field of one exported struct type
// declared in a contract package, identified by its pinned-map key.
type payloadField struct {
	key   string // "<module>.<Type>.<Field>"
	field string // bare field name, for the auto-safe check
}

// findPayloadFields walks every exported struct type in every contract
// package and returns every exported field. Contract packages declare
// data only (TestContractPackagesDeclareOnlyData enforces this), so
// every exported struct here is a wire payload type — there is nothing
// else a contract package is allowed to contain.
func findPayloadFields(t *testing.T) []payloadField {
	t.Helper()
	var out []payloadField

	for _, p := range loadContractPackagesWithTypes(t) {
		require.NotNilf(t, p.Types, "%s: no type information loaded", p.PkgPath)
		module := contractModuleNameT(t, p.PkgPath)
		scope := p.Types.Scope()

		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*types.TypeName)
			if !ok || !tn.Exported() {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			st, ok := named.Underlying().(*types.Struct)
			if !ok {
				continue // not a struct (e.g. a type alias for a subject string, if one existed)
			}

			for i := 0; i < st.NumFields(); i++ {
				f := st.Field(i)
				if !f.Exported() {
					continue
				}
				out = append(out, payloadField{
					key:   module + "." + tn.Name() + "." + f.Name(),
					field: f.Name(),
				})
			}
		}
	}
	require.NotEmpty(t, out, "no payload fields found in any contract package — this test would pass vacuously")
	return out
}

func contractModuleNameT(t *testing.T, pkgPath string) string {
	t.Helper()
	parts := strings.Split(pkgPath, "/")
	require.GreaterOrEqual(t, len(parts), 2, "contract package path %q too short to derive a module name", pkgPath)
	return parts[len(parts)-2]
}

// TestEventPayloadPHIFieldsAreAllowlisted is the arch test from D4: every
// field of every event payload struct in every module's contract package
// must be either
//
//  1. auto-recognised as an opaque identifier (isAutoSafeIdentifierField), or
//  2. pinned in eventPayloadPHIAllowlist with the reason it must carry
//     PHI/identifying data, or
//  3. pinned in eventPayloadReviewedNonPHI with the reason it does not.
//
// A field found by reflection that matches none of the three is a field
// nobody has made a decision about — this test fails closed and names
// it, rather than guessing whether it is safe.
func TestEventPayloadPHIFieldsAreAllowlisted(t *testing.T) {
	fields := findPayloadFields(t)

	seen := map[string]bool{}
	for _, f := range fields {
		seen[f.key] = true

		if isAutoSafeIdentifierField(f.field) {
			// An identifier-shaped field cannot also be independently
			// pinned: if it is, the pin is dead weight and the field's
			// true classification is ambiguous to a reader.
			_, inPHI := eventPayloadPHIAllowlist[f.key]
			_, inSafe := eventPayloadReviewedNonPHI[f.key]
			require.Falsef(t, inPHI || inSafe,
				"%s is auto-classified as an identifier (ends in ID/IDs) AND separately pinned — remove one classification, it should have exactly one",
				f.key)
			continue
		}

		_, inPHI := eventPayloadPHIAllowlist[f.key]
		_, inSafe := eventPayloadReviewedNonPHI[f.key]
		require.Falsef(t, inPHI && inSafe,
			"%s is pinned in BOTH eventPayloadPHIAllowlist and eventPayloadReviewedNonPHI — it cannot be both", f.key)
		require.Truef(t, inPHI || inSafe,
			"%s is a new event payload field with no PHI classification: it does not end in ID/IDs, and is not pinned in eventPayloadPHIAllowlist or eventPayloadReviewedNonPHI in event_payload_test.go. "+
				"If it carries clinical or patient-identifying data, add it to eventPayloadPHIAllowlist with a reason it must be on the wire. "+
				"If it does not, add it to eventPayloadReviewedNonPHI with the reason.",
			f.key)
	}

	// The reverse direction: every pinned entry must name a field that
	// actually exists, the same way TestRateLimitPolicyRoutesAreRegistered
	// catches a typo'd route key instead of a silently-dead allowlist
	// entry that looks like coverage but checks nothing.
	for key, reason := range eventPayloadPHIAllowlist {
		require.Truef(t, seen[key], "eventPayloadPHIAllowlist entry %q (%s) does not match any field found in a contract package — check for a typo or a removed field", key, reason)
	}
	for key, reason := range eventPayloadReviewedNonPHI {
		require.Truef(t, seen[key], "eventPayloadReviewedNonPHI entry %q (%s) does not match any field found in a contract package — check for a typo or a removed field", key, reason)
	}
}

// TestEventPayloadPHIAllowlistIsPinnedExactly guards the security-relevant
// half of the classification (which fields carry PHI) against silent
// growth: if this test's expected literal and the map diverge, someone
// added or removed a PHI field without this line of the diff being the
// thing a reviewer's eye lands on.
func TestEventPayloadPHIAllowlistIsPinnedExactly(t *testing.T) {
	require.Equal(t, map[string]string{
		"medicore.VisitCreatedData.PatientName": "pharmacy and lab display it on their work queues; modules may not import modules, so there is no cross-module read path to fetch it instead",
	}, eventPayloadPHIAllowlist)
}
