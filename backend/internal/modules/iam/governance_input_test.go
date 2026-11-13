package iam_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own module.
)

func TestGovernanceGrantInputBindsTheTemplateToItsNonclinicalScope(t *testing.T) {
	scope := uuid.MustParse("019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa")
	start := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	input := iam.GovernanceGrantInput{Subject: "synthetic-reviewer", ExpectedMembershipEpoch: 1, TemplateKey: "workforce.account_link_reviewer.v1", ScopeKind: "hospital", ScopeID: &scope, Reason: "Independently review Staff account links", ValidFrom: start, ExpiresAt: start.Add(time.Hour)}
	require.NoError(t, input.Validate())
	input.ScopeKind = "tenant"
	input.ScopeID = nil
	require.Error(t, input.Validate())
	input.TemplateKey = "iam.governance_auditor.v1"
	require.NoError(t, input.Validate())
	input.ScopeKind = "hospital"
	input.ScopeID = &scope
	require.Error(t, input.Validate())
}

func TestGovernanceGrantDecoderRejectsSuppliedAuthorityAndAmbiguousBodies(t *testing.T) {
	valid := `{"subject":"synthetic-reviewer","expected_membership_epoch":1,"template_key":"workforce.account_link_reviewer.v1","scope_kind":"hospital","scope_id":"019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa","reason":"Review independent Staff links","valid_from":"2026-11-13T00:00:00Z","expires_at":"2026-11-13T01:00:00Z"}`
	input, err := iam.DecodeGovernanceGrant([]byte(valid))
	require.NoError(t, err)
	require.Equal(t, "synthetic-reviewer", input.Subject)
	for name, raw := range map[string]string{
		"actor":         `{"actor_subject":"forged",` + valid[1:],
		"tenant":        `{"tenant_id":"11111111-1111-4111-8111-111111111111",` + valid[1:],
		"role":          `{"role_key":"tenant_admin",` + valid[1:],
		"duplicate":     `{"subject":"another-person",` + valid[1:],
		"case alias":    `{"Subject":"another-person",` + valid[1:],
		"two documents": valid + `{}`,
		"null":          `null`, "array": `[]`, "missing": `{}`,
	} {
		t.Run(name, func(t *testing.T) { _, err := iam.DecodeGovernanceGrant([]byte(raw)); require.Error(t, err) })
	}
}

func TestGovernanceDecisionDecoderKeepsActorAuthorityOutOfTheBody(t *testing.T) {
	input, err := iam.DecodeGovernanceDecision([]byte(`{"expected_version":1,"decision":"approve","reason":"Independent source evidence reviewed"}`))
	require.NoError(t, err)
	require.EqualValues(t, 1, input.ExpectedVersion)
	require.Equal(t, "approve", input.Decision)
	for name, raw := range map[string]string{
		"actor":          `{"expected_version":1,"decision":"approve","reason":"Reviewed","actor_subject":"issuer"}`,
		"duplicate":      `{"expected_version":1,"decision":"approve","decision":"reject","reason":"Reviewed"}`,
		"unsupported":    `{"expected_version":1,"decision":"self_approve","reason":"Reviewed"}`,
		"zero version":   `{"expected_version":0,"decision":"approve","reason":"Reviewed"}`,
		"no successor":   `{"expected_version":9007199254740991,"decision":"approve","reason":"Reviewed"}`,
		"unsafe version": `{"expected_version":9007199254740992,"decision":"approve","reason":"Reviewed"}`,
		"blank reason":   `{"expected_version":1,"decision":"approve","reason":"   "}`,
		"control reason": `{"expected_version":1,"decision":"approve","reason":"review\nchanged"}`,
	} {
		t.Run(name, func(t *testing.T) { _, err := iam.DecodeGovernanceDecision([]byte(raw)); require.Error(t, err) })
	}
}

