package emergency

import (
	"context"
	"crypto/subtle"
	"encoding/base32"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/transit"
	"gorm.io/gorm"
)

type caseRow struct {
	ID                 uuid.UUID  `json:"id"`
	BaseID             uuid.UUID  `json:"base_id"`
	CaseNumber         string     `json:"case_number"`
	Channel            string     `json:"channel"`
	RequestedService   string     `json:"requested_service"`
	ProcessingBasis    string     `json:"processing_basis"`
	PolicyReference    string     `json:"policy_reference"`
	Status             string     `json:"status"`
	Priority           string     `json:"priority"`
	Version            int64      `json:"version"`
	ContactRetainUntil time.Time  `json:"contact_retain_until"`
	ContactPurgedAt    *time.Time `json:"contact_purged_at"`
	ClosureReason      *string    `json:"closure_reason"`
	ClosedAt           *time.Time `json:"closed_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	RequestFingerprint string     `json:"-"`
	ContactCiphertext  *string    `json:"-"`
	IncidentCiphertext string     `json:"-"`
	ClosureCiphertext  *string    `json:"-"`
	ClosureFingerprint *string    `json:"-"`
}

func (caseRow) TableName() string                 { return "emergency_cases" }
func (c caseRow) PageKey() (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }

const caseColumns = "id,base_id,case_number,channel,requested_service,processing_basis,policy_reference,status,priority,version,contact_retain_until,contact_purged_at,closure_reason,closed_at,created_at,updated_at"
const caseProtectedColumns = caseColumns + ",request_fingerprint,contact_ciphertext,incident_ciphertext,closure_ciphertext,closure_fingerprint"

type contact struct {
	CallerName    string `json:"caller_name" binding:"max=150"`
	CallbackPhone string `json:"callback_phone" binding:"max=30"`
	Relationship  string `json:"relationship" binding:"max=30"`
}
type incident struct {
	LocationText       string `json:"location_text" binding:"max=500"`
	PatientDescription string `json:"patient_description" binding:"max=1500"`
}
type intakeInput struct {
	Channel          string   `json:"channel" binding:"required,max=30"`
	RequestedService string   `json:"requested_service" binding:"required,max=30"`
	ProcessingBasis  string   `json:"processing_basis" binding:"required,max=30"`
	Contact          contact  `json:"contact"`
	Incident         incident `json:"incident"`
}
type timelineRow struct {
	ID           uuid.UUID `json:"id"`
	CaseID       uuid.UUID `json:"case_id"`
	Version      int64     `json:"version"`
	State        string    `json:"state"`
	Reason       *string   `json:"reason"`
	ActorSubject string    `json:"actor_subject"`
	OccurredAt   time.Time `json:"occurred_at"`
	CreatedAt    time.Time `json:"created_at"`
}

func (timelineRow) TableName() string                 { return "emergency_case_timeline" }
func (t timelineRow) PageKey() (time.Time, uuid.UUID) { return t.CreatedAt, t.ID }
func validChoice(group, value string) bool {
	var choices map[string]map[string]string
	if json.Unmarshal([]byte(intakeCatalogue), &choices) != nil {
		return false
	}
	_, ok := choices[group][value]
	return ok
}
func validNarrative(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' })
}
func normalizeIntake(in *intakeInput) bool {
	in.Contact.CallerName = strings.TrimSpace(in.Contact.CallerName)
	in.Contact.CallbackPhone = strings.TrimSpace(in.Contact.CallbackPhone)
	in.Incident.LocationText = strings.TrimSpace(in.Incident.LocationText)
	in.Incident.PatientDescription = strings.TrimSpace(in.Incident.PatientDescription)
	if in.Contact.Relationship == "" {
		in.Contact.Relationship = "unknown"
	}
	if !validChoice("channels", in.Channel) || !validChoice("services", in.RequestedService) || !validChoice("bases", in.ProcessingBasis) || !validChoice("relationships", in.Contact.Relationship) {
		return false
	}
	if in.ProcessingBasis == "medical_emergency" && (in.RequestedService == "patient_transport" || in.RequestedService == "mortuary") {
		return false
	}
	if !validNarrative(in.Incident.LocationText) || !validNarrative(in.Incident.PatientDescription) || (in.Contact.CallerName != "" && !cleanText(in.Contact.CallerName)) {
		return false
	}
	if phone := in.Contact.CallbackPhone; phone != "" {
		digits := strings.TrimPrefix(phone, "+")
		if len(digits) < 7 || len(digits) > 15 || strings.ContainsFunc(digits, func(r rune) bool { return r < '0' || r > '9' }) {
			return false
		}
	}
	return true
}
func caseID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("case"))
	if err != nil || id.Version() != 7 {
		respond.NotFound(c, "case")
		return uuid.Nil, false
	}
	return id, true
}
func caseKey(p authn.Principal) string { return "helivanta-emergency-" + p.TenantID }
func caseContext(p authn.Principal, b, id uuid.UUID, field string) []byte {
	return []byte("helivanta/emergency/case/v1|" + p.TenantID + "|" + b.String() + "|" + id.String() + "|" + field)
}
func (h *handlers) checkScope(ctx context.Context, p authn.Principal, b uuid.UUID, ability string) error {
	return h.transaction(ctx, p, b, false, func(tx *gorm.DB) error { return requireAccess(tx, p, b, ability) })
}
func (h *handlers) putCase(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := caseID(c)
	if !ok {
		return
	}
	var in intakeInput
	if !decode(c, &in) {
		return
	}
	if !normalizeIntake(&in) {
		respond.Error(c, 400, "invalid_request", "invalid intake fields")
		return
	}
	if err := h.checkScope(c.Request.Context(), p, b, "intake"); err != nil {
		respondError(c, err)
		return
	}
	if h.transit == nil {
		respondError(c, transit.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	cb, _ := json.Marshal(in.Contact)
	ib, _ := json.Marshal(in.Incident)
	body, _ := json.Marshal(in)
	sealedContact, err := h.transit.Encrypt(ctx, caseKey(p), caseContext(p, b, id, "contact"), cb)
	if err != nil {
		respondError(c, err)
		return
	}
	sealedIncident, err := h.transit.Encrypt(ctx, caseKey(p), caseContext(p, b, id, "incident"), ib)
	if err != nil {
		respondError(c, err)
		return
	}
	fingerprint, err := h.transit.HMAC(ctx, caseKey(p), append(caseContext(p, b, id, "request"), body...))
	if err != nil {
		respondError(c, err)
		return
	}
	var out caseRow
	err = h.transaction(ctx, p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "intake"); err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO emergency_cases(tenant_id,base_id,id,case_number,channel,requested_service,processing_basis,policy_reference,request_fingerprint,contact_ciphertext,incident_ciphertext,contact_retain_until)
 SELECT ?,?,?,?,?,?,?,b.policy_reference,?,?,?,now()+b.contact_retention_days*interval '1 day' FROM emergency_bases b WHERE b.tenant_id=? AND b.id=?
 ON CONFLICT DO NOTHING`, p.TenantID, b, id, "EC-"+base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(id[:]), in.Channel, in.RequestedService, in.ProcessingBasis, fingerprint, sealedContact, sealedIncident, p.TenantID, b)
		if result.Error != nil {
			return result.Error
		}
		if err := tx.Select(caseProtectedColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error; err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(out.RequestFingerprint), []byte(fingerprint)) != 1 {
			return errCaseConflict
		}
		if result.RowsAffected == 0 {
			return nil
		}
		return changed(tx, p, b, out, emergencycontract.SubjectCaseReceived)
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.Accepted(c, out)
}

