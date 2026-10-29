package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
)

// Ward medicine billed to the admission occupying a bed (#1387). Pharmacy
// keeps who occupies which bed from MediCore's occupancy events; a bed or
// room resolves to its one open stay, at a hospital that has verified the
// pharmacy, and the sale is posted to that admission.

var errStayEnded = errors.New("the stay is no longer open")

// admittedStay is the open stay a sale is billed to.
type admittedStay struct {
	OccupancyID uuid.UUID
	AdmissionID uuid.UUID
	PatientID   uuid.UUID
	HospitalID  uuid.UUID
	BedCode     string
	ResolvedAt  time.Time
}

// The nil-safe accessors write a sale with no admission as SQL NULLs.
func (s *admittedStay) admissionID() *uuid.UUID {
	if s == nil {
		return nil
	}
	return &s.AdmissionID
}
func (s *admittedStay) occupancyID() *uuid.UUID {
	if s == nil {
		return nil
	}
	return &s.OccupancyID
}
func (s *admittedStay) bedCode() *string {
	if s == nil {
		return nil
	}
	return &s.BedCode
}
func (s *admittedStay) resolvedAt() *time.Time {
	if s == nil {
		return nil
	}
	return &s.ResolvedAt
}

func transactionTime(tx *gorm.DB) (time.Time, error) {
	var at struct{ At time.Time }
	// The transaction's time, so the sale's or credit note's created_at and
	// the event that announces it agree.
	err := tx.Raw(`SELECT now() AS at`).Scan(&at).Error
	return at.At, err
}

// resolveStay re-reads the stay a sale names, under a lock, and refuses one
// that has ended or never started. The sale's insert trigger checks it again.
func resolveStay(tx *gorm.DB, tenant string, outlet, occupancy uuid.UUID) (*admittedStay, error) {
	var rows []struct {
		AdmissionID uuid.UUID
		PatientID   uuid.UUID
		HospitalID  uuid.UUID
		BedCode     string
	}
	if err := tx.Raw(`SELECT admission_id,patient_id,hospital_id,bed_code FROM pharmacy_bed_occupancies
 WHERE tenant_id=? AND occupancy_id=? AND started_at IS NOT NULL AND ended_at IS NULL FOR SHARE`, tenant, occupancy).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, errStayEnded
	}
	at, err := transactionTime(tx)
	if err != nil {
		return nil, err
	}
	r := rows[0]
	return &admittedStay{OccupancyID: occupancy, AdmissionID: r.AdmissionID, PatientID: r.PatientID, HospitalID: r.HospitalID, BedCode: r.BedCode, ResolvedAt: at}, nil
}

// occupancyConsumers keep pharmacy_bed_occupancies. Started and ended may
// arrive in either order; each fills only its own half, once.
func occupancyConsumers() []events.Consumer {
	started := medicorecontract.SubjectBedOccupancyStarted
	ended := medicorecontract.SubjectBedOccupancyEnded
	return []events.Consumer{{Name: "pharmacy-bed-occupancy-started", Subject: started, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d medicorecontract.BedOccupancyStartedData
		if evt.Type != "BedOccupancyStarted" || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
			return errors.New("invalid bed occupancy event")
		}
		ids, ok := parseIDs(d.OccupancyID, d.AdmissionID, d.HospitalID, d.BedID, d.PatientID)
		at, err := time.Parse(time.RFC3339Nano, d.StartedAt)
		if !ok || err != nil || d.WardCode == "" || d.RoomCode == "" || d.BedCode == "" {
			return errors.New("invalid bed occupancy event")
		}
		if err := tx.Exec(`INSERT INTO pharmacy_bed_occupancies (tenant_id,occupancy_id,admission_id,hospital_id,bed_id,patient_id,ward_code,room_code,bed_code,started_at)
 VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT (tenant_id,occupancy_id) DO UPDATE SET patient_id=EXCLUDED.patient_id,ward_code=EXCLUDED.ward_code,
 room_code=EXCLUDED.room_code,bed_code=EXCLUDED.bed_code,started_at=EXCLUDED.started_at WHERE pharmacy_bed_occupancies.started_at IS NULL`,
			evt.TenantID, ids[0], ids[1], ids[2], ids[3], ids[4], d.WardCode, d.RoomCode, d.BedCode, at).Error; err != nil {
			return err
		}
		// A replay must say what was recorded; a different start is refused.
		// Compared at the database's microsecond precision: an event may carry
		// nanoseconds Postgres never stored.
		var held struct {
			AdmissionID, HospitalID, BedID, PatientID uuid.UUID
			BedCode                                   string
			StartedAt                                 time.Time
		}
		if err := tx.Raw(`SELECT admission_id,hospital_id,bed_id,patient_id,bed_code,started_at FROM pharmacy_bed_occupancies WHERE tenant_id=? AND occupancy_id=?`, evt.TenantID, ids[0]).Scan(&held).Error; err != nil {
			return err
		}
		if held.AdmissionID != ids[1] || held.HospitalID != ids[2] || held.BedID != ids[3] || held.PatientID != ids[4] || held.BedCode != d.BedCode || !held.StartedAt.Equal(at.Truncate(time.Microsecond)) {
			return errors.New("a replayed bed occupancy differs from the one recorded")
		}
		return nil
	}}, {Name: "pharmacy-bed-occupancy-ended", Subject: ended, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d medicorecontract.BedOccupancyEndedData
		if evt.Type != "BedOccupancyEnded" || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
			return errors.New("invalid bed occupancy event")
		}
		ids, ok := parseIDs(d.OccupancyID, d.AdmissionID, d.HospitalID, d.BedID)
		at, err := time.Parse(time.RFC3339Nano, d.EndedAt)
		if !ok || err != nil {
			return errors.New("invalid bed occupancy event")
		}
		return tx.Exec(`INSERT INTO pharmacy_bed_occupancies (tenant_id,occupancy_id,admission_id,hospital_id,bed_id,ended_at)
 VALUES (?,?,?,?,?,?) ON CONFLICT (tenant_id,occupancy_id) DO UPDATE SET ended_at=EXCLUDED.ended_at WHERE pharmacy_bed_occupancies.ended_at IS NULL`,
			evt.TenantID, ids[0], ids[1], ids[2], ids[3], at).Error
	}}}
}

