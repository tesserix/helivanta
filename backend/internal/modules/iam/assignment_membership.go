package iam

import (
	_ "embed"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed assignment_membership.sql
var assignmentMembershipSchema string

func proposalMembershipEpoch(tx *gorm.DB, tenant, subject string) (int64, error) {
	var row struct{ Epoch int64 }
	err := tx.Table("iam_subject_membership_epochs").Clauses(clause.Locking{Strength: "SHARE"}).Select("epoch").Where("tenant_id=? AND subject=? AND member_count>0", tenant, subject).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	return row.Epoch, err
}
