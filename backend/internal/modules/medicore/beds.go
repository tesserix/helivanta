package medicore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Beds, admissions and who occupies a bed (#106 slice 1). A command locks
// the beds it moves, in id order, checks the caller's expected versions,
// writes occupancy and transition rows and publishes, in one transaction.

var (
	errNoBed           = errors.New("bed not found")
	errNoRoom          = errors.New("room not found")
	errNoAdmission     = errors.New("admission not found")
	errBedNotAvailable = errors.New("the bed is not available")
	errStaleBed        = errors.New("the bed changed; reload it and try again")
	errAlreadyAdmitted = errors.New("the patient already has an active admission at this hospital")
	errNotAdmitted     = errors.New("the admission is not in a bed")
	errBedMove         = errors.New("the bed cannot make that move from its current state")
	errNoOccupant      = errors.New("no one occupies it now")
	errAmbiguousRoom   = errors.New("more than one bed in the room is occupied")
	codePattern        = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,19}$`)
)

type ward struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"-"`
	HospitalID uuid.UUID `json:"hospital_id"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	CreatedBy  string    `json:"-"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime:false;default:clock_timestamp()"`
}

func (ward) TableName() string                 { return "medicore_wards" }
func (w ward) PageKey() (time.Time, uuid.UUID) { return w.CreatedAt, w.ID }

type room struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"-"`
	HospitalID uuid.UUID `json:"hospital_id"`
	WardID     uuid.UUID `json:"ward_id"`
	Code       string    `json:"code"`
	CreatedBy  string    `json:"-"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime:false;default:clock_timestamp()"`
}

func (room) TableName() string                 { return "medicore_rooms" }
func (r room) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

// bedView is a bed on the board, with its current occupancy if any.
type bedView struct {
	ID          uuid.UUID  `json:"id"`
	HospitalID  uuid.UUID  `json:"hospital_id"`
	WardID      uuid.UUID  `json:"ward_id"`
	WardCode    string     `json:"ward_code"`
	RoomID      uuid.UUID  `json:"room_id"`
	RoomCode    string     `json:"room_code"`
	Code        string     `json:"code"`
	BedClass    string     `json:"bed_class"`
	State       string     `json:"state"`
	Version     int        `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	OccupancyID *uuid.UUID `json:"occupancy_id"`
	AdmissionID *uuid.UUID `json:"admission_id"`
	PatientID   *uuid.UUID `json:"patient_id"`
	OccupiedAt  *time.Time `json:"occupied_since"`
}

func (b bedView) PageKey() (time.Time, uuid.UUID) { return b.CreatedAt, b.ID }

const bedViewSelect = `SELECT b.id,b.hospital_id,b.ward_id,w.code AS ward_code,b.room_id,r.code AS room_code,b.code,b.bed_class,b.state,b.version,b.created_at,
 o.id AS occupancy_id,o.admission_id,a.patient_id,o.started_at AS occupied_at
 FROM medicore_beds b JOIN medicore_wards w ON w.tenant_id=b.tenant_id AND w.id=b.ward_id JOIN medicore_rooms r ON r.tenant_id=b.tenant_id AND r.id=b.room_id
 LEFT JOIN medicore_bed_occupancies o ON o.tenant_id=b.tenant_id AND o.bed_id=b.id AND o.ended_at IS NULL
 LEFT JOIN medicore_admissions a ON a.tenant_id=o.tenant_id AND a.id=o.admission_id`

type admission struct {
	ID            uuid.UUID  `json:"id"`
	TenantID      uuid.UUID  `json:"-"`
	HospitalID    uuid.UUID  `json:"hospital_id"`
	PatientID     uuid.UUID  `json:"patient_id"`
	Intent        string     `json:"intent"`
	Status        string     `json:"status"`
	IntentSHA256  string     `json:"-" gorm:"column:intent_sha256"`
	AdmittedBy    string     `json:"admitted_by"`
	AdmittedAt    time.Time  `json:"admitted_at" gorm:"autoCreateTime:false;default:clock_timestamp()"`
	ReleaseReason *string    `json:"release_reason"`
	ReleaseNote   *string    `json:"release_note"`
	ReleasedBy    *string    `json:"released_by"`
	ReleasedAt    *time.Time `json:"released_at"`
	Occupancies   []stay     `json:"occupancies" gorm:"-"`
}

func (admission) TableName() string { return "medicore_admissions" }

// stay is one occupancy interval of an admission.
type stay struct {
	ID           uuid.UUID  `json:"id"`
	BedID        uuid.UUID  `json:"bed_id"`
	StartedAt    time.Time  `json:"started_at"`
	StartedBy    string     `json:"started_by"`
	EndedAt      *time.Time `json:"ended_at"`
	EndedBy      *string    `json:"ended_by"`
	EndedByCause *string    `json:"end"`
}

func bedRoutes(g *platform.Router, h *encounterHandlers) {
	hp := "/hospitals/:hospital"
	g.PUT(hp+"/wards/:ward", PermBedConfigure, h.putWard)
	platform.ListRoute(g, hp+"/wards", PermVisitRead, h.listWards)
	g.PUT(hp+"/wards/:ward/rooms/:room", PermBedConfigure, h.putRoom)
	platform.ListRoute(g, hp+"/rooms", PermVisitRead, h.listRooms)
	g.PUT(hp+"/rooms/:room/beds/:bed", PermBedConfigure, h.putBed)
	platform.ListRoute(g, hp+"/beds", PermVisitRead, h.listBeds)
	g.GET(hp+"/beds/:bed/occupant", PermVisitRead, h.bedOccupant)
	g.GET(hp+"/rooms/:room/occupant", PermVisitRead, h.roomOccupant)
	g.PUT(hp+"/beds/:bed/transitions/:transition", PermAdmissionManage, h.moveBed)
	g.PUT(hp+"/admissions/:admission", PermAdmissionManage, h.admit)
	g.GET(hp+"/admissions/:admission", PermVisitRead, h.getAdmission)
	g.PUT(hp+"/admissions/:admission/transfers/:transfer", PermAdmissionManage, h.transfer)
	g.PUT(hp+"/admissions/:admission/release", PermAdmissionManage, h.release)
}

// inBedHospital is inHospital with the subject the bed tables' row security
// reads, so a row outside the caller's hospital grants is invisible even to
// a query that forgets to filter by hospital.
func (h *encounterHandlers) inBedHospital(c *gin.Context, fn func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error) error {
	return h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		if err := tx.Exec(`SELECT set_config('app.medicore_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		return fn(tx, p, hospital)
	})
}

