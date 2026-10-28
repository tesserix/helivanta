package workforce

import (
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/authn"
)

func validateContactReferences(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, rows, previous []enrollmentContact, provided map[string]bool) error {
	if err := tx.Exec(`SELECT set_config('app.workforce_contact_master_references','supported',true)`).Error; err != nil {
		return err
	}
	previousIDs := make(map[string]string, len(previous))
	for _, row := range previous {
		previousIDs[row.ID] = row.RelationshipRevisionID
	}
	refs := make([]enrollmentMasterReference, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		refs = append(refs, enrollmentMasterReference{kind: "relationship", field: row.ID,
			id: &row.RelationshipRevisionID, name: &row.Relationship, previous: previousIDs[row.ID]})
	}
	if err := resolveMasterReferences(tx, p, hospital, refs, provided); err != nil {
		return err
	}
	for _, row := range rows {
		if utf8.RuneCountInString(row.Relationship) > enrollmentContactRelationshipMaxLength {
			return errEnrollmentFields
		}
	}
	return nil
}
