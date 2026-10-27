package medicore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	drugmastercontract "github.com/tesserix/helivanta/internal/modules/drugmaster/contract"
	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
)

// Allergies and weight (#1373). Spec:
// docs/superpowers/specs/2026-10-27-allergies-and-weight-design.md.

var (
	errUnknownMolecule   = errors.New("the drug master has not published that molecule")
	errActiveAllergy     = errors.New("the patient has an active allergy; resolve it before recording no known allergy")
	errRecordingDisabled = errors.New("allergy recording is not enabled until a clinician approves the reaction and severity lists")
)

type clinicalHandlers struct {
	*encounterHandlers
	allergyRecording bool
}

// inPatient is inHospital for one patient registered at that hospital.
// A patient registered elsewhere, in another tenant or not at all is the
// same 404 as no grant (#1373 A1).
func (h *clinicalHandlers) inPatient(c *gin.Context, fn func(tx *gorm.DB, p authn.Principal, hospital, patient uuid.UUID) error) error {
	patient, err := uuid.Parse(c.Param("patient"))
	if err != nil {
		return errNoPatient
	}
	return h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var known int64
		if err := tx.Table("medicore_patients").Where("tenant_id = ? AND patient_id = ? AND hospital_id = ?", p.TenantID, patient, hospital).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoPatient
		}
		return fn(tx, p, hospital, patient)
	})
}

func readAudit(tx *gorm.DB, p authn.Principal, hospital, patient uuid.UUID, record string) error {
	return tx.Exec(`INSERT INTO medicore_clinical_read_audit (tenant_id, hospital_id, patient_id, record, actor_subject) VALUES (?, ?, ?, ?, ?)`,
		p.TenantID, hospital, patient, record, p.Subject).Error
}

func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func clientID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	return id, err == nil && id.Version() == 7
}

func optionalUUID(s *string) (*uuid.UUID, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil, fmt.Errorf("not a uuid: %q", *s)
	}
	return &id, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type allergyEntry struct {
	ID              uuid.UUID  `gorm:"column:id" json:"id"`
	Version         int        `gorm:"column:version" json:"version"`
	Status          string     `gorm:"column:status" json:"status"`
	SubstanceKind   string     `gorm:"column:substance_kind" json:"substance_kind"`
	MoleculeID      *uuid.UUID `gorm:"column:molecule_id" json:"molecule_id"`
	SubstanceLabel  string     `gorm:"column:substance_label" json:"substance_label"`
	NonDrugCategory *string    `gorm:"column:non_drug_category" json:"non_drug_category"`
	Reaction        string     `gorm:"column:reaction" json:"reaction"`
	ReactionNote    *string    `gorm:"column:reaction_note" json:"reaction_note"`
	Severity        string     `gorm:"column:severity" json:"severity"`
	Source          string     `gorm:"column:source" json:"source"`
	OnsetDate       *string    `gorm:"column:onset_date" json:"onset_date"`
	EncounterID     *uuid.UUID `gorm:"column:encounter_id" json:"encounter_id"`
	Reason          *string    `gorm:"column:reason" json:"reason"`
	RecordedBy      string     `gorm:"column:recorded_by" json:"recorded_by"`
	RecordedAt      time.Time  `gorm:"column:recorded_at" json:"recorded_at"`
	// NeedsCoding marks a drug named in free text: it is on the record but
	// can never clear a prescribing check until recoded (#1373 A3).
	NeedsCoding bool `gorm:"-" json:"needs_coding"`
}

const entryColumns = `id, version, status, substance_kind, molecule_id, substance_label, non_drug_category, reaction, reaction_note,
	severity, source, onset_date::text AS onset_date, encounter_id, reason, recorded_by, recorded_at`

type allergyStatus struct {
	// Status is unknown, no_known_allergy or allergies_recorded. Unknown is
	// never stored: it is what no record means (IN-CL-08).
	Status      string         `json:"status"`
	NoKnown     *noKnownRecord `json:"no_known_allergy"`
	Entries     []allergyEntry `json:"entries"`
	NeedsCoding bool           `json:"needs_coding"`
	// RecordingEnabled says whether new entries can be recorded yet, so a
	// screen can explain why not instead of failing on submit.
	RecordingEnabled bool `json:"recording_enabled"`
}

