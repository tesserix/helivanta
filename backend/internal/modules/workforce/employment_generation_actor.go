package workforce

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func readProtectedGenerationState(tx *gorm.DB, p authn.Principal, hospital, staff, link, id uuid.UUID) (generation employmentGenerationRecord, state string, until time.Time, err error) {
	keys := []string{"app.workforce_hr_intake", "app.workforce_hr_intake_actor", "app.workforce_hr_intake_link", "app.workforce_enrollment", "app.workforce_employment_review", "app.workforce_employment_review_id", "app.workforce_employment_evidence", "app.workforce_employment_person_review", "app.workforce_identity_source", "app.workforce_identity_staff", "app.workforce_person_review_documents", "app.workforce_generation", "app.workforce_generation_staff"}
	names, err := json.Marshal(keys)
	if err != nil {
		return generation, "", until, err
	}
	var settings []struct{ Key, Value string }
	if err = tx.Raw(`SELECT key,COALESCE(current_setting(key,true),'') AS value FROM jsonb_array_elements_text(?::jsonb) AS key`, string(names)).Scan(&settings).Error; err != nil {
		return generation, "", until, err
	}
	previous := map[string]string{}
	for _, setting := range settings {
		previous[setting.Key] = setting.Value
	}
	restore, err := json.Marshal(previous)
	if err != nil {
		return generation, "", until, err
	}
	defer func() {
		err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true) FROM jsonb_each_text(?::jsonb)`, string(restore)).Error)
	}()
	// The owner reads only this selected basis and restores purpose scope before caller work.
	if err = tx.Exec(`SELECT set_config('app.workforce_hr_intake','authorized',true),set_config('app.workforce_hr_intake_actor',?,true),set_config('app.workforce_hr_intake_link',?,true),set_config('app.workforce_enrollment','authorized',true)`, staff.String(), link.String()).Error; err != nil {
		return generation, "", until, err
	}
	generation, err = loadEmploymentGeneration(tx, p, hospital, staff, id)
	if err != nil {
		return generation, "", until, err
	}
	var source IdentitySource
	source, err = loadIdentitySource(tx, p, hospital, staff)
	if err != nil {
		return generation, "", until, err
	}
	state, until, err = employmentGenerationState(tx, p, source, generation)
	return
}

func validateGenerationActor(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, actor reviewedLeaveActor) error {
	if actor.GenerationID == uuid.Nil {
		return nil
	}
	generation, state, _, err := readProtectedGenerationState(tx, p, hospital, actor.StaffID, actor.ID, actor.GenerationID)
	if err != nil {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	if state != "effective" || generation.AccessIntent != "digital" || generation.DeclaredSubject != p.Subject {
		return authz.ErrScopedCommandDenied
	}
	return nil
}
