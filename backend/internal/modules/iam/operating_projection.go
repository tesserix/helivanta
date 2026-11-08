package iam

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type operatingGraph interface {
	ProjectOperatingAssignment(context.Context, authz.OperatingAssignment) error
	CheckOperatingAssignment(context.Context, authz.OperatingAssignment, string, authz.Permission) (bool, error)
}

func operatingProjectionData(evt events.Event) (iamcontract.OperatingDutyChangedData, error) {
	var data iamcontract.OperatingDutyChangedData
	tenant, err := uuid.Parse(evt.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != evt.TenantID || evt.Type != iamcontract.SubjectOperatingDutyChanged || evt.Version != 1 || evt.DestinationTenantID != "" {
		return data, errors.New("invalid operating projection envelope")
	}
	if err := json.Unmarshal(evt.Data, &data); err != nil {
		return data, errors.New("invalid operating projection body")
	}
	id, err := uuid.Parse(data.DutyID)
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != data.DutyID || data.Version < 1 || data.Version > 9007199254740991 {
		return data, errors.New("invalid operating projection identity")
	}
	return data, nil
}
func operatingProjectionConsumer(deps platform.Deps) events.Consumer {
	h := &operatingHandlers{db: deps.DB, bus: deps.Bus, staff: deps.IndependentStaff, locations: deps.OperatingLocations}
	graph, _ := deps.Authz.(operatingGraph)
	return events.Consumer{Subject: iamcontract.SubjectOperatingDutyChanged, Name: "iam-operating-projection", Handle: func(_ context.Context, _ *gorm.DB, evt events.Event) error {
		_, err := operatingProjectionData(evt)
		return err
	}, AfterCommit: func(ctx context.Context, evt events.Event) error {
		data, err := operatingProjectionData(evt)
		if err != nil {
			return err
		}
		return h.projectOperating(ctx, evt.TenantID, uuid.MustParse(data.DutyID), data.Version, graph)
	}}
}
func (h *operatingHandlers) projectOperating(ctx context.Context, tenant string, id uuid.UUID, version int64, graph operatingGraph) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ctx = tenantdb.WithActor(ctx, "service:iam-operating-projector")
	var snapshot authz.OperatingAssignment
	pending := false
	err := h.db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true)`).Error; err != nil {
			return err
		}
		row, err := loadOperatingDuty(tx, tenant, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.Status != "approved_pending_activation" || row.Version != version {
			return nil
		}
		var stored struct{ Document json.RawMessage }
		if err := tx.Table("iam_operating_snapshots").Select("document").Where("tenant_id=? AND duty_id=? AND version=? AND authority_epoch=?", tenant, id, version, row.AuthorityEpoch).Take(&stored).Error; err != nil {
			return err
		}
		if err := json.Unmarshal(stored.Document, &snapshot); err != nil {
			return err
		}
		pending = true
		return nil
	})
	if err != nil || !pending {
		return err
	}
	if graph == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if err := graph.ProjectOperatingAssignment(ctx, snapshot); err != nil {
		return err
	}
	for _, permission := range snapshot.Permissions {
		allowed, err := graph.CheckOperatingAssignment(ctx, snapshot, snapshot.Subject, permission)
		if err != nil {
			return err
		}
		if !allowed {
			return authz.ErrScopedAuthorizationUnavailable
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	return h.db.WithGuardedTenantCommand(ctx, tenant, deadline, func(tx *gorm.DB) error {
		return tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true),set_config('app.iam_operating_command','project',true)`).Error
	}, func(commandCtx context.Context) error {
		return h.db.WithTenantCommand(commandCtx, tenant, func(tx *gorm.DB) error {
			var row operatingDutyRow
			if err := tx.Table("iam_operating_duties").Select("tenant_id,id,input,prepared_by,status,membership_epoch,version,authority_epoch,created_at,updated_at").Where("tenant_id=? AND id=?", tenant, id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			} else if err != nil {
				return err
			}
			if row.Status != "approved_pending_activation" || row.Version != snapshot.Version || row.AuthorityEpoch != snapshot.ScopeEpoch {
				return nil
			}
			view, err := operatingDutyView(row, false)
			if err != nil {
				return err
			}
			revoke := func(reason string) error {
				if err := tx.Exec(`SELECT set_config('app.iam_operating_command','revoke',true)`).Error; err != nil {
					return err
				}
				if err := tx.Exec(`UPDATE iam_operating_duties SET status='revoked',version=version+1,authority_epoch=authority_epoch+1,actor_subject=?,reason=? WHERE tenant_id=? AND id=? AND version=?`, tenantdb.ActorFrom(commandCtx), reason, tenant, id, row.Version).Error; err != nil {
					return err
				}
				return h.publish(tx, tenant, id, row.Version+1)
			}
			if !view.Input.ExpiresAt.After(time.Now()) {
				return revoke("Operating duty expired before projection acknowledgement")
			}
			err = h.withCurrentBasis(commandCtx, authn.Principal{TenantID: tenant, Subject: tenantdb.ActorFrom(commandCtx)}, row, view.Input, false, func(authz.OperatingAssignment) error {
				if err := tx.Exec(`INSERT INTO iam_operating_projection_acks(tenant_id,duty_id,version,authority_epoch)VALUES(?,?,?,?)ON CONFLICT DO NOTHING`, tenant, id, row.Version, row.AuthorityEpoch).Error; err != nil {
					return err
				}
				if err := tx.Exec(`UPDATE iam_operating_duties SET status='active',version=version+1,actor_subject=?,reason=? WHERE tenant_id=? AND id=? AND version=?`, tenantdb.ActorFrom(commandCtx), "Acknowledged exact operating projection", tenant, id, row.Version).Error; err != nil {
					return err
				}
				return h.publish(tx, tenant, id, row.Version+1)
			})
			if errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, errProposalConflict) {
				return revoke("Operating source or Staff identity changed before projection acknowledgement")
			}
			return err
		})
	})
}