type noKnownRecord struct {
	ID         uuid.UUID `gorm:"column:id" json:"id"`
	RecordedBy string    `gorm:"column:recorded_by" json:"recorded_by"`
	RecordedAt time.Time `gorm:"column:recorded_at" json:"recorded_at"`
}

// currentAllergies reads a patient's allergy state. The latest fact wins:
// a "no known allergy" newer than every allergy change is the status; an
// active entry is "recorded"; anything else (including an allergy since
// resolved, with no newer statement) is unknown, never a reassurance.
func currentAllergies(tx *gorm.DB, tenant string, hospital, patient uuid.UUID) (allergyStatus, error) {
	out := allergyStatus{Status: "unknown", Entries: []allergyEntry{}}
	var latest []allergyEntry
	if err := tx.Raw(`SELECT DISTINCT ON (id) `+entryColumns+` FROM medicore_allergy_entries
		WHERE tenant_id = ? AND hospital_id = ? AND patient_id = ? ORDER BY id, version DESC`, tenant, hospital, patient).Scan(&latest).Error; err != nil {
		return out, err
	}
	var lastChange time.Time
	for _, e := range latest {
		if e.RecordedAt.After(lastChange) {
			lastChange = e.RecordedAt
		}
		if e.Status == "active" {
			e.NeedsCoding = e.SubstanceKind == "drug_unlisted"
			out.NeedsCoding = out.NeedsCoding || e.NeedsCoding
			out.Entries = append(out.Entries, e)
		}
	}
	var nka []noKnownRecord
	if err := tx.Raw(`SELECT id, recorded_by, recorded_at FROM medicore_no_known_allergy WHERE tenant_id = ? AND hospital_id = ? AND patient_id = ?
		ORDER BY recorded_at DESC LIMIT 1`, tenant, hospital, patient).Scan(&nka).Error; err != nil {
		return out, err
	}
	switch {
	case len(out.Entries) > 0:
		out.Status = "allergies_recorded"
	case len(nka) == 1 && nka[0].RecordedAt.After(lastChange):
		out.Status, out.NoKnown = "no_known_allergy", &nka[0]
	}
	return out, nil
}

func (h *clinicalHandlers) allergies(c *gin.Context) {
	var out allergyStatus
	err := h.inPatient(c, func(tx *gorm.DB, p authn.Principal, hospital, patient uuid.UUID) error {
		var err error
		if out, err = currentAllergies(tx, p.TenantID, hospital, patient); err != nil {
			return err
		}
		return readAudit(tx, p, hospital, patient, "allergies")
	})
	if err != nil {
		clinicalFailure(c, err)
		return
	}
	out.RecordingEnabled = h.allergyRecording
	respond.OK(c, out)
}

func (h *clinicalHandlers) allergyHistory(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "allergy")
		return
	}
	var rows []allergyEntry
	err = h.inPatient(c, func(tx *gorm.DB, p authn.Principal, hospital, patient uuid.UUID) error {
		// An entry has at most a handful of versions: active, then resolved
		// or entered in error, which ends it.
		if err := tx.Raw(`SELECT `+entryColumns+` FROM medicore_allergy_entries WHERE tenant_id = ? AND hospital_id = ? AND patient_id = ? AND id = ?
			ORDER BY version`, p.TenantID, hospital, patient, id).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return errNoAllergy
		}
		return readAudit(tx, p, hospital, patient, "allergy_history")
	})
	if err != nil {
		clinicalFailure(c, err)
		return
	}
	respond.OK(c, gin.H{"data": rows})
}

var errNoAllergy = errors.New("allergy not found")

type recordAllergyRequest struct {
	SubstanceKind   string  `json:"substance_kind" binding:"required,oneof=drug_molecule drug_unlisted non_drug"`
	MoleculeID      *string `json:"molecule_id"`
	SubstanceLabel  *string `json:"substance_label" binding:"omitempty,max=200"`
	NonDrugCategory *string `json:"non_drug_category" binding:"omitempty,oneof=food latex environmental contrast_media other"`
	Reaction        string  `json:"reaction" binding:"required,oneof=rash urticaria angioedema anaphylaxis bronchospasm gastrointestinal other"`
	ReactionNote    *string `json:"reaction_note" binding:"omitempty,max=500"`
	Severity        string  `json:"severity" binding:"required,oneof=mild moderate severe life_threatening unknown"`
	Source          string  `json:"source" binding:"required,oneof=patient_reported clinician_observed prior_record"`
	OnsetDate       *string `json:"onset_date"`
	EncounterID     *string `json:"encounter_id"`
}

