package pharmacy

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestVerificationDecisionIntent(t *testing.T) {
	note := "Synthetic prescriber clarification."
	reason := "prescriber_clarification"
	amendment := uuid.MustParse("0199e000-0000-7000-8000-000000000001")
	base := PrescriptionVerificationDecisionInput{ExpectedVersion: 1, ReviewedStateHash: strings.Repeat("a", 64), Decision: "approved"}
	if !validVerificationDecisionInput(base) {
		t.Fatal("plain version-bound approval refused")
	}
	for _, decision := range []string{"query_raised", "rejected", "approved_with_change"} {
		t.Run(decision, func(t *testing.T) {
			in := base
			in.Decision = decision
			in.ReasonCode = &reason
			in.ClinicalNote = &note
			if decision == "approved_with_change" {
				in.PrescriberAmendmentID = &amendment
			}
			if !validVerificationDecisionInput(in) {
				t.Fatal("complete decision intent refused")
			}
			noReason := in
			noReason.ReasonCode = nil
			if validVerificationDecisionInput(noReason) {
				t.Fatal("reasonless decision accepted")
			}
			noNote := in
			noNote.ClinicalNote = nil
			if validVerificationDecisionInput(noNote) {
				t.Fatal("decision without clinical note accepted")
			}
			for _, blank := range []string{" ", "\t\n", "\u00a0\ufeff"} {
				empty := in
				empty.ClinicalNote = &blank
				if validVerificationDecisionInput(empty) {
					t.Fatal("blank clinical reason accepted")
				}
			}
		})
	}
	for name, change := range map[string]func(*PrescriptionVerificationDecisionInput){
		"zero version":        func(in *PrescriptionVerificationDecisionInput) { in.ExpectedVersion = 0 },
		"unsafe JS version":   func(in *PrescriptionVerificationDecisionInput) { in.ExpectedVersion = 9007199254740992 },
		"uppercase hash":      func(in *PrescriptionVerificationDecisionInput) { in.ReviewedStateHash = strings.Repeat("A", 64) },
		"short hash":          func(in *PrescriptionVerificationDecisionInput) { in.ReviewedStateHash = strings.Repeat("a", 63) },
		"unknown decision":    func(in *PrescriptionVerificationDecisionInput) { in.Decision = "emergency_approved" },
		"unrelated amendment": func(in *PrescriptionVerificationDecisionInput) { in.PrescriberAmendmentID = &amendment },
		"missing amendment": func(in *PrescriptionVerificationDecisionInput) {
			in.Decision = "approved_with_change"
			in.ReasonCode = &reason
			in.ClinicalNote = &note
		},
		"nil amendment": func(in *PrescriptionVerificationDecisionInput) {
			in.Decision = "approved_with_change"
			in.ReasonCode = &reason
			in.ClinicalNote = &note
			id := uuid.Nil
			in.PrescriberAmendmentID = &id
		},
		"trailing newline hash": func(in *PrescriptionVerificationDecisionInput) { in.ReviewedStateHash += "\n" },
		"trailing newline reason": func(in *PrescriptionVerificationDecisionInput) {
			r := "prescriber_clarification\n"
			in.ReasonCode = &r
		},
		"oversize note":  func(in *PrescriptionVerificationDecisionInput) { n := strings.Repeat("x", 2001); in.ClinicalNote = &n },
		"NUL note":       func(in *PrescriptionVerificationDecisionInput) { n := "Synthetic\x00note"; in.ClinicalNote = &n },
		"invalid UTF-8":  func(in *PrescriptionVerificationDecisionInput) { n := string([]byte{0xff}); in.ClinicalNote = &n },
		"invalid reason": func(in *PrescriptionVerificationDecisionInput) { r := "not a code"; in.ReasonCode = &r },
	} {
		t.Run(name, func(t *testing.T) {
			in := base
			change(&in)
			if validVerificationDecisionInput(in) {
				t.Fatal("invalid intent accepted")
			}
		})
	}
	for _, sample := range []struct {
		note  string
		valid bool
	}{
		{strings.Repeat("\U0001f600", 1000), true}, {strings.Repeat("\U0001f600", 1001), false},
	} {
		in := base
		in.ClinicalNote = &sample.note
		if validVerificationDecisionInput(in) != sample.valid {
			t.Fatal("note limits must use the browser's UTF-16 units")
		}
	}
}

func TestVerificationSecondCheckIntent(t *testing.T) {
	base := PrescriptionVerificationSecondCheckInput{ExpectedVersion: 2, ReviewedStateHash: strings.Repeat("b", 64), PrimaryDecisionID: uuid.MustParse("0199e000-0000-7000-8000-000000000002")}
	if !validVerificationSecondCheckInput(base) {
		t.Fatal("complete second-check intent refused")
	}
	for name, change := range map[string]func(*PrescriptionVerificationSecondCheckInput){
		"zero version":             func(in *PrescriptionVerificationSecondCheckInput) { in.ExpectedVersion = 0 },
		"unsafe JS version":        func(in *PrescriptionVerificationSecondCheckInput) { in.ExpectedVersion = 9007199254740992 },
		"missing primary decision": func(in *PrescriptionVerificationSecondCheckInput) { in.PrimaryDecisionID = uuid.Nil },
		"invalid hash":             func(in *PrescriptionVerificationSecondCheckInput) { in.ReviewedStateHash = "" },
	} {
		t.Run(name, func(t *testing.T) {
			in := base
			change(&in)
			if validVerificationSecondCheckInput(in) {
				t.Fatal("incomplete second-check intent accepted")
			}
		})
	}
}
