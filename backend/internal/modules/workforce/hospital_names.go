package workforce

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
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

// RunHospitalNameAnnouncer announces unsent hospital names, and re-sends unconfirmed ones, until ctx ends.
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

// AnnounceHospitalNamesOnce publishes each hospital's newest unsent name once and re-sends a due unconfirmed
// one; tests drive it instead of the ticker.
func AnnounceHospitalNamesOnce(ctx context.Context, db *tenantdb.DB) error {
	return announceHospitalNames(ctx, db, hospitalNameBatch)
}

func announceHospitalNames(ctx context.Context, db *tenantdb.DB, batch int) error {
	var due []struct {
		TenantID uuid.UUID
		ID       uuid.UUID
	}
	// The one cross-tenant read: ids of hospitals with an unsent version or an unconfirmed one due
	// again, never names. Unsent versions sort first so a backlog of re-sends cannot starve them.
	if err := db.WithAllTenants(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT h.tenant_id,h.id FROM workforce_hospitals h
LEFT JOIN workforce_hospital_name_announcements a ON a.tenant_id=h.tenant_id AND a.hospital_id=h.id
WHERE h.establishment_kind='hospital' AND (a.announced_version IS NULL OR a.announced_version<h.name_version OR a.next_attempt_at<=now())
ORDER BY (a.announced_version IS NULL OR a.announced_version<h.name_version) DESC,a.next_attempt_at NULLS FIRST,h.tenant_id,h.id LIMIT ?`, batch).Scan(&due).Error
	}); err != nil {
		return fmt.Errorf("scan due hospital names: %w", err)
	}
	ctx = tenantdb.WithActor(ctx, hospitalNameAnnouncer)
	for _, d := range due {
		// Set inside the transaction, logged only once it commits: a rolled-back re-send was never sent.
		var attempt int
		var nameVersion int64
		if err := db.WithTenant(ctx, d.TenantID.String(), func(tx *gorm.DB) error {
			var row struct {
				Name        string
				NameVersion int64
			}
			// The hospital row lock serialises replicas; the claim below is what decides, in one
			// statement that re-reads the marker, so a replica that waited here cannot repeat a version.
			err := tx.Raw(`SELECT name,name_version FROM workforce_hospitals WHERE tenant_id=? AND id=? AND establishment_kind='hospital' FOR UPDATE`, d.TenantID, d.ID).Scan(&row).Error
			if err != nil {
				return fmt.Errorf("lock hospital: %w", err)
			}
			if row.NameVersion == 0 {
				return nil
			}
			claim := tx.Exec(`INSERT INTO workforce_hospital_name_announcements(tenant_id,hospital_id,announced_version,attempts,next_attempt_at) VALUES(?,?,?,0,clock_timestamp()+make_interval(secs => ?))
ON CONFLICT(tenant_id,hospital_id) DO UPDATE SET announced_version=EXCLUDED.announced_version,attempts=0,next_attempt_at=EXCLUDED.next_attempt_at,updated_at=clock_timestamp()
WHERE workforce_hospital_name_announcements.announced_version<EXCLUDED.announced_version`, d.TenantID, d.ID, row.NameVersion, firstRedelivery.Seconds())
			if claim.Error != nil {
				return fmt.Errorf("claim hospital name version: %w", claim.Error)
			}
			attempt = 0
			if claim.RowsAffected == 0 {
				// This version was already announced. Re-send it only if still unconfirmed and due,
				// re-read under the lock: a confirmation or another replica's re-send may have landed
				// since the scan. announced_version=name_version makes the current name that version's.
				var attempts []int
				if err := tx.Raw(`SELECT attempts FROM workforce_hospital_name_announcements WHERE tenant_id=? AND hospital_id=? AND announced_version=? AND next_attempt_at<=now() FOR UPDATE`, d.TenantID, d.ID, row.NameVersion).Scan(&attempts).Error; err != nil {
					return fmt.Errorf("re-check unconfirmed hospital name: %w", err)
				} else if len(attempts) == 0 {
					return nil
				}
				attempt = attempts[0] + 1
				if err := tx.Exec(`UPDATE workforce_hospital_name_announcements SET attempts=attempts+1,next_attempt_at=clock_timestamp()+make_interval(secs => ?),updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=?`,
					redeliveryDelay(attempt).Seconds(), d.TenantID, d.ID).Error; err != nil {
					return fmt.Errorf("schedule hospital name re-send: %w", err)
				}
			}
			data, err := json.Marshal(workforcecontract.HospitalNamedData{HospitalID: d.ID.String(), Name: row.Name, NameVersion: row.NameVersion})
			if err != nil {
				return fmt.Errorf("encode hospital name: %w", err)
			}
			if err := events.Enqueue(tx, workforcecontract.SubjectHospitalNamed, events.Event{Type: workforcecontract.SubjectHospitalNamed, Version: 1, TenantID: d.TenantID.String(), Data: data}); err != nil {
				return fmt.Errorf("enqueue hospital name: %w", err)
			}
			nameVersion = row.NameVersion
			return nil
		}); err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.WarnContext(ctx, "workforce hospital name announcement failed; retrying later", "tenant_id", d.TenantID, "hospital_id", d.ID, "error", err)
			}
		} else if attempt > 0 {
			slog.WarnContext(ctx, "re-sent unconfirmed hospital name", "tenant_id", d.TenantID, "hospital_id", d.ID, "name_version", nameVersion, "attempt", attempt)
		}
	}
	return nil
}