// validate checks what the binding tags cannot: which fields each kind
// needs, and dates.
func (r *recordAllergyRequest) validate(today string) error {
	trim := func(s *string) *string {
		if s == nil {
			return nil
		}
		t := strings.TrimSpace(*s)
		if t == "" {
			return nil
		}
		return &t
	}
	r.SubstanceLabel, r.ReactionNote = trim(r.SubstanceLabel), trim(r.ReactionNote)
	switch r.SubstanceKind {
	case "drug_molecule":
		if r.MoleculeID == nil || r.SubstanceLabel != nil || r.NonDrugCategory != nil {
			return errors.New("a coded drug allergy names molecule_id only")
		}
	case "drug_unlisted":
		if r.SubstanceLabel == nil || r.MoleculeID != nil || r.NonDrugCategory != nil {
			return errors.New("an unlisted drug allergy names substance_label only")
		}
	case "non_drug":
		if r.SubstanceLabel == nil || r.NonDrugCategory == nil || r.MoleculeID != nil {
			return errors.New("a non-drug allergy names non_drug_category and substance_label")
		}
	}
	if r.OnsetDate != nil {
		d, err := time.Parse(time.DateOnly, *r.OnsetDate)
		if err != nil || d.Year() < 1900 || *r.OnsetDate > today {
			return errors.New("onset_date must be a date, YYYY-MM-DD, not in the future")
		}
	}
	return nil
}

func todayIndia() string {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		// Fail toward refusing a future date: UTC+14 is the latest "today".
		loc = time.FixedZone("UTC+14", 14*3600)
	}
	return time.Now().In(loc).Format(time.DateOnly)
}

