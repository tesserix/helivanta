package workforce

import (
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func requireIndependentLeaveActor(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID, filer string) error {
	if filer == p.Subject {
		return errLeaveSelfDecision
	}
	// Link commands lock the staff row before changing a binding. The shared
	// lock keeps a new self-link from appearing until this decision commits.
	var person struct{ ID uuid.UUID }
	if err := tx.Table("workforce_staff").Select("id").Clauses(clause.Locking{Strength: "SHARE"}).
		Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, staffID).Take(&person).Error; err != nil {
		return err
	}
	var previous string
	if err := tx.Raw(`SELECT COALESCE(current_setting('app.workforce_login_link_access',true),'')`).Scan(&previous).Error; err != nil {
		return err
	}
	if err := tx.Exec(`SELECT set_config('app.workforce_login_link_access','authorized',true)`).Error; err != nil {
		return err
	}
	var link struct{ ID uuid.UUID }
	err := tx.Table("workforce_login_links").Select("id").Clauses(clause.Locking{Strength: "SHARE"}).
		Where("tenant_id=? AND hospital_id=? AND staff_id=? AND subject=? AND status='active'", p.TenantID, hospital, staffID, p.Subject).
		Take(&link).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if restore := tx.Exec(`SELECT set_config('app.workforce_login_link_access',?,true)`, previous).Error; restore != nil {
		return restore
	}
	if err == nil {
		return errLeaveSelfDecision
	}
	return nil
}