func v7Param(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	return id, err == nil && id.Version() == 7
}

func digestOf(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func bedFailure(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errNoBed):
		respond.NotFound(c, "bed")
	case errors.Is(err, errNoRoom):
		respond.NotFound(c, "room")
	case errors.Is(err, errNoAdmission):
		respond.NotFound(c, "admission")
	case errors.Is(err, errNoOccupant):
		respond.Error(c, http.StatusNotFound, "no_occupant", errNoOccupant.Error())
	case errors.Is(err, errAmbiguousRoom):
		respond.Error(c, http.StatusConflict, "ambiguous", errAmbiguousRoom.Error())
	case errors.Is(err, errBedNotAvailable):
		respond.Error(c, http.StatusConflict, "bed_not_available", err.Error())
	case errors.Is(err, errStaleBed):
		respond.Error(c, http.StatusConflict, "stale_bed", err.Error())
	case errors.Is(err, errAlreadyAdmitted):
		respond.Error(c, http.StatusConflict, "already_admitted", err.Error())
	case errors.Is(err, errNotAdmitted), errors.Is(err, errBedMove):
		respond.Error(c, http.StatusConflict, "illegal_move", err.Error())
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "medicore_admission_active":
		respond.Error(c, http.StatusConflict, "already_admitted", errAlreadyAdmitted.Error())
	case errors.As(err, &pg) && pg.Code == "23505" && strings.HasSuffix(pg.ConstraintName, "_code_key"):
		respond.Conflict(c, "a ward, room or bed code is already used at this hospital")
	case errors.As(err, &pg) && pg.Code == "23505" && strings.HasSuffix(pg.ConstraintName, "_pkey"):
		// The command id is taken, in this hospital or one the caller cannot see.
		respond.Conflict(c, errIntentConflict.Error())
	case errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23P01" || pg.Code == "23514"), errors.Is(err, errUnapplied):
		// A racing command won the bed, or the database refused the move. Logged:
		// a refusal no racing command explains is a defect to find, not to retry.
		requestid.Logger(c).Warn("bed command refused by the database", "error", err)
		respond.Error(c, http.StatusConflict, "stale_bed", errStaleBed.Error())
	default:
		encounterFailure(c, err)
	}
}

