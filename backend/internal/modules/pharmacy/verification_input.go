package pharmacy

import (
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	verificationHash   = regexp.MustCompile(verificationHashPattern)
	verificationReason = regexp.MustCompile(verificationReasonPattern)
)

// validVerificationDecisionInput checks intent only, never clinical eligibility or source authority.
func validVerificationDecisionInput(in PrescriptionVerificationDecisionInput) bool {
	reason, amendment, known := verificationDecisionRequirements(in.Decision)
	if !known || !validVerificationPrecondition(in.ExpectedVersion, in.ReviewedStateHash) {
		return false
	}
	if in.ReasonCode != nil && (len(*in.ReasonCode) > verificationReasonMax || !verificationReason.MatchString(*in.ReasonCode)) {
		return false
	}
	if in.ClinicalNote != nil {
		length, valid := verificationTextLength(*in.ClinicalNote)
		if !valid || length == 0 || length > verificationNoteMaxUTF16 {
			return false
		}
	}
	if reason && (in.ReasonCode == nil || in.ClinicalNote == nil || strings.Trim(*in.ClinicalNote, verificationBlankNoteCharacters) == "") {
		return false
	}
	return amendment == (in.PrescriberAmendmentID != nil) && (in.PrescriberAmendmentID == nil || *in.PrescriberAmendmentID != uuid.Nil)
}

func validVerificationSecondCheckInput(in PrescriptionVerificationSecondCheckInput) bool {
	return in.PrimaryDecisionID != uuid.Nil && validVerificationPrecondition(in.ExpectedVersion, in.ReviewedStateHash)
}

func validVerificationPrecondition(version int64, hash string) bool {
	return version >= 1 && version <= verificationMaxVersion && verificationHash.MatchString(hash)
}

func verificationTextLength(value string) (int, bool) {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return 0, false
	}
	length := 0
	for _, r := range value {
		length += utf16.RuneLen(r)
	}
	return length, true
}
