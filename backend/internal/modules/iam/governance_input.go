package iam

import (
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
)

var errGovernanceInput = errors.New("invalid governance command")

// Validate binds a fixed nonclinical template to its canonical scope and interval.
func (in GovernanceGrantInput) Validate() error {
	if binding.Validator.ValidateStruct(in) != nil || !proposalSubject(in.Subject) || strings.ContainsFunc(in.Subject, func(r rune) bool { return unicode.IsControl(r) || r == '\ufeff' }) || len(utf16.Encode([]rune(in.Subject))) > governanceSubjectMaxUTF16 || !governanceReason(in.Reason) {
		return errGovernanceInput
	}
	var matched bool
	for _, definition := range governanceTemplates {
		if definition.Key == in.TemplateKey && slices.Contains(definition.ScopeKinds, in.ScopeKind) {
			matched = true
			break
		}
	}
	if !matched || (in.ScopeKind == "tenant") != (in.ScopeID == nil) {
		return errGovernanceInput
	}
	if in.ScopeID != nil && (*in.ScopeID == uuid.Nil || in.ScopeID.Variant() != uuid.RFC4122) {
		return errGovernanceInput
	}
	if !governanceInstant(in.ValidFrom) || !governanceInstant(in.ExpiresAt) || !in.ExpiresAt.After(in.ValidFrom) {
		return errGovernanceInput
	}
	return nil
}

func governanceReason(value string) bool {
	return utf8.ValidString(value) && strings.TrimFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r == '\ufeff' }) != "" && len(utf16.Encode([]rune(value))) <= governanceReasonMaxUTF16 && !strings.ContainsFunc(value, unicode.IsControl)
}

// Validate requires an observed revision and attributable, supported decision.
func (in GovernanceGrantDecisionInput) Validate() error {
	if binding.Validator.ValidateStruct(in) != nil || !governanceReason(in.Reason) {
		return errGovernanceInput
	}
	return nil
}

func governanceInstant(value time.Time) bool {
	lower, lowErr := time.Parse(time.RFC3339Nano, governanceInstantBounds[0])
	upper, highErr := time.Parse(time.RFC3339Nano, governanceInstantBounds[1])
	return lowErr == nil && highErr == nil && value.After(lower) && !value.After(upper) && value.Nanosecond()%governanceInstantQuantumNS == 0
}