type wardInput struct {
	Code string `json:"code" binding:"required"`
	Name string `json:"name" binding:"required"`
}

func (h *encounterHandlers) putWard(c *gin.Context) {
	id, ok := v7Param(c, "ward")
	var in wardInput
	if !ok {
		respond.NotFound(c, "ward")
		return
	}
	if err := c.ShouldBindJSON(&in); err != nil || !codePattern.MatchString(in.Code) || strings.TrimSpace(in.Name) != in.Name || in.Name == "" || len(in.Name) > 100 {
		respond.BadRequest(c, errors.New("a ward has a code of up to 20 capital letters, digits and dashes, and a name of up to 100 characters"))
		return
	}
	var row ward
	err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		row = ward{ID: id, TenantID: uuid.MustParse(p.TenantID), HospitalID: hospital, Code: in.Code, Name: in.Name, CreatedBy: p.Subject}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "id"}}, DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		var saved ward
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&saved).Error; err != nil {
			return err
		}
		if saved.HospitalID != hospital || saved.Code != in.Code || saved.Name != in.Name {
			return errIntentConflict
		}
		row = saved
		return nil
	})
	if err != nil {
		bedFailure(c, err)
		return
	}
	respond.OK(c, row)
}

func (h *encounterHandlers) listWards(c *gin.Context, params pagination.Params) ([]ward, error) {
	var rows []ward
	err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		return tenantdb.ApplyKeyset(tx, params).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		bedFailure(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *encounterHandlers) listRooms(c *gin.Context, params pagination.Params) ([]room, error) {
	var rows []room
	err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		return tenantdb.ApplyKeyset(tx, params).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		bedFailure(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *encounterHandlers) putRoom(c *gin.Context) {
	id, ok1 := v7Param(c, "room")
	wardID, ok2 := v7Param(c, "ward")
	var in struct {
		Code string `json:"code" binding:"required"`
	}
	if !ok1 || !ok2 {
		respond.NotFound(c, "room")
		return
	}
	if err := c.ShouldBindJSON(&in); err != nil || !codePattern.MatchString(in.Code) {
		respond.BadRequest(c, errors.New("a room has a code of up to 20 capital letters, digits and dashes"))
		return
	}
	var row room
	err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		row = room{ID: id, TenantID: uuid.MustParse(p.TenantID), HospitalID: hospital, WardID: wardID, Code: in.Code, CreatedBy: p.Subject}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "id"}}, DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		var saved room
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&saved).Error; err != nil {
			return err
		}
		if saved.HospitalID != hospital || saved.WardID != wardID || saved.Code != in.Code {
			return errIntentConflict
		}
		row = saved
		return nil
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23503" {
		respond.NotFound(c, "ward")
		return
	}
	if err != nil {
		bedFailure(c, err)
		return
	}
	respond.OK(c, row)
}

var bedClasses = map[string]bool{"general": true, "semi_private": true, "private": true, "icu": true, "hdu": true, "isolation": true, "neonatal": true, "day_care": true}

func (h *encounterHandlers) putBed(c *gin.Context) {
	id, ok1 := v7Param(c, "bed")
	roomID, ok2 := v7Param(c, "room")
	var in struct {
		Code     string `json:"code" binding:"required"`
		BedClass string `json:"bed_class" binding:"required"`
	}
	if !ok1 || !ok2 {
		respond.NotFound(c, "bed")
		return
	}
	if err := c.ShouldBindJSON(&in); err != nil || !codePattern.MatchString(in.Code) || !bedClasses[in.BedClass] {
		respond.BadRequest(c, errors.New("a bed has a code of up to 20 capital letters, digits and dashes, and a bed class of general, semi_private, private, icu, hdu, isolation, neonatal or day_care"))
		return
	}
	var row bedView
	err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var r room
		if err := tx.Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, roomID).Take(&r).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errNoBed
			}
			return err
		}
		if err := tx.Exec(`INSERT INTO medicore_beds (tenant_id,hospital_id,ward_id,room_id,id,code,bed_class,created_by) VALUES (?,?,?,?,?,?,?,?) ON CONFLICT (tenant_id,id) DO NOTHING`,
			p.TenantID, hospital, r.WardID, roomID, id, in.Code, in.BedClass, p.Subject).Error; err != nil {
			return err
		}
		if err := loadBed(tx, p.TenantID, hospital, id, &row); err != nil {
			return err
		}
		if row.RoomID != roomID || row.Code != in.Code || row.BedClass != in.BedClass {
			return errIntentConflict
		}
		return nil
	})
	if err != nil {
		bedFailure(c, err)
		return
	}
	respond.OK(c, row)
}

