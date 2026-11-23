package pharmacy

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/authz"
)

var errStocktakeIndependence = errors.New("stocktake participant identity or independence changed")
var errStocktakeParticipantSource = errors.New("stocktake participant source unavailable")

type stocktakeParticipant struct {
	ActorSubject string
	StaffID      uuid.UUID
	StaffVersion int64
	LinkID       uuid.UUID
	LinkVersion  int64
}

func (h *catalogueHandlers) reviewStocktakeParticipants(tx *gorm.DB, use stocktakeWorkUse, take uuid.UUID) error {
	if use.hospital != nil {
		return h.reviewHospitalStocktakeParticipants(tx, use, take)
	}
	if h.staffHistory == nil {
		return errStocktakeParticipantSource
	}
	var original int64
	if err := tx.Table("pharmacy_stocktake_operating_evidence").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND id=? AND kind='start'", use.tenant, use.outlet, take, take).Count(&original).Error; err != nil {
		return err
	}
	if original != 1 {
		return errStocktakeIndependence
	}
	var after *stocktakeParticipant
	for {
		query := tx.Table("pharmacy_stocktake_operating_evidence").Distinct("actor_subject", "staff_id", "staff_version", "link_id", "link_version").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND kind IN('start','count','submit')", use.tenant, use.outlet, take)
		if after != nil {
			query = query.Where("(actor_subject,staff_id,staff_version,link_id,link_version)>(?,?,?,?,?)", after.ActorSubject, after.StaffID, after.StaffVersion, after.LinkID, after.LinkVersion)
		}
		var page []stocktakeParticipant
		if err := query.Order("actor_subject,staff_id,staff_version,link_id,link_version").Limit(200).Find(&page).Error; err != nil {
			return err
		}
		for _, person := range page {
			observation := authz.IndependentStaffObservation{Subject: person.ActorSubject, Reference: authz.IndependentStaffReference{StaffID: person.StaffID, StaffVersion: person.StaffVersion, LinkID: person.LinkID, LinkVersion: person.LinkVersion}}
			scope := authz.IndependentStaffScope{TenantID: use.tenant, EmployerKind: "pharmacy", EmployerID: use.pharmacy, Subject: use.subject}
			if err := h.staffHistory.WithIndependentStaffHistoryReview(tx.Statement.Context, scope, use.grant.Identity, observation, func(context.Context) error { return nil }); err != nil {
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
