// Registration is the front door of the whole platform: every clinical,
// billing and pharmacy event downstream resolves to the patient_id
// minted here. Getting duplicate detection wrong here corrupts
// everything that follows, so this file implements spec D4 (a
// confident match blocks, a possible match only offers candidates) and
// D5 (the consent receipt is written in the same transaction as the
// patient, because a patient without one is a DPDP compliance defect,
// not a formality).
package patientmaster

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/mediconnectclaims"
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/modules/patientmaster/matching"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/transit"
)

// dateLayout is the ISO calendar-date form every DOB on the wire and in
// matching.Candidate uses — never a timestamp, since a patient's date of
// birth has no time component to lose or fabricate.
const dateLayout = "2006-01-02"

var errRegistrationKeyReused = errors.New("registration request ID reused with different input")

var patientPhone = regexp.MustCompile(`^(\+[1-9][0-9]{6,14}|[0-9]{10})$`)

// patient is the GORM projection of the patients table. It implements
// its own MarshalJSON rather than relying on struct tags because DOB is
// stored (and compared, by matching.Score) as a bare date — encoding it
// through time.Time's default RFC3339 marshaling would put a fabricated
// midnight timestamp on the wire.
type patient struct {
	ID                 uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()"`
	TenantID           uuid.UUID
	HospitalID         uuid.UUID
	MRN                string
	GivenName          string
	FamilyName         string
	NormalizedName     string
	PhoneticKey        string
	Sex                string
	DOB                time.Time
	DOBEstimated       bool
	Mobile             string
	AddressLine        string
	CreatedAt          time.Time
	ReportedBloodGroup *reportedBloodGroup `gorm:"-"`
}

func (patient) TableName() string { return "patients" }

// PageKey is the keyset position of this row, satisfying platform.Keyed.
func (p patient) PageKey() (time.Time, uuid.UUID) { return p.CreatedAt, p.ID }

// fullName is the single comparable name matching.Candidate expects —
// given and family name joined, exactly as a clerk reads it off a
// wristband or a referral letter.
func (p patient) fullName() string {
	return strings.TrimSpace(p.GivenName + " " + p.FamilyName)
}