func parseIDs(values ...string) ([]uuid.UUID, bool) {
	out := make([]uuid.UUID, len(values))
	for i, v := range values {
		id, err := uuid.Parse(v)
		if err != nil || id == uuid.Nil {
			return nil, false
		}
		out[i] = id
	}
	return out, true
}

var locationCode = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,19}$`)

type stayView struct {
	OccupancyID uuid.UUID `json:"occupancy_id"`
	AdmissionID uuid.UUID `json:"admission_id"`
	PatientID   uuid.UUID `json:"patient_id"`
	HospitalID  uuid.UUID `json:"hospital_id"`
	WardCode    string    `json:"ward_code"`
	RoomCode    string    `json:"room_code"`
	BedCode     string    `json:"bed_code"`
}

func admissionRoutes(g *platform.Router, deps platform.Deps) {
	h := &catalogueHandlers{db: deps.DB, bus: deps.Bus}
	g.GET("/pharmacies/:pharmacy/outlets/:outlet/admission-occupant", PermSaleCreate, h.admissionOccupant)
}

// admissionOccupant resolves a bed or room code to its one open stay, at a
// hospital that has verified this outlet's pharmacy. An empty bed or room is
// no_occupant; a room with several occupied beds is ambiguous, never a guess.
func (h *catalogueHandlers) admissionOccupant(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	hospital, err := uuid.Parse(c.Query("hospital_id"))
	bed, room := c.Query("bed_code"), c.Query("room_code")
	column, code := "bed_code", bed
	if room != "" {
		column, code = "room_code", room
	}
	if err != nil || hospital == uuid.Nil || (bed == "") == (room == "") || !locationCode.MatchString(code) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "hospital_id and exactly one of bed_code or room_code are required")
		return
	}
	var found []stayView
	err = h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var verified int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_outlets o JOIN pharmacy_hospital_affiliations a ON a.tenant_id=o.tenant_id AND a.pharmacy_id=o.pharmacy_id
 WHERE o.tenant_id=? AND o.id=? AND a.hospital_id=?
  AND (SELECT v.kind FROM pharmacy_affiliation_verifications v WHERE v.tenant_id=a.tenant_id AND v.affiliation_id=a.id ORDER BY v.sequence DESC LIMIT 1)='accepted'`,
			p.TenantID, outlet, hospital).Scan(&verified).Error; err != nil {
			return err
		}
		if verified == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Raw(`SELECT occupancy_id,admission_id,patient_id,hospital_id,ward_code,room_code,bed_code FROM pharmacy_bed_occupancies
 WHERE tenant_id=? AND hospital_id=? AND `+column+`=? AND started_at IS NOT NULL AND ended_at IS NULL ORDER BY occupancy_id LIMIT 2`, p.TenantID, hospital, code).Scan(&found).Error; err != nil {
			return err
		}
		// Who occupies a bed is a sensitive read: every resolve is audited,
		// a miss included, against the stay found or else the hospital.
		resource := hospital
		if len(found) == 1 {
			resource = found[0].OccupancyID
		}
		return tx.Exec(`INSERT INTO audit_events (tenant_id,actor_subject,action,resource_type,resource_id) VALUES (?,?,'read','pharmacy_admission_resolve',?)`,
			p.TenantID, p.Subject, resource).Error
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "hospital")
	case err != nil:
		catalogueError(c, err)
	case len(found) == 0:
		respond.Error(c, http.StatusNotFound, "no_occupant", "no one occupies it now")
	case len(found) > 1:
		respond.Error(c, http.StatusConflict, "ambiguous", "more than one bed in the room is occupied; give the bed")
	default:
		respond.OK(c, found[0])
	}
}
