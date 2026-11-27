package iam

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

//go:embed manual_person_source_before.sql
var manualPersonSourceBeforeSchema string

//go:embed manual_person_source.sql
var manualPersonSourceSchema string

//go:embed manual_person_source_contract_generated.sql
var manualPersonSourceContractSchema string

const manualIdentityImportBodyMaximum = 64 << 10

var errManualIdentityImportConflict = errors.New("manual identity source import conflicts with retained intent")
var errManualIdentityOperatorRequired = errors.New("manual identity source import requires schema owner")

// DecodeManualIdentitySource bounds and validates a source document without I/O.
func DecodeManualIdentitySource(reader io.Reader) (ManualIdentitySourceImport, error) {
	payload, err := io.ReadAll(io.LimitReader(reader, manualIdentityImportBodyMaximum+1))
	if err != nil || len(payload) > manualIdentityImportBodyMaximum {
		return ManualIdentitySourceImport{}, errManualIdentityInput
	}
	var in ManualIdentitySourceImport
	if suppliertrade.DecodeBoundedObject(payload, &in, manualIdentityImportBodyMaximum) != nil {
		return ManualIdentitySourceImport{}, errManualIdentityInput
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return ManualIdentitySourceImport{}, errManualIdentityInput
	}
	for _, field := range manualIdentityImportInstantFields {
		var instant string
		if json.Unmarshal(fields[field], &instant) != nil || !governanceWireInstant.MatchString(instant) {
			return ManualIdentitySourceImport{}, errManualIdentityInput
		}
	}
	if err := validateManualIdentitySource(in, in.ObservedAt); err != nil {
		return ManualIdentitySourceImport{}, err
	}
	return in, nil
}

// ValidateManualIdentitySource checks a proposed new import's current validity.
func ValidateManualIdentitySource(in ManualIdentitySourceImport, now time.Time) error {
	return validateManualIdentitySource(in, now)
}

