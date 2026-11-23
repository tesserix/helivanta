package pharmacy

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/authz"
)

type hospitalStocktakeParticipant struct {
	ActorSubject    string
	StaffID, LinkID uuid.UUID
	LinkVersion     int64
	GenerationID    uuid.UUID
	EmploymentEpoch int64
}

func (h *catalogueHandlers) reviewHospitalStocktakeParticipants(tx *gorm.DB, use stocktakeWorkUse, take uuid.UUID) error {
	if h.stocktakeActors == nil || use.hospital == nil {
		return errStocktakeParticipantSource
	}
	var original int64
	if err := tx.Table(use.evidenceTable()).Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND id=? AND kind='start'", use.tenant, use.outlet, take, take).Count(&original).Error; err != nil {
		return err
	}
	if original != 1 {
		return errStocktakeIndependence
	}
	var after *hospitalStocktakeParticipant
	for {
		query := tx.Table(use.evidenceTable()).Distinct("actor_subject", "staff_id", "link_id", "link_version", "generation_id", "employment_epoch").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND kind IN('start','count','submit')", use.tenant, use.outlet, take)
		if after != nil {
			query = query.Where("(actor_subject,staff_id,link_id,link_version,generation_id,employment_epoch)>(?,?,?,?,?,?)", after.ActorSubject, after.StaffID, after.LinkID, after.LinkVersion, after.GenerationID, after.EmploymentEpoch)
		}
		var page []hospitalStocktakeParticipant
		if err := query.Order("actor_subject,staff_id,link_id,link_version,generation_id,employment_epoch").Limit(200).Find(&page).Error; err != nil {
			return err
		}
		for _, person := range page {
			observation := authz.StocktakeActorObservation{Subject: person.ActorSubject, Reference: authz.StaffActorReference{StaffID: person.StaffID, LinkID: person.LinkID, LinkVersion: person.LinkVersion, GenerationID: person.GenerationID, EmploymentEpoch: person.EmploymentEpoch}}
			scope := authz.StaffActorScope{TenantID: use.tenant, Subject: use.subject, HospitalID: use.hospital.hospital, DepartmentPath: use.hospital.path}
			if err := h.stocktakeActors.WithStocktakeActorHistoryReview(tx.Statement.Context, scope, use.hospital.actor, observation, func(context.Context) error { return nil }); err != nil {
				if errors.Is(err, authz.ErrScopedCommandDenied) {
					return errors.Join(errStocktakeIndependence, err)
				}
				return errors.Join(errStocktakeParticipantSource, err)
			}
		}
		if len(page) < 200 {
			return nil
		}
		last := page[len(page)-1]
		after = &last
	}
}
