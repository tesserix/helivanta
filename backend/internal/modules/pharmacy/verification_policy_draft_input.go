package pharmacy

import (
	"strings"
)

// validVerificationPolicyDraftInput validates administrative intent, never clinical authority.
func validVerificationPolicyDraftInput(in VerificationPolicyDraftInput) bool {
	if in.ExpectedVersion == nil || *in.ExpectedVersion < 0 || *in.ExpectedVersion > verificationPolicyMaxVersion || in.SecondCheckRequired == nil {
		return false
	}
	if in.ReviewValidForSeconds < 1 || in.ReviewValidForSeconds > verificationPolicyReviewDurationMax || in.QueryResponseTargetSeconds < 1 || in.QueryResponseTargetSeconds > verificationPolicyResponseDurationMax {
		return false
	}
	label := strings.Trim(in.Label, verificationBlankNoteCharacters)
	reason := strings.Trim(in.Reason, verificationBlankNoteCharacters)
	if verificationPolicyLabelNoASCIIControls {
		for _, r := range label {
			if r < 32 || r == 127 {
				return false
			}
		}
	}
	labelLength, labelValid := verificationTextLength(label)
	reasonLength, reasonValid := verificationTextLength(reason)
	return labelValid && reasonValid && labelLength >= 1 && labelLength <= verificationPolicyLabelMax && reasonLength >= 1 && reasonLength <= verificationPolicyReasonMax
}