func TestGovernanceGrantDecoderBoundsBytesAndRejectsNullOrInvalidUTF8(t *testing.T) {
	valid := `{"subject":"synthetic-reviewer","expected_membership_epoch":1,"template_key":"iam.governance_auditor.v1","scope_kind":"tenant","reason":"Independent source review","valid_from":"2026-11-13T00:00:00Z","expires_at":"2026-11-13T01:00:00Z"}`
	bounded := valid + strings.Repeat(" ", 4096-len(valid))
	_, err := iam.DecodeGovernanceGrant([]byte(bounded))
	require.NoError(t, err)
	for name, body := range map[string][]byte{
		"oversized":        []byte(bounded + " "),
		"null optional id": []byte(`{"scope_id":null,` + valid[1:]),
		"noninteger epoch": []byte(strings.Replace(valid, `"expected_membership_epoch":1`, `"expected_membership_epoch":"1"`, 1)),
		"control reason":   []byte(strings.Replace(valid, "Independent source review", `review\nchanged`, 1)),
		"invalid utf8":     append(append([]byte(`{"reason":"`), byte(0xff)), []byte(`",`+strings.Replace(valid[1:], `"reason":"Independent source review",`, "", 1))...),
	} {
		t.Run(name, func(t *testing.T) { _, err := iam.DecodeGovernanceGrant(body); require.Error(t, err) })
	}
}

func TestGovernanceGrantInputRejectsInvalidMembershipAndScopeReferences(t *testing.T) {
	id := uuid.MustParse("019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa")
	start := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	valid := iam.GovernanceGrantInput{Subject: "synthetic-reviewer", ExpectedMembershipEpoch: 1, TemplateKey: "workforce.account_link_reviewer.v1", ScopeKind: "hospital", ScopeID: &id, Reason: "Independent review", ValidFrom: start, ExpiresAt: start.Add(time.Hour)}
	for _, kind := range []string{"hospital", "pharmacy", "supplier"} {
		input := valid
		input.ScopeKind = kind
		require.NoError(t, input.Validate())
	}
	for name, change := range map[string]func(*iam.GovernanceGrantInput){
		"missing membership": func(in *iam.GovernanceGrantInput) { in.ExpectedMembershipEpoch = 0 },
		"unsafe epoch":       func(in *iam.GovernanceGrantInput) { in.ExpectedMembershipEpoch = 9007199254740992 },
		"missing employer":   func(in *iam.GovernanceGrantInput) { in.ScopeID = nil },
		"nil employer":       func(in *iam.GovernanceGrantInput) { zero := uuid.Nil; in.ScopeID = &zero },
		"invalid variant": func(in *iam.GovernanceGrantInput) {
			wrong := uuid.MustParse("11111111-1111-4111-1111-111111111111")
			in.ScopeID = &wrong
		},
		"unknown template": func(in *iam.GovernanceGrantInput) { in.TemplateKey = "tenant_admin" },
		"department":       func(in *iam.GovernanceGrantInput) { in.ScopeKind = "department" },
		"blank reason":     func(in *iam.GovernanceGrantInput) { in.Reason = "   " },
		"control reason":   func(in *iam.GovernanceGrantInput) { in.Reason = "review\nchanged" },
		"long reason":      func(in *iam.GovernanceGrantInput) { in.Reason = strings.Repeat("x", 501) },
		"blank subject":    func(in *iam.GovernanceGrantInput) { in.Subject = " " },
		"injected subject": func(in *iam.GovernanceGrantInput) { in.Subject = "user:another" },
		"expired interval": func(in *iam.GovernanceGrantInput) { in.ExpiresAt = in.ValidFrom },
		"zero start":       func(in *iam.GovernanceGrantInput) { in.ValidFrom = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) { in := valid; change(&in); require.Error(t, in.Validate()) })
	}
}

func TestGovernanceGrantInputUsesBoundedMicrosecondInstants(t *testing.T) {
	start := time.Date(2026, 11, 13, 0, 0, 0, 1000, time.UTC)
	valid := iam.GovernanceGrantInput{Subject: "synthetic-auditor", ExpectedMembershipEpoch: 1, TemplateKey: "iam.governance_auditor.v1", ScopeKind: "tenant", Reason: "Review source authority", ValidFrom: start, ExpiresAt: start.Add(time.Microsecond)}
	require.NoError(t, valid.Validate())
	for name, value := range map[string]time.Time{"submicrosecond": start.Add(time.Nanosecond), "zero": {}, "year zero": time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)} {
		t.Run(name, func(t *testing.T) {
			input := valid
			input.ValidFrom = value
			input.ExpiresAt = start.Add(time.Hour)
			require.Error(t, input.Validate())
		})
	}
	input := valid
	input.ExpiresAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	require.Error(t, input.Validate())
}