func loadBed(tx *gorm.DB, tenant string, hospital, id uuid.UUID, out *bedView) error {
	var rows []bedView
	if err := tx.Raw(bedViewSelect+` WHERE b.tenant_id=? AND b.hospital_id=? AND b.id=?`, tenant, hospital, id).Scan(&rows).Error; err != nil {
		return err
	}
	if len(rows) != 1 {
		return errNoBed
	}
	*out = rows[0]
	return nil
}

func (h *encounterHandlers) listBeds(c *gin.Context, params pagination.Params) ([]bedView, error) {
	var wardFilter *uuid.UUID
	if raw := c.Query("ward_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			respond.BadRequest(c, errors.New("ward_id must be a ward id"))
			return nil, nil
		}
		wardFilter = &id
	}
	var rows []bedView
	err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		q := tx.Table("(?) AS beds", tx.Raw(bedViewSelect+` WHERE b.tenant_id=? AND b.hospital_id=?`, p.TenantID, hospital))
		if wardFilter != nil {
			q = q.Where("ward_id=?", *wardFilter)
		}
		return tenantdb.ApplyKeyset(q, params).Find(&rows).Error
	})
	if err != nil {
		bedFailure(c, err)
		return nil, nil
	}
	return rows, nil
}

// lockBeds takes the beds a command moves, in id order, so two commands over
// the same beds never deadlock, and returns them by id.
func lockBeds(tx *gorm.DB, tenant string, hospital uuid.UUID, ids ...uuid.UUID) (map[uuid.UUID]bedView, error) {
	var rows []struct {
		ID      uuid.UUID
		WardID  uuid.UUID
		RoomID  uuid.UUID
		State   string
		Version int
	}
	if err := tx.Raw(`SELECT id,ward_id,room_id,state,version FROM medicore_beds WHERE tenant_id=? AND hospital_id=? AND id IN ? ORDER BY id FOR UPDATE`, tenant, hospital, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[uuid.UUID]bedView{}
	for _, r := range rows {
		out[r.ID] = bedView{ID: r.ID, WardID: r.WardID, RoomID: r.RoomID, State: r.State, Version: r.Version}
	}
	if len(out) != len(ids) {
		return nil, errNoBed
	}
	return out, nil
}

// moveBedState writes the transition and steps the bed, which the bed guard
// accepts only with that transition in place.
func moveBedState(tx *gorm.DB, tenant string, hospital uuid.UUID, b bedView, id uuid.UUID, to, cause string, reason *string, actor string, at time.Time) error {
	if err := tx.Exec(`INSERT INTO medicore_bed_transitions (tenant_id,hospital_id,id,bed_id,version,from_state,to_state,cause,reason,actor_subject,occurred_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		tenant, hospital, id, b.ID, b.Version+1, b.State, to, cause, reason, actor, at).Error; err != nil {
		return err
	}
	return exactlyOne(tx.Exec(`UPDATE medicore_beds SET state=?,version=version+1 WHERE tenant_id=? AND id=? AND version=?`, to, tenant, b.ID, b.Version))
}

func openOccupancy(tx *gorm.DB, tenant string, hospital uuid.UUID, b bedView, admissionID, patient uuid.UUID, at time.Time, actor string) error {
	occupancy := uuid.Must(uuid.NewV7())
	if err := tx.Exec(`INSERT INTO medicore_bed_occupancies (tenant_id,hospital_id,id,bed_id,admission_id,started_at,started_by) VALUES (?,?,?,?,?,?,?)`,
		tenant, hospital, occupancy, b.ID, admissionID, at, actor).Error; err != nil {
		return err
	}
	return publish(tx, tenant, medicorecontract.SubjectBedOccupancyStarted, "BedOccupancyStarted", medicorecontract.BedOccupancyStartedData{
		OccupancyID: occupancy.String(), AdmissionID: admissionID.String(), PatientID: patient.String(), HospitalID: hospital.String(),
		WardID: b.WardID.String(), RoomID: b.RoomID.String(), BedID: b.ID.String(), StartedAt: at.UTC().Format(time.RFC3339Nano),
	})
}

func closeOccupancy(tx *gorm.DB, tenant string, hospital uuid.UUID, o stay, admissionID uuid.UUID, at time.Time, end, actor string) error {
	if err := exactlyOne(tx.Exec(`UPDATE medicore_bed_occupancies SET ended_at=?,ended_by=?,ended_by_cause=? WHERE tenant_id=? AND id=? AND ended_at IS NULL`,
		at, actor, end, tenant, o.ID)); err != nil {
		return err
	}
	return publish(tx, tenant, medicorecontract.SubjectBedOccupancyEnded, "BedOccupancyEnded", medicorecontract.BedOccupancyEndedData{
		OccupancyID: o.ID.String(), AdmissionID: admissionID.String(), HospitalID: hospital.String(), BedID: o.BedID.String(),
		EndedAt: at.UTC().Format(time.RFC3339Nano), End: end,
	})
}

var errUnapplied = errors.New("a bed command changed nothing it should have")

// exactlyOne fails a write that changed no row, which under row security
// means the row stopped being visible: the command must not half-apply.
func exactlyOne(res *gorm.DB) error {
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return errUnapplied
	}
	return nil
}

func now(tx *gorm.DB) (time.Time, error) {
	var at struct{ At time.Time }
	err := tx.Raw(`SELECT clock_timestamp() AS at`).Scan(&at).Error
	return at.At, err
}

type admitInput struct {
	PatientID          uuid.UUID `json:"patient_id" binding:"required"`
	BedID              uuid.UUID `json:"bed_id" binding:"required"`
	Intent             string    `json:"intent" binding:"required,oneof=elective emergency"`
	ExpectedBedVersion int       `json:"expected_bed_version" binding:"required,min=1"`
}

func (h *encounterHandlers) admit(c *gin.Context) {
	id, ok := v7Param(c, "admission")
	if !ok {
		respond.NotFound(c, "admission")
		return
	}
	var in admitInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respond.BadRequest(c, err)
		return
	}
	var row admission
	created := false
	err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		digest := digestOf(hospital.String(), in.PatientID.String(), in.BedID.String(), in.Intent)
		// The bed lock first, so a concurrent identical retry waits and then
		// finds this admission and replays it.
		beds, err := lockBeds(tx, p.TenantID, hospital, in.BedID)
		if err != nil {
			return err
		}
		var existing []admission
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Limit(1).Find(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 {
			if existing[0].HospitalID != hospital || existing[0].IntentSHA256 != digest || existing[0].AdmittedBy != p.Subject {
				return errIntentConflict
			}
			return loadAdmission(tx, p.TenantID, hospital, id, &row)
		}
		var known int64
		if err := tx.Table("medicore_patients").Where("tenant_id=? AND patient_id=? AND hospital_id=?", p.TenantID, in.PatientID, hospital).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoPatient
		}
		b := beds[in.BedID]
		if b.Version != in.ExpectedBedVersion {
			return errStaleBed
		}
		if b.State != "available" {
			return errBedNotAvailable
		}
		at, err := now(tx)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO medicore_admissions (tenant_id,hospital_id,id,patient_id,intent,intent_sha256,admitted_by,admitted_at) VALUES (?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, id, in.PatientID, in.Intent, digest, p.Subject, at).Error; err != nil {
			return err
		}
		if err := moveBedState(tx, p.TenantID, hospital, b, uuid.Must(uuid.NewV7()), "occupied", "admission", nil, p.Subject, at); err != nil {
			return err
		}
		if err := openOccupancy(tx, p.TenantID, hospital, b, id, in.PatientID, at, p.Subject); err != nil {
			return err
		}
		created = true
		return loadAdmission(tx, p.TenantID, hospital, id, &row)
	})
	if err != nil {
		bedFailure(c, err)
		return
	}
	if created {
		respond.Created(c, row)
		return
	}
	respond.OK(c, row)
}

func loadAdmission(tx *gorm.DB, tenant string, hospital, id uuid.UUID, out *admission) error {
	if err := tx.Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id).Take(out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errNoAdmission
		}
		return err
	}
	out.Occupancies = []stay{}
	return tx.Table("medicore_bed_occupancies").Select("id,bed_id,started_at,started_by,ended_at,ended_by,ended_by_cause").
		Where("tenant_id=? AND admission_id=?", tenant, id).Order("started_at,id").Find(&out.Occupancies).Error
}

func (h *encounterHandlers) getAdmission(c *gin.Context) {
	id, ok := v7Param(c, "admission")
	if !ok {
		respond.NotFound(c, "admission")
		return
	}
	var row admission
	if err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		return loadAdmission(tx, p.TenantID, hospital, id, &row)
	}); err != nil {
		bedFailure(c, err)
		return
	}
	respond.OK(c, row)
}

// openStay locks the admission and returns its open occupancy.
func openStay(tx *gorm.DB, tenant string, hospital, id uuid.UUID, a *admission) (stay, error) {
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id).Take(a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return stay{}, errNoAdmission
		}
		return stay{}, err
	}
	var open []stay
	if err := tx.Table("medicore_bed_occupancies").Select("id,bed_id,started_at,started_by").
		Where("tenant_id=? AND admission_id=? AND ended_at IS NULL", tenant, id).Find(&open).Error; err != nil {
		return stay{}, err
	}
	if a.Status != "admitted" || len(open) != 1 {
		return stay{}, errNotAdmitted
	}
	return open[0], nil
}

type transferInput struct {
	ToBedID            uuid.UUID `json:"to_bed_id" binding:"required"`
	ExpectedBedVersion int       `json:"expected_bed_version" binding:"required,min=1"`
}

func (h *encounterHandlers) transfer(c *gin.Context) {
	id, ok1 := v7Param(c, "admission")
	transferID, ok2 := v7Param(c, "transfer")
	if !ok1 || !ok2 {
		respond.NotFound(c, "admission")
		return
	}
	var in transferInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respond.BadRequest(c, err)
		return
	}
	var row admission
	err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		digest := digestOf(id.String(), in.ToBedID.String())
		// The admission lock first, so a concurrent identical retry waits and
		// then finds this transfer and replays it.
		var a admission
		from, stayErr := openStay(tx, p.TenantID, hospital, id, &a)
		if errors.Is(stayErr, errNoAdmission) {
			return stayErr
		}
		var done []struct {
			AdmissionID  uuid.UUID
			IntentSHA256 string `gorm:"column:intent_sha256"`
			ActorSubject string
		}
		if err := tx.Table("medicore_admission_transfers").Select("admission_id,intent_sha256,actor_subject").Where("tenant_id=? AND id=?", p.TenantID, transferID).Find(&done).Error; err != nil {
			return err
		}
		if len(done) == 1 {
			if done[0].AdmissionID != id || done[0].IntentSHA256 != digest || done[0].ActorSubject != p.Subject {
				return errIntentConflict
			}
			return loadAdmission(tx, p.TenantID, hospital, id, &row)
		}
		if stayErr != nil {
			return stayErr
		}
		if from.BedID == in.ToBedID {
			return errBedMove
		}
		beds, err := lockBeds(tx, p.TenantID, hospital, from.BedID, in.ToBedID)
		if err != nil {
			return err
		}
		target := beds[in.ToBedID]
		if target.Version != in.ExpectedBedVersion {
			return errStaleBed
		}
		if target.State != "available" {
			return errBedNotAvailable
		}
		at, err := now(tx)
		if err != nil {
			return err
		}
		// The source closes and the target opens at the same instant.
		if err := closeOccupancy(tx, p.TenantID, hospital, from, id, at, "transfer", p.Subject); err != nil {
			return err
		}
		if err := moveBedState(tx, p.TenantID, hospital, beds[from.BedID], uuid.Must(uuid.NewV7()), "cleaning", "transfer_out", nil, p.Subject, at); err != nil {
			return err
		}
		if err := moveBedState(tx, p.TenantID, hospital, target, uuid.Must(uuid.NewV7()), "occupied", "transfer_in", nil, p.Subject, at); err != nil {
			return err
		}
		if err := openOccupancy(tx, p.TenantID, hospital, target, id, a.PatientID, at, p.Subject); err != nil {
			return err
		}
		var opened struct{ ID uuid.UUID }
		if err := tx.Raw(`SELECT id FROM medicore_bed_occupancies WHERE tenant_id=? AND admission_id=? AND ended_at IS NULL`, p.TenantID, id).Scan(&opened).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO medicore_admission_transfers (tenant_id,hospital_id,id,admission_id,from_occupancy_id,to_occupancy_id,intent_sha256,actor_subject) VALUES (?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, transferID, id, from.ID, opened.ID, digest, p.Subject).Error; err != nil {
			return err
		}
		return loadAdmission(tx, p.TenantID, hospital, id, &row)
	})
	if err != nil {
		bedFailure(c, err)
		return
	}
	respond.OK(c, row)
}

var releaseReasons = map[string]bool{"discharged": true, "transferred_out": true, "died": true, "left_against_advice": true, "other": true}

type releaseInput struct {
	Reason string  `json:"reason" binding:"required"`
	Note   *string `json:"note"`
}

func (h *encounterHandlers) release(c *gin.Context) {
	id, ok := v7Param(c, "admission")
	if !ok {
		respond.NotFound(c, "admission")
		return
	}
	var in releaseInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respond.BadRequest(c, err)
		return
	}
	if in.Note != nil {
		trimmed := strings.TrimSpace(*in.Note)
		in.Note = &trimmed
	}
	if !releaseReasons[in.Reason] || (in.Note != nil && (*in.Note == "" || len([]rune(*in.Note)) > 500)) || (in.Reason == "other" && in.Note == nil) {
		respond.BadRequest(c, errors.New("reason is discharged, transferred_out, died, left_against_advice or other; other needs a note of up to 500 characters"))
		return
	}
	var row admission
	err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var a admission
		from, err := openStay(tx, p.TenantID, hospital, id, &a)
		if errors.Is(err, errNotAdmitted) && a.Status == "released" {
			// A retry is answered from the record; a different release is not.
			if a.ReleaseReason == nil || *a.ReleaseReason != in.Reason || !sameNote(a.ReleaseNote, in.Note) || a.ReleasedBy == nil || *a.ReleasedBy != p.Subject {
				return errIntentConflict
			}
			return loadAdmission(tx, p.TenantID, hospital, id, &row)
		}
		if err != nil {
			return err
		}
		beds, err := lockBeds(tx, p.TenantID, hospital, from.BedID)
		if err != nil {
			return err
		}
		at, err := now(tx)
		if err != nil {
			return err
		}
		if err := closeOccupancy(tx, p.TenantID, hospital, from, id, at, "release", p.Subject); err != nil {
			return err
		}
		if err := moveBedState(tx, p.TenantID, hospital, beds[from.BedID], uuid.Must(uuid.NewV7()), "cleaning", "release", nil, p.Subject, at); err != nil {
			return err
		}
		if err := exactlyOne(tx.Exec(`UPDATE medicore_admissions SET status='released',release_reason=?,release_note=?,released_by=?,released_at=? WHERE tenant_id=? AND id=? AND status='admitted'`,
			in.Reason, in.Note, p.Subject, at, p.TenantID, id)); err != nil {
			return err
		}
		if err := publish(tx, p.TenantID, medicorecontract.SubjectAdmissionReleased, "AdmissionReleased", medicorecontract.AdmissionReleasedData{
			AdmissionID: id.String(), PatientID: a.PatientID.String(), HospitalID: hospital.String(), ReleasedAt: at.UTC().Format(time.RFC3339Nano), Reason: in.Reason,
		}); err != nil {
			return err
		}
		return loadAdmission(tx, p.TenantID, hospital, id, &row)
	})
	if err != nil {
		bedFailure(c, err)
		return
	}
	respond.OK(c, row)
}

func sameNote(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// moveBed makes the moves that are not admission moves: ready after cleaning,
// out of service, and back to service through cleaning.
func (h *encounterHandlers) moveBed(c *gin.Context) {
	id, ok1 := v7Param(c, "bed")
	transitionID, ok2 := v7Param(c, "transition")
	if !ok1 || !ok2 {
		respond.NotFound(c, "bed")
		return
	}
	var in struct {
		To              string  `json:"to" binding:"required,oneof=available out_of_service cleaning"`
		Reason          *string `json:"reason"`
		ExpectedVersion int     `json:"expected_version" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respond.BadRequest(c, err)
		return
	}
	if in.Reason != nil {
		trimmed := strings.TrimSpace(*in.Reason)
		in.Reason = &trimmed
	}
	needsReason := in.To != "available"
	if needsReason != (in.Reason != nil) || (in.Reason != nil && (*in.Reason == "" || len([]rune(*in.Reason)) > 500)) {
		respond.BadRequest(c, errors.New("taking a bed out of service or returning it needs a reason of up to 500 characters; marking it ready takes none"))
		return
	}
	var row bedView
	err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		// The bed lock first, so a concurrent identical retry waits and then
		// finds this transition and replays it.
		beds, err := lockBeds(tx, p.TenantID, hospital, id)
		if err != nil {
			return err
		}
		var done []struct {
			BedID        uuid.UUID
			ToState      string
			Reason       *string
			ActorSubject string
		}
		if err := tx.Table("medicore_bed_transitions").Select("bed_id,to_state,reason,actor_subject").Where("tenant_id=? AND id=?", p.TenantID, transitionID).Find(&done).Error; err != nil {
			return err
		}
		if len(done) == 1 {
			if done[0].BedID != id || done[0].ToState != in.To || !sameNote(done[0].Reason, in.Reason) || done[0].ActorSubject != p.Subject {
				return errIntentConflict
			}
			return loadBed(tx, p.TenantID, hospital, id, &row)
		}
		b := beds[id]
		if b.Version != in.ExpectedVersion {
			return errStaleBed
		}
		cause := map[[2]string]string{{"cleaning", "available"}: "ready", {"available", "out_of_service"}: "out_of_service",
			{"cleaning", "out_of_service"}: "out_of_service", {"out_of_service", "cleaning"}: "return_to_service"}[[2]string{b.State, in.To}]
		if cause == "" {
			return errBedMove
		}
		at, err := now(tx)
		if err != nil {
			return err
		}
		if err := moveBedState(tx, p.TenantID, hospital, b, transitionID, in.To, cause, in.Reason, p.Subject, at); err != nil {
			return err
		}
		return loadBed(tx, p.TenantID, hospital, id, &row)
	})
	if err != nil {
		bedFailure(c, err)
		return
	}
	respond.OK(c, row)
}

