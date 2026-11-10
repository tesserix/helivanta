package workforce

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const (
	hospitalNameAnnouncer = "system:workforce-hospital-name-announcer"
	hospitalNameBatch     = 200
)

//go:embed hospital_names.sql
var hospitalNamesSchema string

// RunHospitalNameAnnouncer announces unsent hospital names until ctx ends.
func RunHospitalNameAnnouncer(ctx context.Context, deps platform.Deps) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := AnnounceHospitalNamesOnce(ctx, deps.DB); err != nil {
				slog.WarnContext(ctx, "workforce hospital name announcement pass failed", "error", err)
			}
		}
	}
}

// AnnounceHospitalNamesOnce publishes each hospital's newest unsent name once; tests drive it instead of the ticker.
func AnnounceHospitalNamesOnce(ctx context.Context, db *tenantdb.DB) error {
	var due []struct {
		TenantID uuid.UUID
		ID       uuid.UUID
	}
	// The one cross-tenant read: ids of hospitals with an unsent version, never names.
	if err := db.WithAllTenants(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT h.tenant_id,h.id FROM workforce_hospitals h
LEFT JOIN workforce_hospital_name_announcements a ON a.tenant_id=h.tenant_id AND a.hospital_id=h.id
WHERE h.establishment_kind='hospital' AND (a.announced_version IS NULL OR a.announced_version<h.name_version)
ORDER BY h.tenant_id,h.id LIMIT ?`, hospitalNameBatch).Scan(&due).Error
	}); err != nil {
		return err
	}
	ctx = tenantdb.WithActor(ctx, hospitalNameAnnouncer)
	for _, d := range due {
		if err := db.WithTenant(ctx, d.TenantID.String(), func(tx *gorm.DB) error {
			var row struct {
				Name        string
				NameVersion int64
			}
			// Locked and re-checked: the name may have moved or been announced since the scan.
			err := tx.Raw(`SELECT h.name,h.name_version FROM workforce_hospitals h
LEFT JOIN workforce_hospital_name_announcements a ON a.tenant_id=h.tenant_id AND a.hospital_id=h.id
WHERE h.tenant_id=? AND h.id=? AND h.establishment_kind='hospital' AND (a.announced_version IS NULL OR a.announced_version<h.name_version)
FOR UPDATE OF h`, d.TenantID, d.ID).Scan(&row).Error
			if err != nil {
				return err
			}
			if row.NameVersion == 0 {
				return nil
			}
			data, err := json.Marshal(workforcecontract.HospitalNamedData{HospitalID: d.ID.String(), Name: row.Name, NameVersion: row.NameVersion})
			if err != nil {
				return err
			}
			if err := events.Enqueue(tx, workforcecontract.SubjectHospitalNamed, events.Event{Type: workforcecontract.SubjectHospitalNamed, Version: 1, TenantID: d.TenantID.String(), Data: data}); err != nil {
				return err
			}
			return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "hospital_id"}}, DoUpdates: clause.Assignments(map[string]any{"announced_version": row.NameVersion, "updated_at": gorm.Expr("clock_timestamp()")})}).
				Table("workforce_hospital_name_announcements").Create(map[string]any{"tenant_id": d.TenantID, "hospital_id": d.ID, "announced_version": row.NameVersion}).Error
		}); err != nil && !errors.Is(err, context.Canceled) {
			slog.WarnContext(ctx, "workforce hospital name announcement failed; retrying later", "tenant_id", d.TenantID, "hospital_id", d.ID, "error", err)
		}
	}
	return nil
}
