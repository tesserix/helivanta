package workforce

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type shiftDefinition struct {
	ID               uuid.UUID `json:"id"`
	RevisionID       uuid.UUID `json:"revision_id"`
	ShiftType        string    `json:"shift_type"`
	Code             string    `json:"code"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	Active           bool      `json:"active"`
	Version          int64     `json:"version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	StartTime        string    `json:"start_time"`
	EndTime          string    `json:"end_time"`
	BreakMinutes     int       `json:"break_minutes"`
	GraceMinutes     int       `json:"grace_minutes"`
	FullDayOTMinutes int       `json:"full_day_ot_minutes"`
	HalfDayOTMinutes int       `json:"half_day_ot_minutes"`
	Locum            bool      `json:"locum"`
	DurationMinutes  int       `json:"duration_minutes"`
}

func (shiftDefinition) TableName() string                 { return "workforce_shifts" }
func (m shiftDefinition) PageKey() (time.Time, uuid.UUID) { return m.CreatedAt, m.ID }

type shiftRevision struct {
	ID               uuid.UUID `json:"id"`
	ShiftID          uuid.UUID `json:"shift_id"`
	ShiftType        string    `json:"shift_type"`
	Code             string    `json:"code"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	Active           bool      `json:"active"`
	Version          int64     `json:"version"`
	CreatedAt        time.Time `json:"created_at"`
	StartTime        string    `json:"start_time"`
	EndTime          string    `json:"end_time"`
	BreakMinutes     int       `json:"break_minutes"`
	GraceMinutes     int       `json:"grace_minutes"`
	FullDayOTMinutes int       `json:"full_day_ot_minutes"`
	HalfDayOTMinutes int       `json:"half_day_ot_minutes"`
	Locum            bool      `json:"locum"`
	DurationMinutes  int       `json:"duration_minutes"`
}

func (shiftRevision) TableName() string                 { return "workforce_shift_revisions" }
func (m shiftRevision) PageKey() (time.Time, uuid.UUID) { return m.CreatedAt, m.ID }

type shiftInput struct {
	ExpectedVersion  *int64 `json:"expected_version" binding:"required,gte=0"`
	ShiftType        string `json:"shift_type" binding:"required,oneof=day evening night rotating on_call"`
	Code             string `json:"code" binding:"required,max=40"`
	Name             string `json:"name" binding:"required,max=80"`
	Description      string `json:"description" binding:"max=500"`
	Active           *bool  `json:"active" binding:"required"`
	StartTime        string `json:"start_time" binding:"required"`
	EndTime          string `json:"end_time" binding:"required"`
	BreakMinutes     *int   `json:"break_minutes" binding:"required,gte=0,lte=1439"`
	GraceMinutes     *int   `json:"grace_minutes" binding:"required,gte=0,lte=240"`
	FullDayOTMinutes *int   `json:"full_day_ot_minutes" binding:"required,gte=0,lte=1440"`
	HalfDayOTMinutes *int   `json:"half_day_ot_minutes" binding:"required,gte=0,lte=1440"`
	Locum            *bool  `json:"locum" binding:"required"`
}

var errShiftVersion = errors.New("shift version conflict")

func (h *handlers) listShifts(c *gin.Context, params pagination.Params) ([]shiftDefinition, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	kind := c.Query("shift_type")
	if kind != "" && !contains(kind, "day", "evening", "night", "rotating", "on_call") {
		respond.Error(c, 400, "invalid_request", "invalid shift type")
		return nil, nil
	}
	rows := []shiftDefinition{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		q := tenantdb.ApplyKeyset(tx, params).Where("tenant_id = ? AND hospital_id = ?", p.TenantID, hospital)
		if kind != "" {
			q = q.Where("shift_type = ?", kind)
		}
		return q.Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) shiftHistory(c *gin.Context, params pagination.Params) ([]shiftRevision, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "shift")
		return nil, nil
	}
	rows := []shiftRevision{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var master shiftDefinition
		if err := tx.Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id).First(&master).Error; err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Where("tenant_id = ? AND hospital_id = ? AND shift_id = ?", p.TenantID, hospital, id).Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) putShift(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "shift")
		return
	}
	var in shiftInput
	if !decode(c, &in) {
		return
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Description = strings.TrimSpace(in.Description)
	start, startErr := time.Parse("15:04", in.StartTime)
	end, endErr := time.Parse("15:04", in.EndTime)
	duration := (int(end.Sub(start).Minutes()) + 1440) % 1440
	if startErr != nil || endErr != nil || start.Format("15:04") != in.StartTime || end.Format("15:04") != in.EndTime || duration == 0 || *in.BreakMinutes >= duration || *in.HalfDayOTMinutes > *in.FullDayOTMinutes {
		respond.Error(c, 400, "invalid_request", "Use HH:MM times, a nonzero shift under 24 hours, a break shorter than the shift, and full-day OT minutes at least half-day OT minutes.")
		return
	}
	if !employeeCode.MatchString(in.Code) || in.Name == "" || strings.ContainsRune(in.Name, 0) || strings.ContainsRune(in.Description, 0) {
		respond.Error(c, 400, "invalid_request", "invalid Shift definition fields")
		return
	}
	var result shiftDefinition
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var grant struct{ Level string }
		if err := tx.Raw(`SELECT level FROM workforce_hospital_access WHERE tenant_id = ? AND hospital_id = ? AND subject = ? FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&grant).Error; err != nil {
			return err
		}
		if grant.Level != "manager" {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_shift','authorized',true),pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":"+hospital.String()+":shift:"+id.String()).Error; err != nil {
			return err
		}
		var old shiftDefinition
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id).First(&old).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		version := int64(1)
		if err == nil {
			if old.Code != in.Code {
				return errShiftVersion
			}
			if old.Name == in.Name && old.Description == in.Description && old.Active == *in.Active && old.ShiftType == in.ShiftType && old.StartTime == in.StartTime && old.EndTime == in.EndTime && old.BreakMinutes == *in.BreakMinutes && old.GraceMinutes == *in.GraceMinutes && old.FullDayOTMinutes == *in.FullDayOTMinutes && old.HalfDayOTMinutes == *in.HalfDayOTMinutes && old.Locum == *in.Locum {
				result = old
				return nil
			}
			if old.Version != *in.ExpectedVersion {
				return errShiftVersion
			}
			version = old.Version + 1
		} else if *in.ExpectedVersion != 0 {
			return gorm.ErrRecordNotFound
		}
		revision := uuid.New()

		if err := tx.Exec(`INSERT INTO workforce_shifts (tenant_id,hospital_id,id,shift_type,code,name,description,active,version,revision_id,start_time,end_time,break_minutes,grace_minutes,full_day_ot_minutes,half_day_ot_minutes,locum)
   VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT (tenant_id,hospital_id,id) DO UPDATE SET shift_type=EXCLUDED.shift_type,name=EXCLUDED.name,description=EXCLUDED.description,active=EXCLUDED.active,version=EXCLUDED.version,revision_id=EXCLUDED.revision_id,start_time=EXCLUDED.start_time,end_time=EXCLUDED.end_time,break_minutes=EXCLUDED.break_minutes,grace_minutes=EXCLUDED.grace_minutes,full_day_ot_minutes=EXCLUDED.full_day_ot_minutes,half_day_ot_minutes=EXCLUDED.half_day_ot_minutes,locum=EXCLUDED.locum,updated_at=clock_timestamp()`, p.TenantID, hospital, id, in.ShiftType, in.Code, in.Name, in.Description, *in.Active, version, revision, in.StartTime, in.EndTime, *in.BreakMinutes, *in.GraceMinutes, *in.FullDayOTMinutes, *in.HalfDayOTMinutes, *in.Locum).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_shift_revisions (tenant_id,hospital_id,id,shift_id,version,shift_type,code,name,description,active,start_time,end_time,break_minutes,grace_minutes,full_day_ot_minutes,half_day_ot_minutes,locum) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, revision, id, version, in.ShiftType, in.Code, in.Name, in.Description, *in.Active, in.StartTime, in.EndTime, *in.BreakMinutes, *in.GraceMinutes, *in.FullDayOTMinutes, *in.HalfDayOTMinutes, *in.Locum).Error; err != nil {
			return err
		}

		if err := tx.Exec(`INSERT INTO workforce_audit (tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES (?,?,?,'workforce.shift.changed',?,jsonb_build_object('version',?::bigint,'previous_version',?::bigint))`, p.TenantID, hospital, p.Subject, id, version, version-1).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.ShiftChangedData{HospitalID: hospital.String(), ShiftID: id.String(), RevisionID: revision.String(), Version: version})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, ShiftChanged, events.Event{Type: ShiftChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		return tx.Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id).First(&result).Error
	})
	if errors.Is(err, errShiftVersion) {
		respond.Conflict(c, "Shift definition changed since you opened it, or its code was changed. Reload before saving.")
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		respond.Conflict(c, "Code or name already exists in this hospital.")
		return
	}
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, result)
}
