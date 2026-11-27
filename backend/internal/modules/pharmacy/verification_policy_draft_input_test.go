package pharmacy

import (
	"strings"
	"testing"
)

func TestVerificationPolicyDraftIntent(t *testing.T) {
	makeInput := func() VerificationPolicyDraftInput {
		version := int64(0)
		second := true
		return VerificationPolicyDraftInput{ExpectedVersion: &version, Label: "TEST review policy", SecondCheckRequired: &second, ReviewValidForSeconds: 600, QueryResponseTargetSeconds: 1200, Reason: "Synthetic administrative proposal"}
	}
	cases := []struct {
		name   string
		mutate func(*VerificationPolicyDraftInput)
		valid  bool
	}{
		{"first intent", func(*VerificationPolicyDraftInput) {}, true},
		{"next revision", func(in *VerificationPolicyDraftInput) { v := int64(4); in.ExpectedVersion = &v }, true},
		{"explicit single review proposal", func(in *VerificationPolicyDraftInput) { v := false; in.SecondCheckRequired = &v }, true},
		{"missing version", func(in *VerificationPolicyDraftInput) { in.ExpectedVersion = nil }, false},
		{"negative version", func(in *VerificationPolicyDraftInput) { v := int64(-1); in.ExpectedVersion = &v }, false},
		{"overflow revision", func(in *VerificationPolicyDraftInput) { v := int64(9007199254740991); in.ExpectedVersion = &v }, false},
		{"missing second-check choice", func(in *VerificationPolicyDraftInput) { in.SecondCheckRequired = nil }, false},
		{"zero review interval", func(in *VerificationPolicyDraftInput) { in.ReviewValidForSeconds = 0 }, false},
		{"negative response interval", func(in *VerificationPolicyDraftInput) { in.QueryResponseTargetSeconds = -1 }, false},
		{"duration overflow", func(in *VerificationPolicyDraftInput) { in.ReviewValidForSeconds = 9223372037 }, false},
		{"interior ASCII control", func(in *VerificationPolicyDraftInput) { in.Label = "Synthetic\x01policy" }, false},
		{"DEL control", func(in *VerificationPolicyDraftInput) { in.Label = "Synthetic\x7fpolicy" }, false},
		{"non-ASCII boundary", func(in *VerificationPolicyDraftInput) { in.Label = "Synthetic\u0080policy" }, true},
		{"blank label", func(in *VerificationPolicyDraftInput) { in.Label = "\u00a0\ufeff" }, false},
		{"maximum unicode label", func(in *VerificationPolicyDraftInput) { in.Label = strings.Repeat("😀", 60) }, true},
		{"oversized unicode label", func(in *VerificationPolicyDraftInput) { in.Label = strings.Repeat("😀", 61) }, false},
		{"missing reason", func(in *VerificationPolicyDraftInput) { in.Reason = "" }, false},
		{"invalid unicode", func(in *VerificationPolicyDraftInput) { in.Reason = string([]byte{0xff}) }, false},
		{"nul reason", func(in *VerificationPolicyDraftInput) { in.Reason = "x\x00y" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := makeInput()
			tc.mutate(&in)
			if got := validVerificationPolicyDraftInput(in); got != tc.valid {
				t.Fatalf("valid=%v want=%v", got, tc.valid)
			}
		})
	}
}
