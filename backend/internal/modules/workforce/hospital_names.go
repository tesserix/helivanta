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
			// The hospital row lock serialises replicas; the claim below is what decides, in one
			// statement that re-reads the marker, so a replica that waited here cannot repeat a version.
			err := tx.Raw(`SELECT name,name_version FROM workforce_hospitals WHERE tenant_id=? AND id=? AND establishment_kind='hospital' FOR UPDATE`, d.TenantID, d.ID).Scan(&row).Error
			if err != nil {
				return err
			}
			if row.NameVersion == 0 {
				return nil
			}
			claim := tx.Exec(`INSERT INTO workforce_hospital_name_announcements(tenant_id,hospital_id,announced_version) VALUES(?,?,?)
ON CONFLICT(tenant_id,hospital_id) DO UPDATE SET announced_version=EXCLUDED.announced_version,updated_at=clock_timestamp()
WHERE workforce_hospital_name_announcements.announced_version<EXCLUDED.announced_version`, d.TenantID, d.ID, row.NameVersion)
			if claim.Error != nil || claim.RowsAffected == 0 {
				return claim.Error // zero rows: this version was already announced
			}
			data, err := json.Marshal(workforcecontract.HospitalNamedData{HospitalID: d.ID.String(), Name: row.Name, NameVersion: row.NameVersion})
			if err != nil {
				return err
			}
			return events.Enqueue(tx, workforcecontract.SubjectHospitalNamed, events.Event{Type: workforcecontract.SubjectHospitalNamed, Version: 1, TenantID: d.TenantID.String(), Data: data})
		}); err != nil && !errors.Is(err, context.Canceled) {
			slog.WarnContext(ctx, "workforce hospital name announcement failed; retrying later", "tenant_id", d.TenantID, "hospital_id", d.ID, "error", err)
		}
	}
	return nil
}