func (p patient) MarshalJSON() ([]byte, error) {
	type alias struct {
		ID                 string              `json:"id"`
		HospitalID         string              `json:"hospital_id"`
		MRN                string              `json:"mrn"`
		GivenName          string              `json:"given_name"`
		FamilyName         string              `json:"family_name"`
		Sex                string              `json:"sex"`
		DOB                string              `json:"dob"`
		DOBEstimated       bool                `json:"dob_estimated"`
		Mobile             string              `json:"mobile"`
		AddressLine        string              `json:"address_line"`
		CreatedAt          string              `json:"created_at"`
		ReportedBloodGroup *reportedBloodGroup `json:"reported_blood_group,omitempty"`
	}
	return json.Marshal(alias{
		ID:                 p.ID.String(),
		ReportedBloodGroup: p.ReportedBloodGroup,
		HospitalID:         p.HospitalID.String(),
		MRN:                p.MRN,
		GivenName:          p.GivenName,
		FamilyName:         p.FamilyName,
		Sex:                p.Sex,
		DOB:                p.DOB.Format(dateLayout),
		DOBEstimated:       p.DOBEstimated,
		Mobile:             p.Mobile,
		AddressLine:        p.AddressLine,
		CreatedAt:          p.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// patientConsent is the DPDP consent receipt, written in the same
// transaction as its patient row (spec D5).
type patientConsent struct {
	ID                   uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()"`
	TenantID             uuid.UUID
	PatientID            uuid.UUID
	NoticeVersion        string
	ConsentedBy          string
	GuardianName         string
	GuardianRelationship string
	RecordedBySubject    string
	CreatedAt            time.Time
}

func (patientConsent) TableName() string { return "patient_consents" }

// patientDuplicateOverride is the audit row that makes deferring
// merge/unmerge to a later slice honest (spec D4): every confident
// duplicate created anyway is recorded with who did it, against which
// existing patient, at what score, and why.
type patientDuplicateOverride struct {
	ID               uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()"`
	TenantID         uuid.UUID
	CreatedPatientID uuid.UUID
	MatchedPatientID uuid.UUID
	Score            float64
	Reason           string
	ActorSubject     string
	CreatedAt        time.Time
}

func (patientDuplicateOverride) TableName() string { return "patient_duplicate_overrides" }

// registerRequest is the registration wire shape. Sex, mobile and
// address are all optional: a clerk under OPD-rush pressure must be able
// to capture a name, DOB and consent and move on — everything else is
// completed later rather than blocking the queue.
type reportedBloodGroupInput struct {
	ABO    string `json:"abo" binding:"required,oneof=A B AB O unknown"`
	Rh     string `json:"rh" binding:"required,oneof=positive negative unknown"`
	Source string `json:"source" binding:"required,oneof=patient guardian document"`
}

type reportedBloodGroup struct {
	reportedBloodGroupInput
	RecordedAt time.Time `json:"recorded_at" gorm:"column:created_at"`
}

type registerRequest struct {
	ReportedBloodGroup *reportedBloodGroupInput `json:"reported_blood_group,omitempty"`
	RequestID          string                   `json:"request_id" binding:"required,uuid"`
	GivenName          string                   `json:"given_name" binding:"required,max=100"`
	FamilyName         string                   `json:"family_name" binding:"max=100"`
	Sex                string                   `json:"sex" binding:"omitempty,oneof=male female other"`
	DOB                string                   `json:"dob" binding:"required,datetime=2006-01-02"`
	DOBEstimated       bool                     `json:"dob_estimated"`
	Mobile             string                   `json:"mobile" binding:"max=16"`
	AddressLine        string                   `json:"address_line" binding:"max=300"`
	NoticeVersion      string                   `json:"notice_version" binding:"required,max=40"`
	ConsentedBy        string                   `json:"consented_by" binding:"required,oneof=patient guardian"`
	GuardianName       string                   `json:"guardian_name" binding:"max=100"`
	GuardianRel        string                   `json:"guardian_relationship" binding:"max=40"`
	Override           bool                     `json:"override"`
	OverrideReason     string                   `json:"override_reason" binding:"max=300"`
}

type patientHandlers struct {
	db      *tenantdb.DB
	bus     *events.Bus
	transit *transit.Client
	claims  *mediconnectclaims.Client
}

// possibleMatch is the wire shape of one Possible-band candidate offered
// alongside a fresh registration (spec D4). Only fields a clerk needs to
// recognise the record — never anything the caller isn't already
// authorised to see, which is why this mirrors patient's own public
// fields rather than embedding the row directly.
type possibleMatch struct {
	PatientID  string  `json:"patient_id"`
	MRN        string  `json:"mrn"`
	GivenName  string  `json:"given_name"`
	FamilyName string  `json:"family_name"`
	Score      float64 `json:"score"`
}

// describeMatch projects a matched patient row into the wire shape, used
// both for the Possible-band disclosures and for the candidate carried
// on a blocking 409 — the two are the same decision for the clerk, so
// they get the same information.
func describeMatch(r patient, score float64) possibleMatch {
	return possibleMatch{
		PatientID:  r.ID.String(),
		MRN:        r.MRN,
		GivenName:  r.GivenName,
		FamilyName: r.FamilyName,
		Score:      score,
	}
}

// generateMRN mints a tenant-scoped Medical Record Number.
//
// #70's plan calls for "the facility-scoped MRN" but no facility concept
// exists yet (module.go's note on D2) and neither the design spec nor
// this task's brief specifies a format — so this is a deliberate,
// documented call: 10 random base32 characters, the same order of
// entropy this codebase already trusts for every primary key via
// gen_random_uuid(), rather than a sequential counter. A per-tenant
// sequential MRN would need its own counter row to stay race-free under
// two clerks registering at the same desk simultaneously, and adding
// that table is out of this task's scope (only patients.go and Routes
// are touched). A random MRN sidesteps the race entirely: two
// concurrent registrations drawing the same 50 bits of entropy is not a
// risk worth a schema change to prevent.
func generateMRN() (string, error) {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ" // Crockford base32, no I/L/O/U
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating mrn: %w", err)
	}
	out := make([]byte, len(buf))
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return "MRN-" + string(out), nil
}

// register binds a registration, checks it against the tenant's
// existing patients, and — unless blocked — writes the patient and its
// consent receipt in one transaction (spec D5). See the ordered steps
// below; they mirror the plan's numbering exactly so a reviewer can
// check this function against the brief line by line.
func (h *patientHandlers) register(c *gin.Context) {
	hospital, scoped := hospitalScope(c)
	if !scoped {
		return
	}
	principal, tenantUUID, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}

	// Step 1: bind and validate. An override with a blank reason is
	// refused before anything else runs — the reason IS the control, and
	// letting the transaction open first would mean building an audit
	// row was ever on the table for a request that names no reason.
	var req registerRequest
	if !decodePatientInput(c, &req) {
		return
	}
	req.GivenName = strings.TrimSpace(req.GivenName)
	req.FamilyName = strings.TrimSpace(req.FamilyName)
	req.GuardianName = strings.TrimSpace(req.GuardianName)
	req.GuardianRel = strings.TrimSpace(req.GuardianRel)
	req.NoticeVersion = strings.TrimSpace(req.NoticeVersion)
	if req.GivenName == "" || req.NoticeVersion == "" || (req.Mobile != "" && !patientPhone.MatchString(req.Mobile)) {
		respond.BadRequest(c, errors.New("invalid patient name, notice version or phone number"))
		return
	}
	if req.Override && strings.TrimSpace(req.OverrideReason) == "" {
		respond.BadRequest(c, errors.New("override_reason is required when override is true"))
		return
	}
	dob, err := time.Parse(dateLayout, req.DOB)
	if err != nil {
		// Defensive: the binding tag above already enforces this shape.
		respond.BadRequest(c, fmt.Errorf("dob: %w", err))
		return
	}

	today := time.Now().UTC().Format(dateLayout)
	if req.DOB > today {
		respond.BadRequest(c, errors.New("date of birth cannot be in the future"))
		return
	}
	dateNow, _ := time.Parse(dateLayout, today)
	if dob.AddDate(18, 0, 0).After(dateNow) && req.ConsentedBy != "guardian" {
		respond.BadRequest(c, errors.New("a guardian consent receipt is required for a minor"))
		return
	}
	if req.ConsentedBy == "guardian" && (req.GuardianName == "" || req.GuardianRel == "") {
		respond.BadRequest(c, errors.New("guardian name and relationship are required"))
		return
	}

	var (
		replay          json.RawMessage
		blocked         bool
		blockedMatch    possibleMatch
		created         patient
		possibleMatches = []possibleMatch{}
	)

	txErr := h.hospitalTransaction(c.Request.Context(), principal, hospital, true, func(tx *gorm.DB) error {
		encoded, err := json.Marshal(req)
		if err != nil {
			return err
		}
		fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, principal.TenantID+hospital.String()+req.RequestID).Error; err != nil {
			return err
		}
		var saved struct {
			Fingerprint string
			Response    []byte
		}
		if err := tx.Raw(`SELECT fingerprint, response FROM patient_registration_requests WHERE tenant_id = ? AND hospital_id = ? AND id = ?`, principal.TenantID, hospital, req.RequestID).Scan(&saved).Error; err != nil {
			return err
		}
		if saved.Fingerprint != "" {
			if saved.Fingerprint != fingerprint {
				return errRegistrationKeyReused
			}
			replay = json.RawMessage(saved.Response)
			return patientAudit(tx, principal, hospital, "read", nil)
		}

		fullName := strings.TrimSpace(req.GivenName + " " + req.FamilyName)
		phoneticKey := matching.PhoneticKey(fullName)

		// Step 2a: serialise every registration that lands in the same
		// blocking bucket, inside this transaction. Without it the
		// duplicate check is a read-then-write under READ COMMITTED:
		// two clerks registering the same patient at the same moment
		// each load a corpus that does not yet contain the other, each
		// scores NoMatch, and both insert — the exact duplicate this
		// endpoint exists to prevent, created by the endpoint itself.
		// docs/standards/backend.md treats this shape as a correctness
		// bug, not a tolerable race.
		//
		// The lock is on the blocking key, so it serialises only the
		// handful of registrations that could possibly match each other
		// and never the endpoint as a whole. It is transaction-scoped:
		// released by COMMIT or ROLLBACK, so no path can leak it.
		//
		// NOT a unique index on (tenant_id, phonetic_key, dob, mobile):
		// an index is a hard constraint and would also reject the
		// DELIBERATE override, which spec D4 makes a required feature —
		// trading a race for a broken escape hatch, and a clerk who
		// cannot register the patient in front of them works around the
		// system entirely.
		//
		// Known gap, stated rather than hidden: the corpus below also
		// blocks on mobile, and this lock does not cover the
		// mobile-only arm. Two simultaneous registrations that share a
		// mobile but not a phonetic key are still racy. Locking both
		// keys needs a defined lock ordering to stay deadlock-free and
		// is a change of its own; the phonetic key is the arm that
		// always participates.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?::text || ?::text))`,
			principal.TenantID+hospital.String(), phoneticKey).Error; err != nil {
			return fmt.Errorf("locking registration blocking key: %w", err)
		}

		// Step 2b: load the candidate corpus. Blocked on phonetic_key OR
		// mobile — both are indexed (tenant_id, phonetic_key) and
		// (tenant_id, mobile) — never the whole table. The accepted
		// consequence: two records agreeing on neither field are never
		// compared, bounding recall by the blocking key rather than
		// widening this into a full scan on the busiest endpoint in the
		// hospital.
		corpusQuery := tx.Model(&patient{}).Where("phonetic_key = ?", phoneticKey)
		if req.Mobile != "" {
			corpusQuery = tx.Model(&patient{}).Where("phonetic_key = ? OR mobile = ?", phoneticKey, req.Mobile)
		}
		var corpusRows []patient
		if err := corpusQuery.Find(&corpusRows).Error; err != nil {
			return fmt.Errorf("loading match corpus: %w", err)
		}

		subject := matching.Candidate{
			Name:         fullName,
			DOB:          req.DOB,
			DOBEstimated: req.DOBEstimated,
			Mobile:       req.Mobile,
		}
		corpus := make([]matching.Candidate, len(corpusRows))
		for i, r := range corpusRows {
			corpus[i] = matching.Candidate{
				PatientID:    r.ID.String(),
				Name:         r.fullName(),
				DOB:          r.DOB.Format(dateLayout),
				DOBEstimated: r.DOBEstimated,
				Mobile:       r.Mobile,
			}
		}

		if err := patientAudit(tx, principal, hospital, "match", corpusRows); err != nil {
			return err
		}

		// Step 3: rank.
		ranked := matching.Rank(subject, corpus, matching.DefaultThresholds())

		byID := make(map[string]patient, len(corpusRows))
		for _, r := range corpusRows {
			byID[r.ID.String()] = r
		}

		var confident *matching.Result
		if len(ranked) > 0 && ranked[0].Band == matching.BandConfident {
			confident = &ranked[0]
		}

		if confident == nil {
			for i := range ranked {
				candidate := byID[ranked[i].Candidate.PatientID]
				if candidate.NormalizedName == matching.NormalizeName(fullName) && candidate.DOB.Format(dateLayout) == req.DOB {
					confident = &ranked[i]
					break
				}
			}
		}

		// Step 4: block a confident match unless overridden. No row is
		// written on this path.
		//
		// The 409 carries the same candidate shape the Possible band
		// returns, not a bare id and score. This is the one moment a
		// human adjudicates "same person or not", and an id alone gives
		// them nothing to decide with — the only action the payload
		// supported was `override`, which inverts the policy's intent.
		if confident != nil && !req.Override {
			blocked = true
			blockedMatch = describeMatch(byID[confident.Candidate.PatientID], confident.Score)
			return nil
		}

		// Spec D4's middle row: "Possible — candidates shown, ranked. No
		// block." Unlike Confident, this must never withhold creation —
		// common Indian names collide constantly, and refusing to
		// register until a clerk confirms would stop unrelated people
		// with a shared name from registering at all. So the patient is
		// created below regardless, and every Possible-band candidate is
		// surfaced in the response as a disclosure, not a gate: the
		// clerk finds out about a look-alike record only after this one
		// already exists. Letting them choose the existing record
		// INSTEAD of creating a new one needs search-before-register in
		// the console, which the spec explicitly defers to that later
		// UI (D4) — this is the interim, honest half-measure until it
		// ships, not the intended end state.
		for _, res := range ranked {
			if res.Band != matching.BandPossible {
				continue
			}
			r, ok := byID[res.Candidate.PatientID]
			if !ok {
				continue
			}
			possibleMatches = append(possibleMatches, describeMatch(r, res.Score))
		}

		mrn, err := generateMRN()
		if err != nil {
			return err
		}

		// Step 5: insert the patient, with normalized_name and
		// phonetic_key computed here — the indexes above are over this
		// compared form, not the raw input.
		row := patient{
			TenantID:       tenantUUID,
			HospitalID:     hospital,
			MRN:            mrn,
			GivenName:      req.GivenName,
			FamilyName:     req.FamilyName,
			NormalizedName: matching.NormalizeName(fullName),
			PhoneticKey:    phoneticKey,
			Sex:            req.Sex,
			DOB:            dob,
			DOBEstimated:   req.DOBEstimated,
			Mobile:         req.Mobile,
			AddressLine:    req.AddressLine,
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("inserting patient: %w", err)
		}

		if req.ReportedBloodGroup != nil {
			group := reportedBloodGroup{reportedBloodGroupInput: *req.ReportedBloodGroup}
			if err := tx.Raw(`INSERT INTO patient_reported_blood_groups
                (tenant_id, patient_id, abo, rh, source, recorded_by_subject)
                VALUES (?, ?, ?, ?, ?, ?) RETURNING created_at`,
				tenantUUID, row.ID, group.ABO, group.Rh, group.Source, principal.Subject).Scan(&group.RecordedAt).Error; err != nil {
				return fmt.Errorf("inserting reported blood group: %w", err)
			}
			row.ReportedBloodGroup = &group
		}

		// Step 6: the consent receipt, in the SAME transaction. A
		// patient row that outlives a failed consent insert is the
		// compliance defect spec D5 exists to make unreachable.
		consent := patientConsent{
			TenantID:             tenantUUID,
			PatientID:            row.ID,
			NoticeVersion:        req.NoticeVersion,
			ConsentedBy:          req.ConsentedBy,
			GuardianName:         req.GuardianName,
			GuardianRelationship: req.GuardianRel,
			RecordedBySubject:    principal.Subject,
		}
		if err := tx.Create(&consent).Error; err != nil {
			return fmt.Errorf("inserting consent: %w", err)
		}

		// Step 7: if this row exists only because a confident match was
		// overridden, record why — both ids, the score, the reason and
		// the actor. This is merge's future worklist (spec D4).
		if confident != nil && req.Override {
			matchedID, err := uuid.Parse(confident.Candidate.PatientID)
			if err != nil {
				return fmt.Errorf("parsing matched patient id: %w", err)
			}
			override := patientDuplicateOverride{
				TenantID:         tenantUUID,
				CreatedPatientID: row.ID,
				MatchedPatientID: matchedID,
				Score:            confident.Score,
				Reason:           req.OverrideReason,
				ActorSubject:     principal.Subject,
			}
			if err := tx.Create(&override).Error; err != nil {
				return fmt.Errorf("inserting duplicate override: %w", err)
			}
		}

		// Step 8: publish patient.registered, in the SAME transaction as
		// the patient and consent inserts above. The payload carries the
		// patient and hospital references, without demographics — see
		// contract.PatientRegisteredData's doc comment for why.
		data, err := json.Marshal(patientmastercontract.PatientRegisteredData{
			PatientID:  row.ID.String(),
			HospitalID: hospital.String(),
		})
		if err != nil {
			return fmt.Errorf("marshaling patient.registered payload: %w", err)
		}
		if err := h.bus.Publish(tx, patientmastercontract.SubjectPatientRegistered, events.Event{
			Type: "PatientRegistered", Version: 1, TenantID: principal.TenantID, Data: data,
		}); err != nil {
			return fmt.Errorf("publishing patient.registered: %w", err)
		}

		created = row
		if err := patientAudit(tx, principal, hospital, "register", []patient{row}); err != nil {
			return err
		}
		response, err := json.Marshal(gin.H{"patient": created, "possible_matches": possibleMatches})
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO patient_registration_requests (id, tenant_id, hospital_id, fingerprint, response) VALUES (?, ?, ?, ?, ?::jsonb)`, req.RequestID, principal.TenantID, hospital, fingerprint, string(response)).Error
	})
	if errors.Is(txErr, errRegistrationKeyReused) {
		respond.Conflict(c, "request_id was already used for another registration")
		return
	}
	if txErr != nil {
		patientError(c, txErr)
		return
	}
	if len(replay) > 0 {
		respond.Created(c, replay)
		return
	}
	if blocked {
		respond.ConflictWithDetail(c, "a confident duplicate match exists", gin.H{
			"match": blockedMatch,
		})
		return
	}
	// One 201 shape, always. Previously NoMatch returned a bare patient
	// object and Possible returned {patient, possible_matches}, so a
	// client had to branch on the shape just to find the patient id.
	// Making possible_matches an always-present (possibly empty) array
	// is free today and a breaking change the day the console consumes
	// this.
	respond.Created(c, gin.H{"patient": created, "possible_matches": possibleMatches})
}

// get loads one patient by id. RLS filters rows outside the caller's
// tenant, so a cross-tenant probe and a genuine miss both surface as
// gorm.ErrRecordNotFound here — the same 404, never a 403 that would
// tell a prober the id exists.
func (h *patientHandlers) get(c *gin.Context) {
	hospital, scoped := hospitalScope(c)
	if !scoped {
		return
	}
	principal, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "patient")
		return
	}
	var row patient
	err = h.hospitalTransaction(c.Request.Context(), principal, hospital, false, func(tx *gorm.DB) error {
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		var group struct {
			ABO       string
			Rh        string
			Source    string
			CreatedAt time.Time
		}
		result := tx.Raw(`SELECT abo, rh, source, created_at FROM patient_reported_blood_groups
            WHERE tenant_id = ? AND patient_id = ?`, principal.TenantID, id).Scan(&group)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			row.ReportedBloodGroup = &reportedBloodGroup{
				reportedBloodGroupInput: reportedBloodGroupInput{ABO: group.ABO, Rh: group.Rh, Source: group.Source},
				RecordedAt:              group.CreatedAt,
			}
		}
		return patientAudit(tx, principal, hospital, "read", []patient{row})
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "patient")
		return
	}
	if err != nil {
		patientError(c, err)
		return
	}
	respond.OK(c, row)
}

// list returns one page of this tenant's patients, newest first.
//
// It returns whatever ApplyKeyset yields — up to Limit+1 rows — and
// leaves trimming, has_more and cursor construction to
// platform.ListRoute, which owns all three.
func (h *patientHandlers) list(c *gin.Context, p pagination.Params) ([]patient, error) {
	hospital, scoped := hospitalScope(c)
	if !scoped {
		return nil, nil
	}
	principal, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []patient
	err := h.hospitalTransaction(c.Request.Context(), principal, hospital, false, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyKeyset(tx, p).Where("tenant_id = ? AND hospital_id = ?", principal.TenantID, hospital).Find(&rows).Error; err != nil {
			return err
		}
		return patientAudit(tx, principal, hospital, "list", rows)
	})
	if err != nil {
		patientError(c, err)
		return nil, nil
	}
	return rows, nil
}