var errCaseConflict = errors.New("case content or version conflict")

func changed(tx *gorm.DB, p authn.Principal, b uuid.UUID, row caseRow, subject string) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO emergency_case_timeline(tenant_id,base_id,case_id,id,version,state,reason,actor_subject) VALUES(?,?,?,?,?,?,?,?)`, p.TenantID, b, row.ID, id, row.Version, row.Status, row.ClosureReason, p.Subject).Error; err != nil {
		return err
	}
	data, err := json.Marshal(emergencycontract.CaseChangedData{BaseID: b.String(), CaseID: row.ID.String(), Version: row.Version, Status: row.Status})
	if err != nil {
		return err
	}
	if err := events.Enqueue(tx, subject, events.Event{ID: id.String(), Type: row.Status, Version: 1, TenantID: p.TenantID, OccurredAt: row.UpdatedAt, Data: data}); err != nil {
		return err
	}
	return audit(tx, p, b, row.ID, "case."+row.Status, nil)
}
func (h *handlers) listCases(c *gin.Context, params pagination.Params) ([]caseRow, error) {
	p, b, ok := scope(c)
	if !ok {
		return nil, nil
	}
	var rows []caseRow
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyKeyset(tx, params).Select(caseColumns).Where("tenant_id=? AND base_id=?", p.TenantID, b).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			return audit(tx, p, b, b, "case.queue_requested", nil)
		}
		return nil
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *handlers) getCase(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := caseID(c)
	if !ok {
		return
	}
	h.caseDetails(c, p, b, id)
}

func (h *handlers) caseDetails(c *gin.Context, p authn.Principal, b, id uuid.UUID) {
	var row caseRow
	var expired bool
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := tx.Select(caseProtectedColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&row).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT contact_retain_until<=now() FROM emergency_cases WHERE tenant_id=? AND base_id=? AND id=?`, p.TenantID, b, id).Scan(&expired).Error; err != nil {
			return err
		}
		return audit(tx, p, b, id, "case.details_requested", nil)
	})
	if err != nil {
		respondError(c, err)
		return
	}
	if h.transit == nil {
		respondError(c, transit.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	var contactData *contact
	if !expired && row.ContactCiphertext != nil {
		plain, err := h.transit.Decrypt(ctx, caseKey(p), caseContext(p, b, id, "contact"), *row.ContactCiphertext)
		if err != nil {
			respondError(c, err)
			return
		}
		var data contact
		if json.Unmarshal(plain, &data) != nil {
			respondError(c, transit.ErrIntegrity)
			return
		}
		contactData = &data
	}
	plain, err := h.transit.Decrypt(ctx, caseKey(p), caseContext(p, b, id, "incident"), row.IncidentCiphertext)
	if err != nil {
		respondError(c, err)
		return
	}
	var incidentData incident
	if json.Unmarshal(plain, &incidentData) != nil {
		respondError(c, transit.ErrIntegrity)
		return
	}
	var rationale *string
	if row.ClosureCiphertext != nil {
		plain, err = h.transit.Decrypt(ctx, caseKey(p), caseContext(p, b, id, "closure"), *row.ClosureCiphertext)
		if err != nil {
			respondError(c, err)
			return
		}
		s := string(plain)
		rationale = &s
	}
	respond.OK(c, struct {
		caseRow
		Contact            *contact `json:"contact"`
		Incident           incident `json:"incident"`
		ContactExpired     bool     `json:"contact_expired"`
		ClosureExplanation *string  `json:"closure_explanation"`
	}{row, contactData, incidentData, expired, rationale})
}
func (h *handlers) timeline(c *gin.Context, params pagination.Params) ([]timelineRow, error) {
	p, b, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, ok := caseID(c)
	if !ok {
		return nil, nil
	}
	var rows []timelineRow
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		var row caseRow
		if err := tx.Select("id").Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&row).Error; err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select("id,case_id,version,state,reason,actor_subject,occurred_at,created_at").Where("tenant_id=? AND base_id=? AND case_id=?", p.TenantID, b, id).Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}