// recordAllergy records an entry under a client UUIDv7, idempotently
// (#1373 A8). It is off until the vocabularies are clinically approved.
func (h *clinicalHandlers) recordAllergy(c *gin.Context) {
	if !h.allergyRecording {
		respond.Error(c, http.StatusServiceUnavailable, "allergy_recording_disabled", errRecordingDisabled.Error())
		return
	}
	id, ok := clientID(c)
	if !ok {
		respond.NotFound(c, "allergy")
		return
	}
	var req recordAllergyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	if err := req.validate(todayIndia()); err != nil {
		respond.BadRequest(c, err)
		return
	}
	molecule, err := optionalUUID(req.MoleculeID)
	if err != nil {
		respond.BadRequest(c, err)
		return
	}
	encounter, err := optionalUUID(req.EncounterID)
	if err != nil {
		respond.BadRequest(c, err)
		return
	}
	var row allergyEntry
	created := false
	err = h.inPatient(c, func(tx *gorm.DB, p authn.Principal, hospital, patient uuid.UUID) error {
		intent := digest(hospital.String(), patient.String(), req.SubstanceKind, deref(req.MoleculeID), deref(req.SubstanceLabel),
			deref(req.NonDrugCategory), req.Reaction, deref(req.ReactionNote), req.Severity, req.Source, deref(req.OnsetDate), deref(req.EncounterID))
		var existing []struct {
			IntentSHA256, RecordedBy string
			HospitalID, PatientID    uuid.UUID
		}
		if err := tx.Raw(`SELECT intent_sha256, recorded_by, hospital_id, patient_id FROM medicore_allergy_entries WHERE tenant_id = ? AND id = ? AND version = 1`,
			p.TenantID, id).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 {
			e := existing[0]
			if e.IntentSHA256 != intent || e.RecordedBy != p.Subject || e.HospitalID != hospital || e.PatientID != patient {
				return errIntentConflict
			}
			return tx.Raw(`SELECT `+entryColumns+` FROM medicore_allergy_entries WHERE tenant_id = ? AND id = ? AND version = 1`, p.TenantID, id).Scan(&row).Error
		}
		label := deref(req.SubstanceLabel)
		if molecule != nil {
			// The label shown is the drug master's name, never the client's.
			var names []string
			if err := tx.Raw(`SELECT inn_name FROM medicore_drug_molecules WHERE id = ?`, *molecule).Scan(&names).Error; err != nil {
				return err
			}
			if len(names) == 0 {
				return errUnknownMolecule
			}
			label = names[0]
		}
		if err := tx.Exec(`INSERT INTO medicore_allergy_entries (tenant_id, id, version, hospital_id, patient_id, status, substance_kind, molecule_id,
			substance_label, non_drug_category, reaction, reaction_note, severity, source, onset_date, encounter_id, intent_sha256, recorded_by)
			VALUES (?, ?, 1, ?, ?, 'active', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.TenantID, id, hospital, patient, req.SubstanceKind, molecule,
			label, req.NonDrugCategory, req.Reaction, req.ReactionNote, req.Severity, req.Source, req.OnsetDate, encounter, intent, p.Subject).Error; err != nil {
			return err
		}
		created = true
		if err := tx.Raw(`SELECT `+entryColumns+` FROM medicore_allergy_entries WHERE tenant_id = ? AND id = ? AND version = 1`, p.TenantID, id).Scan(&row).Error; err != nil {
			return err
		}
		return publishAllergy(tx, p.TenantID, hospital, patient, id, 1, "entry")
	})
	if err != nil {
		clinicalFailure(c, err)
		return
	}
	row.NeedsCoding = row.SubstanceKind == "drug_unlisted" && row.Status == "active"
	if created {
		respond.Created(c, row)
		return
	}
	respond.OK(c, row)
}

type correctAllergyRequest struct {
	ExpectedVersion int    `json:"expected_version" binding:"required,min=1"`
	Status          string `json:"status" binding:"required,oneof=resolved entered_in_error"`
	Reason          string `json:"reason" binding:"required,max=500"`
}

// correctAllergy retires an active entry with a reason, as a new version;
// the original stays (IN-CL-12). Allowed even while recording is off, so a
// wrong entry can always be corrected.
func (h *clinicalHandlers) correctAllergy(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "allergy")
		return
	}
	var req correctAllergyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		respond.BadRequest(c, errReasonRequired)
		return
	}
	var row allergyEntry
	err = h.inPatient(c, func(tx *gorm.DB, p authn.Principal, hospital, patient uuid.UUID) error {
		var cur []allergyEntry
		if err := tx.Raw(`SELECT `+entryColumns+` FROM medicore_allergy_entries WHERE tenant_id = ? AND hospital_id = ? AND patient_id = ? AND id = ?
			ORDER BY version DESC LIMIT 1 FOR UPDATE`, p.TenantID, hospital, patient, id).Scan(&cur).Error; err != nil {
			return err
		}
		if len(cur) == 0 {
			return errNoAllergy
		}
		if cur[0].Version != req.ExpectedVersion {
			return errStaleEncounter
		}
		if cur[0].Status != "active" {
			return errIllegalMove
		}
		next := cur[0].Version + 1
		if err := tx.Exec(`INSERT INTO medicore_allergy_entries (tenant_id, id, version, hospital_id, patient_id, status, substance_kind, molecule_id,
			substance_label, non_drug_category, reaction, reaction_note, severity, source, onset_date, encounter_id, reason, intent_sha256, recorded_by)
			SELECT tenant_id, id, ?, hospital_id, patient_id, ?, substance_kind, molecule_id, substance_label, non_drug_category, reaction, reaction_note,
			 severity, source, onset_date, encounter_id, ?, ?, ? FROM medicore_allergy_entries WHERE tenant_id = ? AND id = ? AND version = ?`,
			next, req.Status, reason, digest(req.Status, reason), p.Subject, p.TenantID, id, cur[0].Version).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT `+entryColumns+` FROM medicore_allergy_entries WHERE tenant_id = ? AND id = ? AND version = ?`, p.TenantID, id, next).Scan(&row).Error; err != nil {
			return err
		}
		return publishAllergy(tx, p.TenantID, hospital, patient, id, next, "entry")
	})
	if err != nil {
		clinicalFailure(c, err)
		return
	}
	respond.OK(c, row)
}

type noKnownRequest struct {
	EncounterID *string `json:"encounter_id"`
}

