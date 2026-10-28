package workforce

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/authn"
)

func validateQualificationReferences(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, rows, previous []enrollmentQualification, provided map[string]bool) error {
	if err := tx.Exec(`SELECT set_config('app.workforce_qualification_master_references','supported',true)`).Error; err != nil {
		return err
	}
	previousIDs := make(map[string]string, len(previous))
	for _, row := range previous {
		previousIDs[row.ID] = row.QualificationRevisionID
	}
	refs := make([]enrollmentMasterReference, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		refs = append(refs, enrollmentMasterReference{kind: "qualification", field: row.ID,
			id: &row.QualificationRevisionID, name: &row.Qualification, previous: previousIDs[row.ID]})
	}
	return resolveMasterReferences(tx, p, hospital, refs, provided)
}