func TestGovernanceReasonsUseTheSameTextLimitAsBrowserContracts(t *testing.T) {
	start := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	input := iam.GovernanceGrantInput{Subject: "synthetic-auditor", ExpectedMembershipEpoch: 1, TemplateKey: "iam.governance_auditor.v1", ScopeKind: "tenant", Reason: strings.Repeat("🔎", 250), ValidFrom: start, ExpiresAt: start.Add(time.Hour)}
	require.NoError(t, input.Validate())
	input.Reason += "🔎"
	require.Error(t, input.Validate())
	input.Reason = "\ufeff"
	require.Error(t, input.Validate())
}

func TestGovernanceNoticeAcknowledgementRejectsForgedRecipientsAndAmbiguousVersions(t *testing.T) {
	input, err := iam.DecodeGovernanceNoticeAcknowledgement([]byte(`{"expected_version":1}`))
	require.NoError(t, err)
	require.EqualValues(t, 1, input.ExpectedVersion)
	for name, body := range map[string]string{
		"recipient":  `{"expected_version":1,"recipient_subject":"another-owner"}`,
		"tenant":     `{"expected_version":1,"tenant_id":"another-tenant"}`,
		"duplicate":  `{"expected_version":1,"expected_version":2}`,
		"case alias": `{"Expected_version":1}`,
		"zero":       `{"expected_version":0}`,
		"unsafe":     `{"expected_version":9007199254740992}`,
		"null":       `{"expected_version":null}`,
		"missing":    `{}`,
		"trailing":   `{"expected_version":1}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := iam.DecodeGovernanceNoticeAcknowledgement([]byte(body))
			require.Error(t, err)
		})
	}
}

func TestGovernanceSubjectsRejectInvisibleOrControlCharacters(t *testing.T) {
	start := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	for _, character := range []string{"\ufeff", "\u0085", "\u0001"} {
		input := iam.GovernanceGrantInput{Subject: "reviewer" + character + "suffix", ExpectedMembershipEpoch: 1, TemplateKey: "iam.governance_auditor.v1", ScopeKind: "tenant", Reason: "Review evidence", ValidFrom: start, ExpiresAt: start.Add(time.Hour)}
		require.Error(t, input.Validate(), "invisible or control characters cannot identify a governance recipient")
	}
}

func TestGovernanceGrantDecoderDoesNotNormalizeMalformedOrTruncatedInstants(t *testing.T) {
	const valid = `{"subject":"synthetic-auditor","expected_membership_epoch":1,"template_key":"iam.governance_auditor.v1","scope_kind":"tenant","reason":"Review evidence","valid_from":"2026-11-13T00:00:00Z","expires_at":"2026-11-15T00:00:00Z"}`
	for _, instant := range []string{"2026-11-13T0:00:00Z", "2026-11-13T00:00:00,1Z", "2026-11-13T00:00:00+24:00", "2026-11-13T00:00:00+00:60", "2026-11-13T00:00:00.0000000001Z"} {
		t.Run(instant, func(t *testing.T) {
			_, err := iam.DecodeGovernanceGrant([]byte(strings.Replace(valid, "2026-11-13T00:00:00Z", instant, 1)))
			require.Error(t, err)
		})
	}
	for _, instant := range []string{"2026-11-13T00:00:00.000001Z", "2026-11-13T00:00:00.0000010000Z", "2026-11-13T00:00:00+05:30"} {
		t.Run(instant, func(t *testing.T) {
			input, err := iam.DecodeGovernanceGrant([]byte(strings.Replace(valid, "2026-11-13T00:00:00Z", instant, 1)))
			require.NoError(t, err)
			require.False(t, input.ValidFrom.IsZero())
		})
	}
}