// ImportManualIdentitySource is an operator operation on an existing owner transaction.
// It never opens an administrative pool or changes membership or role grants.
func ImportManualIdentitySource(tx *gorm.DB, in ManualIdentitySourceImport) (ManualIdentityImportReceipt, error) {
	var receipt ManualIdentityImportReceipt
	if err := validateManualIdentitySource(in, in.ObservedAt); err != nil {
		return receipt, err
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return receipt, errManualIdentityInput
	}
	digest := sha256.Sum256(payload)
	intent := hex.EncodeToString(digest[:])
	err = tx.Transaction(func(tx *gorm.DB) error {
		var operator struct {
			Role  string
			Owner bool
		}
		if err := tx.Raw("SELECT current_user AS role, current_user=pg_get_userbyid(relowner) AS owner FROM pg_class WHERE oid='iam_manual_identity_profiles'::regclass").Scan(&operator).Error; err != nil {
			return err
		}
		if !operator.Owner {
			return errManualIdentityOperatorRequired
		}
		var intact bool
		if err := tx.Raw("SELECT iam_manual_identity_integrity() AND iam_staff_selection_integrity() AND iam_operating_governance_integrity()").Scan(&intact).Error; err != nil {
			return err
		}
		if !intact {
			return errManualIdentityState
		}

		if err := tx.Exec("SELECT set_config('app.tenant_id',?,true),set_config('app.iam_manual_identity_import','authorized',true)", in.TenantID.String()).Error; err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "iam-manual-identity:"+in.TenantID.String()).Error; err != nil {
			return err
		}
		var retained struct {
			IntentDigest string
			ActorRole    string
			People       int
			Accounts     int
			ImportedAt   time.Time
		}
		query := tx.Table("iam_manual_identity_imports").Where("tenant_id=? AND source_profile_id=?", in.TenantID, in.SourceProfileID).Take(&retained)
		if query.Error == nil {
			if retained.IntentDigest != intent || retained.ActorRole != operator.Role {
				return errManualIdentityImportConflict
			}
			receipt = ManualIdentityImportReceipt{SourceProfileID: in.SourceProfileID, People: retained.People, Accounts: retained.Accounts, ImportedAt: retained.ImportedAt, ClinicalAuthority: "not_evaluated"}
			return tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'replay','iam_manual_identity_imports',?)", in.TenantID, "operator:"+operator.Role, in.SourceProfileID).Error
		}
		if !errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return query.Error
		}
		var now time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			return err
		}
		if err := validateManualIdentitySource(in, now); err != nil {
			return err
		}
		var existing int64
		if err := tx.Table("iam_manual_identity_source_heads").Where("tenant_id=?", in.TenantID).Count(&existing).Error; err != nil {
			return err
		}
		if existing != 0 {
			return errManualIdentityImportConflict
		}
		accounts := make([]ManualIdentityRootAccount, 0)
		for _, person := range in.Roots {
			accounts = append(accounts, person.Accounts...)
		}
		slices.SortFunc(accounts, func(a, b ManualIdentityRootAccount) int {
			if a.Subject < b.Subject {
				return -1
			}
			if a.Subject > b.Subject {
				return 1
			}
			return 0
		})
		for _, account := range accounts {
			epoch, err := proposalMembershipEpoch(tx, in.TenantID.String(), account.Subject)
			if err != nil {
				return err
			}
			if epoch != account.ExpectedMembershipEpoch {
				return errManualIdentityState
			}
			var member bool
			if err := tx.Raw("SELECT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=? AND subject=?)", in.TenantID, account.Subject).Scan(&member).Error; err != nil {
				return err
			}
			if !member {
				return errManualIdentityState
			}
		}
		if err := tx.Exec(`INSERT INTO iam_manual_identity_profiles(tenant_id,id,policy_reference,review_reference,witness_reference,independent_root_review_confirmed,display_name,observed_at,valid_from,valid_until,reason)
  VALUES(?,?,?,?,?,true,?,?,?,?,?)`, in.TenantID, in.SourceProfileID, in.PolicyReference, in.ReviewReference, in.WitnessReference, in.DisplayName, in.ObservedAt, in.ValidFrom, in.ValidUntil, in.Reason).Error; err != nil {
			return err
		}
		for _, person := range in.Roots {
			if err := tx.Exec("INSERT INTO iam_manual_identity_people(tenant_id,source_profile_id,id,source_person_reference,finding_reference) VALUES(?,?,?,?,?)", in.TenantID, in.SourceProfileID, person.PersonID, person.SourcePersonReference, person.FindingReference).Error; err != nil {
				return err
			}
			for _, account := range person.Accounts {
				if err := tx.Exec("INSERT INTO iam_manual_identity_bindings(tenant_id,source_profile_id,person_id,subject,membership_epoch) VALUES(?,?,?,?,?)", in.TenantID, in.SourceProfileID, person.PersonID, account.Subject, account.ExpectedMembershipEpoch).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Exec("INSERT INTO iam_manual_identity_source_heads(tenant_id,source_profile_id) VALUES(?,?)", in.TenantID, in.SourceProfileID).Error; err != nil {
			return err
		}
		if err := tx.Raw("INSERT INTO iam_manual_identity_imports(tenant_id,source_profile_id,intent_digest,actor_role,people,accounts) VALUES(?,?,?,?,?,?) RETURNING imported_at", in.TenantID, in.SourceProfileID, intent, operator.Role, len(in.Roots), len(accounts)).Scan(&receipt.ImportedAt).Error; err != nil {
			return err
		}
		receipt.SourceProfileID, receipt.People, receipt.Accounts, receipt.ClinicalAuthority = in.SourceProfileID, len(in.Roots), len(accounts), "not_evaluated"
		return tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'import','iam_manual_identity_imports',?)", in.TenantID, "operator:"+operator.Role, in.SourceProfileID).Error
	})
	if err != nil {
		return ManualIdentityImportReceipt{}, fmt.Errorf("import manual identity source: %w", err)
	}
	return receipt, nil
}