type occupant struct {
	AdmissionID uuid.UUID `json:"admission_id"`
	PatientID   uuid.UUID `json:"patient_id"`
	BedID       uuid.UUID `json:"bed_id"`
	OccupancyID uuid.UUID `json:"occupancy_id"`
	StartedAt   time.Time `json:"started_at"`
}

const occupantSelect = `SELECT o.admission_id,a.patient_id,o.bed_id,o.id AS occupancy_id,o.started_at FROM medicore_bed_occupancies o
 JOIN medicore_admissions a ON a.tenant_id=o.tenant_id AND a.id=o.admission_id JOIN medicore_beds b ON b.tenant_id=o.tenant_id AND b.id=o.bed_id
 WHERE o.tenant_id=? AND o.hospital_id=? AND o.ended_at IS NULL`

func (h *encounterHandlers) bedOccupant(c *gin.Context) { h.occupantOf(c, "bed", "b.id") }

func (h *encounterHandlers) roomOccupant(c *gin.Context) { h.occupantOf(c, "room", "b.room_id") }

// occupantOf answers who occupies a bed or room now. An empty bed or room is
// no_occupant and a room with several occupied beds is ambiguous: never a guess.
func (h *encounterHandlers) occupantOf(c *gin.Context, param, column string) {
	id, ok := v7Param(c, param)
	if !ok {
		respond.NotFound(c, "bed")
		return
	}
	var found []occupant
	err := h.inBedHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var exists int64
		table := map[string]string{"b.id": "medicore_beds", "b.room_id": "medicore_rooms"}[column]
		if err := tx.Table(table).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Count(&exists).Error; err != nil {
			return err
		}
		if exists == 0 {
			if param == "room" {
				return errNoRoom
			}
			return errNoBed
		}
		return tx.Raw(occupantSelect+` AND `+column+`=? ORDER BY o.id LIMIT 2`, p.TenantID, hospital, id).Scan(&found).Error
	})
	switch {
	case err != nil:
		bedFailure(c, err)
	case len(found) == 0:
		bedFailure(c, errNoOccupant)
	case len(found) > 1:
		bedFailure(c, errAmbiguousRoom)
	default:
		respond.OK(c, found[0])
	}
}
