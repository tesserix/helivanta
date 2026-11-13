package iam

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"slices"
	"unicode/utf8"

	"github.com/gin-gonic/gin/binding"
)

var governanceWireInstant = regexp.MustCompile(governanceInstantPattern)

// DecodeGovernanceGrant accepts one bounded, unambiguous intent without actor authority.
func DecodeGovernanceGrant(raw []byte) (GovernanceGrantInput, error) {
	var input GovernanceGrantInput
	if governanceBody(raw, governanceGrantInputFields) != nil || json.Unmarshal(raw, &input) != nil || input.Validate() != nil {
		return GovernanceGrantInput{}, errGovernanceInput
	}
	return input, nil
}

func governanceBody(raw []byte, fields []string) error {
	if len(raw) > governanceMaxRequestBytes || !utf8.Valid(raw) {
		return errGovernanceInput
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return errGovernanceInput
	}
	seen := make(map[string]bool, len(fields))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return errGovernanceInput
		}
		key, ok := token.(string)
		if !ok || !slices.Contains(fields, key) || seen[key] {
			return errGovernanceInput
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errGovernanceInput
		}
		if key == "valid_from" || key == "expires_at" {
			var instant string
			if json.Unmarshal(value, &instant) != nil || !governanceWireInstant.MatchString(instant) {
				return errGovernanceInput
			}
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return errGovernanceInput
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errGovernanceInput
	}
	return nil
}

// DecodeGovernanceDecision retains only decision intent; the reviewer comes from authentication.
func DecodeGovernanceDecision(raw []byte) (GovernanceGrantDecisionInput, error) {
	var input GovernanceGrantDecisionInput
	if governanceBody(raw, governanceGrantDecisionInputFields) != nil || json.Unmarshal(raw, &input) != nil || input.Validate() != nil {
		return GovernanceGrantDecisionInput{}, errGovernanceInput
	}
	return input, nil
}

// DecodeGovernanceNoticeAcknowledgement accepts a version without recipient authority.
func DecodeGovernanceNoticeAcknowledgement(raw []byte) (GovernanceNoticeAcknowledgementInput, error) {
	var input GovernanceNoticeAcknowledgementInput
	if governanceBody(raw, governanceNoticeAcknowledgementInputFields) != nil || json.Unmarshal(raw, &input) != nil || binding.Validator.ValidateStruct(input) != nil {
		return GovernanceNoticeAcknowledgementInput{}, errGovernanceInput
	}
	return input, nil
}