// recordNoKnownAllergy records that a clinician asked and the patient has
// no known allergy (#1373 A2). Refused while any allergy is active. It uses
// none of the vocabularies under review, so the recording switch does not
// apply.
func (h *clinicalHandlers) recordNoKnownAllergy(c *gin.Context) {
	id, ok := clientID(c)
	if !ok {
		respond.NotFound(c, "no known allergy statement")
		return
	}
	var req noKnownRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	encounter, err := optionalUUID(req.EncounterID)
	if err != nil {
		respond.BadRequest(c, err)
		return
	}
	var row noKnownRecord
	created := false
	err = h.inPatient(c, func(tx *gorm.DB, p authn.Principal, hospital, patient uuid.UUID) error {
		intent := digest(hospital.String(), patient.String(), deref(req.EncounterID))
		var existing []struct {
			IntentSHA256, RecordedBy string
		}
		if err := tx.Raw(`SELECT intent_sha256, recorded_by FROM medicore_no_known_allergy WHERE tenant_id = ? AND id = ?`, p.TenantID, id).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 {
			if existing[0].IntentSHA256 != intent || existing[0].RecordedBy != p.Subject {
				return errIntentConflict
			}
		} else {
			status, err := currentAllergies(tx, p.TenantID, hospital, patient)
			if err != nil {
				return err
			}
			if status.Status == "allergies_recorded" {
				return errActiveAllergy
			}
			if err := tx.Exec(`INSERT INTO medicore_no_known_allergy (tenant_id, id, hospital_id, patient_id, encounter_id, intent_sha256, recorded_by)
				VALUES (?, ?, ?, ?, ?, ?, ?)`, p.TenantID, id, hospital, patient, encounter, intent, p.Subject).Error; err != nil {
				return err
			}
			created = true
			if err := publishAllergy(tx, p.TenantID, hospital, patient, id, 1, "no_known_allergy"); err != nil {
				return err
			}
		}
		return tx.Raw(`SELECT id, recorded_by, recorded_at FROM medicore_no_known_allergy WHERE tenant_id = ? AND id = ?`, p.TenantID, id).Scan(&row).Error
	})
	if err != nil {
		clinicalFailure(c, err)
		return
	}
	if created {
		respond.Created(c, row)
		return
	}
	respond.OK(c, row)
}

func publishAllergy(tx *gorm.DB, tenant string, hospital, patient, id uuid.UUID, version int, kind string) error {
	return publish(tx, tenant, medicorecontract.SubjectAllergyChanged, "AllergyChanged", medicorecontract.AllergyChangedData{
		PatientID: patient.String(), HospitalID: hospital.String(), RecordID: id.String(), Version: version, Kind: kind,
	})
}

func clinicalFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errNoAllergy):
		respond.NotFound(c, "allergy")
	case errors.Is(err, errNoWeight):
		respond.NotFound(c, "weight")
	case errors.Is(err, errUnknownMolecule):
		respond.Error(c, http.StatusUnprocessableEntity, "unknown_molecule", err.Error())
	case errors.Is(err, errActiveAllergy):
		respond.Conflict(c, err.Error())
	case isCheckViolation(err, "is not this patient's at this hospital"):
		respond.Error(c, http.StatusUnprocessableEntity, "encounter_mismatch", "the encounter is not this patient's at this hospital")
	case isCheckViolation(err, "the patient has an active allergy"):
		respond.Conflict(c, errActiveAllergy.Error())
	default:
		encounterFailure(c, err)
	}
}

func isCheckViolation(err error, message string) bool {
	return err != nil && strings.Contains(err.Error(), message)
}

// moleculePublished projects the drug master's molecules, so an allergy
// can only name one that exists (#1373 A4). Redelivery is a no-op; a
// malformed event is an error, retried and never dropped.
func moleculePublished(_ context.Context, tx *gorm.DB, evt events.Event) error {
	var d drugmastercontract.MoleculePublishedData
	if evt.Version != 1 || json.Unmarshal(evt.Data, &d) != nil {
		return errors.New("invalid molecule event")
	}
	id, err := uuid.Parse(d.MoleculeID)
	if err != nil || id == uuid.Nil || strings.TrimSpace(d.INNName) == "" {
		return errors.New("invalid molecule reference")
	}
	return tx.Exec(`INSERT INTO medicore_drug_molecules (id, inn_name, atc_code) VALUES (?, ?, ?) ON CONFLICT (id) DO NOTHING`,
		id, d.INNName, d.ATCCode).Error
}
