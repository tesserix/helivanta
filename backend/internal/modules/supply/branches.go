package supply

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type premiseRequest struct {
	premiseInput
	branchProvided bool
}

func (in *premiseRequest) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in.premiseInput); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	_, in.branchProvided = fields["branch_premise_id"]
	return nil
}

var errBranchKind = errors.New("only storage premises can have a branch")

func lockPremiseStructure(tx *gorm.DB, p authn.Principal, partyID uuid.UUID) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":supply-structure:"+partyID.String()).Error
}

func validateBranch(tx *gorm.DB, p authn.Principal, partyID, id uuid.UUID, previous *uuid.UUID, fields PremiseFields) error {
	if fields.BranchPremiseID == nil {
		return nil
	}
	if fields.Kind != "warehouse" && fields.Kind != "depot" {
		return errBranchKind
	}
	if *fields.BranchPremiseID == id {
		return gorm.ErrRecordNotFound
	}
	// A location link never widens access beyond the current organisation grant.
	var owner party
	if err := tx.Select("id").Where("tenant_id=? AND id=? AND record_kind='owned_organisation'", p.TenantID, partyID).First(&owner).Error; err != nil {
		return err
	}
	var parent premise
	query := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("tenant_id=? AND party_id=? AND id=? AND country_code=? AND kind='branch'", p.TenantID, partyID, *fields.BranchPremiseID, fields.CountryCode)
	if previous == nil || *previous != *fields.BranchPremiseID {
		query = query.Where("status='active'")
	}
	return query.First(&parent).Error
}
