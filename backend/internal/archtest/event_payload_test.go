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
// fields a human has confirmed do NOT carry PHI or identifying data.
// It exists so the classifier does not have
// to guess at "enum-shaped" or "operational" strings by type or name
// pattern — Go's type system cannot tell RoleKey from PatientName, both
// are plain `string` fields, so the distinction is recorded here
// instead of inferred.
var eventPayloadReviewedNonPHI = map[string]string{
	"supply.PremiseUpdatedData.PartyID":                      "opaque organisation scope; no legal identity or address",
	"supply.PremiseUpdatedData.PremiseID":                    "opaque premises reference; no location or contact data",
	"supply.PremiseUpdatedData.Version":                      "monotonic revision; no declared premises attributes",
	"supply.LicenseDeclaredData.PartyID":                     "opaque organisation scope; no business identity",
	"supply.LicenseDeclaredData.LicenseID":                   "opaque licence reference; no licence number, authority or evidence",
	"supply.LicenseDeclaredData.Version":                     "monotonic declaration revision; not proof of validity or approval",
	"supply.PartyUpdatedData.PartyID":                        "opaque private supplier reference; no legal identity, address, contacts or tax numbers",
	"supply.PartyUpdatedData.Version":                        "monotonic profile revision for invalidation; no business or personal attributes",
	"supply.PartyAccessChangedData.PartyID":                  "opaque profile scope; private lookup still requires a current explicit grant",
	"supply.PartyAccessChangedData.AccessID":                 "opaque access-record reference; no user identity or role is published",
	"supply.PartyAccessChangedData.Operation":                "bounded granted/revoked operation; no recipient or permission details",
	"pharmacy.PayableRecordedData.PharmacyID":                "opaque pharmacy scope; no payee identity or prices",
	"pharmacy.PayableRecordedData.OutletID":                  "opaque outlet scope; protected lookup required",
	"pharmacy.PayableRecordedData.PayableID":                 "opaque document identity; no financial amounts",
	"pharmacy.SettlementRecordedData.PharmacyID":             "opaque pharmacy scope; no financial details",
	"pharmacy.SettlementRecordedData.OutletID":               "opaque outlet scope; no hospital or patient identity",
	"pharmacy.SettlementRecordedData.PayableID":              "opaque payable identity for idempotent downstream posting",
	"pharmacy.SettlementRecordedData.SettlementID":           "opaque settlement identity; no payer, payment method, reference or amount",
	"pharmacy.CatalogueUpdatedData.PharmacyID":               "opaque owning pharmacy reference; does not confer tenant/outlet access",
	"pharmacy.StockMovementRecordedData.PharmacyID":          "opaque pharmacy ownership scope; no hospital or patient identity",
	"pharmacy.StockMovementRecordedData.OutletID":            "opaque outlet scope; no hospital demographics",
	"pharmacy.StockMovementRecordedData.MovementID":          "opaque quarantined-stock movement reference; no patient or financial details",
	"pharmacy.StockMovementRecordedData.LocationID":          "opaque inventory location reference; requires protected lookup",
	"pharmacy.StockMovementRecordedData.BatchID":             "opaque batch reference; no quantities, drug identity or expiry details",
	"pharmacy.CatalogueUpdatedData.OutletID":                 "opaque outlet scope; protected product lookup still requires a current outlet grant",
	"pharmacy.CatalogueUpdatedData.ProductID":                "opaque private-product reference; no patient, prescriber or commercial attributes",
	"pharmacy.CatalogueUpdatedData.Version":                  "monotonic catalogue revision for replay and invalidation",
	"pharmacy.CatalogueUpdatedData.Status":                   "allowlisted product lifecycle state; no clinical decision or patient data",
	"workforce.DocumentScanInput.TenantID":                   "internal tenancy scope for processing; no organization attributes",
	"workforce.DocumentScanInput.HospitalID":                 "opaque origin hospital scope; no facility attributes",
	"workforce.DocumentScanInput.StaffID":                    "opaque protected owner reference; no personnel facts",
	"workforce.DocumentScanInput.FileID":                     "opaque immutable intent reference; no filename or content",
	"workforce.DocumentScanInput.ScanAttempt":                "deterministic processing attempt; no personnel facts",
	"workforce.DocumentChangedData.HospitalID":               "opaque hospital scope; no facility attributes",
	"workforce.DocumentChangedData.StaffID":                  "opaque workforce owner reference; no employment or health attributes",
	"workforce.DocumentChangedData.FileID":                   "opaque immutable file reference; no filename or storage location",
	"workforce.DocumentChangedData.ScanAttempt":              "processing attempt for deduplication; no content or personnel facts",
	"workforce.DocumentChangedData.Status":                   "bounded file processing outcome only; not a clinical result or employee outcome",
	"workforce.DocumentScanRequestedData.ScanAttempt":        "processing attempt for deterministic workflow identity",
	"workforce.DocumentScanRequestedData.HospitalID":         "internal hospital scope; no facility attributes",
	"workforce.DocumentScanRequestedData.StaffID":            "opaque workforce row reference; protected lookup requires exact tenant/hospital file scope",
	"workforce.DocumentScanRequestedData.FileID":             "internal immutable upload intent reference; no name, filename, storage key or content",
	"workforce.RosterConfigChangedData.HospitalID":           "opaque hospital scope; protected lookup required",
	"workforce.RosterConfigChangedData.ConfigID":             "opaque configuration reference; no scheduling rules or personnel data",
	"workforce.RosterConfigChangedData.RevisionID":           "immutable internal revision reference; no configuration content",
	"workforce.RosterConfigChangedData.Version":              "monotonic revision for replay/order checks",
	"workforce.LeaveConfigChangedData.HospitalID":            "opaque hospital scope; protected lookup required",
	"workforce.LeaveConfigChangedData.ConfigID":              "opaque configuration reference; no leave rules or personnel data",
	"workforce.LeaveConfigChangedData.RevisionID":            "immutable internal revision reference; no configuration content",
	"workforce.LeaveConfigChangedData.Kind":                  "closed enum of configuration categories; no content",
	"workforce.LeaveConfigChangedData.Version":               "monotonic revision for replay/order checks",
	"workforce.StaffLeaveAssignmentChangedData.HospitalID":   "opaque hospital scope; protected lookup required",
	"workforce.StaffLeaveAssignmentChangedData.StaffID":      "opaque staff reference; no name, code or leave content",
	"workforce.StaffLeaveAssignmentChangedData.RevisionID":   "immutable internal revision reference; protected lookup required",
	"workforce.StaffLeaveAssignmentChangedData.Version":      "monotonic revision for replay/order checks",
	"workforce.LeaveLedgerChangedData.HospitalID":            "opaque hospital scope; protected lookup required",
	"workforce.LeaveLedgerChangedData.LeaveYear":             "leave-year label for re-reading balances; no personnel facts",
	"workforce.LeaveLedgerChangedData.Source":                "closed enum posting/year_end/correction/request; no content",
	"workforce.LeaveLedgerChangedData.ReferenceID":           "opaque run or entry reference; no staff, amounts or reasons",
	"workforce.LeaveLedgerChangedData.Entries":               "entry count for reconciliation; no staff or amounts",
	"workforce.LeaveRequestChangedData.HospitalID":           "opaque hospital scope; protected lookup required",
	"workforce.LeaveRequestChangedData.StaffID":              "opaque staff reference; no name, dates, type or reason",
	"workforce.LeaveRequestChangedData.RequestID":            "opaque request reference; protected lookup required",
	"workforce.LeaveRequestChangedData.Status":               "closed enum pending/approved/rejected/cancelled; no content",
	"workforce.LeaveRequestChangedData.Version":              "monotonic request version for replay/order checks",
	"workforce.DutyExchangeChangedData.HospitalID":           "opaque hospital scope; protected lookup required",
	"workforce.DutyExchangeChangedData.ExchangeID":           "opaque exchange reference; no people, dates or reason",
	"workforce.DutyExchangeChangedData.Status":               "closed enum pending/approved/rejected/cancelled/revoked; no content",
	"workforce.DutyExchangeChangedData.Version":              "monotonic exchange version for replay/order checks",
	"workforce.OvertimeSettingsChangedData.HospitalID":       "opaque hospital scope; protected lookup required",
	"workforce.OvertimeSettingsChangedData.RevisionID":       "immutable internal revision reference; no limits or policy content",
	"workforce.OvertimeSettingsChangedData.Version":          "monotonic revision for replay/order checks",
	"workforce.OvertimeChangedData.HospitalID":               "opaque hospital scope; protected lookup required",
	"workforce.OvertimeChangedData.RequestID":                "opaque request reference; no person, times, minutes or reason",
	"workforce.OvertimeChangedData.Status":                   "closed enum pending/approved/rejected/cancelled/revoked; no content",
	"workforce.OvertimeChangedData.Version":                  "monotonic request version for replay/order checks",
	"workforce.AttendanceSettingsChangedData.HospitalID":     "opaque hospital scope; protected lookup required",
	"workforce.AttendanceSettingsChangedData.RevisionID":     "immutable internal revision reference; no policy content",
	"workforce.AttendanceSettingsChangedData.Version":        "monotonic revision for replay/order checks",
	"workforce.AttendancePunchChangedData.HospitalID":        "opaque hospital scope; protected lookup required",
	"workforce.AttendancePunchChangedData.PunchID":           "opaque punch reference; no person, time, direction or reason",
	"workforce.AttendancePunchChangedData.Action":            "closed enum recorded/voided; no content",
	"workforce.AttendanceCorrectionChangedData.HospitalID":   "opaque hospital scope; protected lookup required",
	"workforce.AttendanceCorrectionChangedData.CorrectionID": "opaque correction reference; no person, time, direction or reason",
	"workforce.AttendanceCorrectionChangedData.Status":       "closed enum pending/approved/rejected/cancelled; no content",
	"workforce.AttendanceCorrectionChangedData.Version":      "monotonic request version for replay/order checks",
	"workforce.RosterSettingsChangedData.HospitalID":         "opaque hospital scope; protected lookup required",
	"workforce.RosterSettingsChangedData.RevisionID":         "immutable internal revision reference; no timezone or policy content",
	"workforce.RosterSettingsChangedData.Version":            "monotonic revision for replay/order checks",
	"workforce.StaffRosterAssignmentChangedData.HospitalID":  "opaque hospital scope; protected lookup required",
	"workforce.StaffRosterAssignmentChangedData.StaffID":     "opaque staff reference; no name, code or schedule content",
	"workforce.StaffRosterAssignmentChangedData.RevisionID":  "immutable internal revision reference; protected lookup required",
	"workforce.StaffRosterAssignmentChangedData.Version":     "monotonic revision for replay/order checks",

	"workforce.RosterChangedData.HospitalID": "opaque hospital scope; protected lookup required",
	"workforce.RosterChangedData.RosterID":   "opaque roster reference; no staff, dates or shift content",
	"workforce.RosterChangedData.RevisionID": "immutable internal revision reference; protected lookup required",
	"workforce.RosterChangedData.Version":    "monotonic revision for replay/order checks",

	"workforce.ShiftChangedData.HospitalID":           "internal hospital scope; no facility details",
	"workforce.ShiftChangedData.ShiftID":              "internal shift configuration reference; no personnel or policy content",
	"workforce.ShiftChangedData.RevisionID":           "internal immutable shift revision reference; protected lookup required",
	"workforce.ShiftChangedData.Version":              "monotonic configuration revision for retry/order checks",
	"workforce.StaffEnrollmentChangedData.HospitalID": "internally minted hospital scope; contains no facility name or contact details",
	"workforce.StaffEnrollmentChangedData.StaffID":    "internally minted staff row reference; resolving protected enrollment requires explicit permission and hospital manager access",
	"workforce.StaffEnrollmentChangedData.Version":    "monotonic resource revision for retry/order checks; contains no employee attributes",
	"workforce.MasterChangedData.HospitalID":          "internal hospital scope; no facility details",
	"workforce.MasterChangedData.MasterID":            "internal HR configuration reference; no employee data",
	"workforce.MasterChangedData.RevisionID":          "internal immutable configuration revision reference",
	"workforce.MasterChangedData.Kind":                "allowlisted HR configuration category; no configuration names or attributes",
	"workforce.MasterChangedData.Version":             "monotonic configuration revision; no employee data",
	"iam.MemberChangedData.Subject":                   "the iam subject identifier (e.g. GIP subject claim), already known to every module via authn.Principal.Subject; a reference, not identifying content",
	"iam.CredentialRevokedData.Subject":               "same iam subject identifier as MemberChangedData.Subject",
	"iam.MemberChangedData.RoleKey":                   "a key into the fixed role registry (docs/standards/backend.md), not clinical or identifying data",

	"medicore.VisitCreatedData.Department": "the ward/department the visit is opened in; operational routing metadata, not patient-identifying",

	// Internally-minted row ids. Listed one by one rather than matched by
	// an "ends in ID" rule: see the test's comment for why that rule was
	// removed. The reason below is the thing being asserted — it is NOT
	// true of every identifier, and an externally-issued one (Aadhaar,
	// ABHA, an insurer member number) belongs in the PHI allowlist.
	"medicore.VisitCreatedData.VisitID":              "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"medicore.VisitCreatedData.PatientID":            "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"patientmaster.PatientRegisteredData.HospitalID": "internally minted hospital scope for authorization; no name, address or external facility identifier",
	"patientmaster.PatientRegisteredData.PatientID":  "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"lab.ResultReadyData.VisitID":                    "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"lab.ResultReadyData.OrderID":                    "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"pharmacy.DispenseRecordedData.VisitID":          "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"pharmacy.DispenseRecordedData.DispenseID":       "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"reference.PingedData.PingID":                    "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"reference.PingForwardedData.PingID":             "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",

	// reference is the module that proves the platform wiring end to end
	// (issue #2); it deliberately exposes no tenant data (module.go's
	// Permissions() comment) and carries no clinical concept at all, so
	// its free-text field is operational test fixture content, never PHI.
	"reference.PingForwardedData.Message": "reference carries no clinical data by design; this is fixture text proving the directed-write wiring (#932), not patient content",
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
//  1. pinned in eventPayloadPHIAllowlist with the reason it must carry
//     PHI/identifying data, or
//  2. pinned in eventPayloadReviewedNonPHI with the reason it does not.
//
// There is NO automatic classification, deliberately. An earlier version
// auto-passed any field whose name ended in "ID" or "IDs", on the
// reasoning that this codebase's identifiers (VisitID, OrderID, PingID)
// are opaque row pointers. That rule silently admitted `AadhaarID` and
// `ABHAID` — proved by mutation during review of #835 Task 3, both of
// which the test passed without comment. In an ABDM/DPDP context those
// are among the most sensitive fields the platform could put on a wire:
// Aadhaar is sensitive personal data under the DPDP Act and ABHA is the
// national health identifier. "It is an identifier" is an argument for
// scrutiny, not against it.
//
// So every field is classified by a human exactly once. The cost is one
// map line per new payload field — 10 entries across five modules today
// — and that cost IS the control: adding a field to an event contract
// becomes a diff a reviewer must justify, which is what spec D4 asks for.
//
// A field matching neither map is a field nobody has decided about; this
// test fails closed and names it rather than guessing.
func TestEventPayloadPHIFieldsAreAllowlisted(t *testing.T) {
	fields := findPayloadFields(t)

	seen := map[string]bool{}
	for _, f := range fields {
		seen[f.key] = true

		_, inPHI := eventPayloadPHIAllowlist[f.key]
		_, inSafe := eventPayloadReviewedNonPHI[f.key]
		require.Falsef(t, inPHI && inSafe,
			"%s is pinned in BOTH eventPayloadPHIAllowlist and eventPayloadReviewedNonPHI — it cannot be both", f.key)
		require.Truef(t, inPHI || inSafe,
			"%s is a new event payload field with no PHI classification: it is not pinned in eventPayloadPHIAllowlist or eventPayloadReviewedNonPHI in event_payload_test.go. "+
				"Every field is classified by hand — there is no automatic pass for identifier-shaped names, because AadhaarID and ABHAID are identifiers too. "+
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
