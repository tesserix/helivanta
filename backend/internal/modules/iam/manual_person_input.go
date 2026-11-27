package iam

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/google/uuid"
)

var errManualIdentityInput = errors.New("invalid manual person identity input")

func manualIdentityExpectedVersion(version *int64, minimum int64) bool {
	return version != nil && *version >= minimum && *version <= manualIdentityMaxVersion
}

func manualIdentitySubject(subject string) bool {
	if len(subject) < 1 || len(subject) > 200 {
		return false
	}
	for _, c := range subject {
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func validateManualIdentityCase(in ManualIdentityCaseInput) error {
	if !manualIdentityUUID(in.SourceProfileID) || !manualIdentityExpectedVersion(in.ExpectedProfileVersion, 1) ||
		!in.ConsentAcknowledged || !manualIdentityText(in.Reason, manualIdentityCaseReasonMaximum) {
		return errManualIdentityInput
	}
	return nil
}

func validateManualIdentityExamination(in ManualIdentityExaminationInput, now time.Time) error {
	if !manualIdentityExpectedVersion(in.ExpectedCaseVersion, 1) || !manualIdentityExpectedVersion(in.ExpectedPersonVersion, 0) ||
		!manualIdentityUUID(in.PersonID) || !manualIdentityUUID(in.SourcePersonReference) ||
		!manualIdentityUUID(in.SourceReference) || !manualIdentityUUID(in.FindingReference) ||
		!in.OriginalEvidenceExamined || !in.KnownAssociationsChecked ||
		!governanceInstant(in.ObservedAt) || !governanceInstant(in.ValidUntil) ||
		in.ObservedAt.After(now) || !in.ValidUntil.After(now) || !in.ValidUntil.After(in.ObservedAt) ||
		!manualIdentityText(in.Reason, manualIdentityExaminationReasonMaximum) {
		return errManualIdentityInput
	}
	return nil
}

func validateManualIdentityDecision(in ManualIdentityDecisionInput) error {
	if !manualIdentityExpectedVersion(in.ExpectedCaseVersion, 1) || !manualIdentityUUID(in.ExaminationID) ||
		(in.Decision != "approve" && in.Decision != "reject") || !in.EvidenceExamined || !in.IndependentPersonConfirmed ||
		!manualIdentityText(in.Reason, manualIdentityDecisionReasonMaximum) {
		return errManualIdentityInput
	}
	return nil
}

func manualIdentityUUID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == manualIdentityResourceUUIDVersion && id.Variant() == uuid.RFC4122
}
func manualIdentityText(value string, maximum int) bool {
	return utf8.ValidString(value) && strings.Trim(value, manualIdentityBlankCharacters) != "" && len(utf16.Encode([]rune(value))) <= maximum && !strings.ContainsFunc(value, unicode.IsControl)
}

func validateManualIdentitySource(in ManualIdentitySourceImport, now time.Time) error {
	if in.TenantID == uuid.Nil || in.TenantID.Variant() != uuid.RFC4122 || !manualIdentityUUID(in.SourceProfileID) || !manualIdentityUUID(in.PolicyReference) || !manualIdentityUUID(in.ReviewReference) || !manualIdentityUUID(in.WitnessReference) || !in.IndependentRootReviewConfirmed || !manualIdentityText(in.DisplayName, manualIdentityDisplayNameMaximum) || !manualIdentityText(in.Reason, manualIdentityImportReasonMaximum) || !governanceInstant(in.ObservedAt) || !governanceInstant(in.ValidFrom) || !governanceInstant(in.ValidUntil) || in.ObservedAt.After(now) || !in.ValidUntil.After(in.ValidFrom) || !in.ValidUntil.After(now) || len(in.Roots) < manualIdentityRootMinimum || len(in.Roots) > manualIdentityRootMaximum {
		return errManualIdentityInput
	}
	people := map[uuid.UUID]bool{}
	references := map[uuid.UUID]bool{}
	accounts := map[string]bool{}
	for _, person := range in.Roots {
		if !manualIdentityUUID(person.PersonID) || !manualIdentityUUID(person.SourcePersonReference) || !manualIdentityUUID(person.FindingReference) || people[person.PersonID] || references[person.SourcePersonReference] || len(person.Accounts) < 1 || len(person.Accounts) > manualIdentityAccountMaximum {
			return errManualIdentityInput
		}
		people[person.PersonID] = true
		references[person.SourcePersonReference] = true
		for _, account := range person.Accounts {
			if !manualIdentitySubject(account.Subject) || accounts[account.Subject] || account.ExpectedMembershipEpoch < 1 || account.ExpectedMembershipEpoch > manualIdentityMaxVersion {
				return errManualIdentityInput
			}
			accounts[account.Subject] = true
		}
	}
	return nil
}
