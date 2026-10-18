package workforce

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
)

const credentialReviewColumns = "id,staff_id,credential_id,base_version,status,approved_version,type,number,issuer,valid_from::text AS valid_from,valid_until::text AS valid_until,evidence_document_ids::text AS evidence_ids,reason,proposed_by_subject,decided_by_subject,decided_at,decision_note,created_at" //nolint:gosec // G101: a column list, not a secret

var (
	errCredentialInvalid      = errors.New("credential review is invalid")
	errCredentialNoteRequired = errors.New("credential rejection needs a note")
	errCredentialVersion      = errors.New("credential validity version conflict")
	errCredentialPending      = errors.New("credential review already pending")
	errCredentialReplay       = errors.New("credential review id reused with different content")
	errCredentialUnchanged    = errors.New("credential review changes nothing")
	errCredentialSelf         = errors.New("credential review decided by its proposer")
	errCredentialNotProposer  = errors.New("credential review withdrawn by someone else")
	errCredentialState        = errors.New("credential review already decided")
	errCredentialRemoved      = errors.New("credential removed from enrollment")
	errCredentialZoneUnset    = errors.New("hospital time zone not set")
)

func (r credentialReview) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

// The register pages by expiry date then review id, which is unique per hospital.
func (e credentialRegisterEntry) PageKey() (time.Time, uuid.UUID) {
	day, _ := time.Parse(time.DateOnly, e.ValidUntil)
	return day, e.ReviewID
}

type credentialReviewRow struct {
	Review      credentialReview `gorm:"embedded"`
	EvidenceIDs string
}

func uuidArrayLiteral(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func parseUUIDArray(text string) ([]uuid.UUID, error) {
	inner, opened := strings.CutPrefix(text, "{")
	inner, closed := strings.CutSuffix(inner, "}")
	if !opened || !closed {
		return nil, errors.New("malformed uuid array")
	}
	ids := []uuid.UUID{}
	if inner == "" {
		return ids, nil
	}
	for part := range strings.SplitSeq(inner, ",") {
		id, err := uuid.Parse(part)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func loadCredentialReviews(q *gorm.DB) ([]credentialReview, error) {
	var rows []credentialReviewRow
	if err := q.Select(credentialReviewColumns).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]credentialReview, len(rows))
	for i, row := range rows {
		ids, err := parseUUIDArray(row.EvidenceIDs)
		if err != nil {
			return nil, err
		}
		result[i] = row.Review
		result[i].EvidenceDocumentIDs = ids
	}
	return result, nil
}

func loadCredentialReview(tx *gorm.DB, tenant string, hospital, staff, credential, id uuid.UUID, lock bool) (credentialReview, error) {
	q := tx.Table("workforce_credential_reviews").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND credential_id=? AND id=?", tenant, hospital, staff, credential, id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	rows, err := loadCredentialReviews(q)
	if err != nil {
		return credentialReview{}, err
	}
	if len(rows) == 0 {
		return credentialReview{}, gorm.ErrRecordNotFound
	}
	return rows[0], nil
}

type credentialValidityRow struct {
	CredentialID uuid.UUID
	Reviewed     credentialReviewed `gorm:"embedded"`
}

func loadCredentialValidity(tx *gorm.DB, tenant string, hospital, staff uuid.UUID, credential *uuid.UUID, lock bool) (map[uuid.UUID]credentialReviewed, error) {
	q := tx.Table("workforce_credential_validity").Select("credential_id,number,valid_from::text AS valid_from,valid_until::text AS valid_until,version,review_id,updated_at").
		Where("tenant_id=? AND hospital_id=? AND staff_id=?", tenant, hospital, staff)
	if credential != nil {
		q = q.Where("credential_id=?", *credential)
	}
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rows []credentialValidityRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]credentialReviewed, len(rows))
	for _, row := range rows {
		result[row.CredentialID] = row.Reviewed
	}
	return result, nil
}

// credentialClock reads the database clock once per request and the hospital roster zone; a missing or unknown zone yields nil.
func credentialClock(tx *gorm.DB, tenant string, hospital uuid.UUID) (*time.Location, time.Time, error) {
	var row struct {
		Now      time.Time
		Timezone *string
	}
	if err := tx.Raw(`SELECT clock_timestamp() AS now,(SELECT timezone FROM workforce_roster_settings WHERE tenant_id=? AND hospital_id=?) AS timezone`, tenant, hospital).Scan(&row).Error; err != nil {
		return nil, time.Time{}, err
	}
	if row.Timezone == nil {
		return nil, row.Now, nil
	}
	loc, err := time.LoadLocation(*row.Timezone)
	if err != nil {
		return nil, row.Now, nil
	}
	return loc, row.Now, nil
}

// credentialStanding treats the expiry day as valid, the same inclusive convention as supply licences.
func credentialStanding(validFrom, validUntil string, loc *time.Location, now time.Time) (string, *int) {
	if loc == nil {
		return "date_unknown", nil
	}
	today, _ := time.Parse(time.DateOnly, now.In(loc).Format(time.DateOnly))
	from, errFrom := time.Parse(time.DateOnly, validFrom)
	until, errUntil := time.Parse(time.DateOnly, validUntil)
	if errFrom != nil || errUntil != nil {
		return "date_unknown", nil
	}
	days := int(until.Sub(today).Hours() / 24)
	switch {
	case from.After(today):
		return "not_yet_valid", &days
	case days < 0:
		return "expired", &days
	case days == 0:
		return "expires_today", &days
	}
	return "valid", &days
}

func credentialIDs(c *gin.Context, names ...string) ([]uuid.UUID, bool) {
	ids := make([]uuid.UUID, len(names))
	for i, name := range names {
		id, err := uuid.Parse(c.Param(name))
		if err != nil || id == uuid.Nil {
			respond.NotFound(c, map[string]string{"id": "staff member", "credential": "credential", "review": "credential review"}[name])
			return nil, false
		}
		ids[i] = id
	}
	return ids, true
}

// Mirrors the table checks so a malformed proposal is a 400 before any write; the database still enforces every rule.
func normalizeCredentialReview(in *credentialReviewInput) error {
	in.Number, in.Reason = strings.TrimSpace(in.Number), strings.TrimSpace(in.Reason)
	if in.Number == "" || utf8.RuneCountInString(in.Number) > 100 || in.Reason == "" || utf8.RuneCountInString(in.Reason) > 500 ||
		!validStatutoryText(in.Number) || !validStatutoryText(in.Reason) || in.ValidFrom == "" || in.ValidUntil == "" || !validPeriod(in.ValidFrom, in.ValidUntil) {
		return errCredentialInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range in.EvidenceDocumentIDs {
		if id == uuid.Nil || seen[id] {
			return errCredentialInvalid
		}
		seen[id] = true
	}
	return nil
}

func sameCredentialProposal(r credentialReview, in credentialReviewInput, staff, credential uuid.UUID, subject string) bool {
	return r.StaffID == staff && r.CredentialID == credential && r.ProposedBySubject == subject && r.BaseVersion == *in.ExpectedVersion &&
		r.Number == in.Number && r.ValidFrom == in.ValidFrom && r.ValidUntil == in.ValidUntil && r.Reason == in.Reason && slices.Equal(r.EvidenceDocumentIDs, in.EvidenceDocumentIDs)
}

// Audit rows and events carry identifiers, status and version only; numbers, issuers and dates stay behind the API.
func recordCredentialReview(h *handlers, tx *gorm.DB, tenant string, hospital uuid.UUID, subject, status string, staff, credential, review uuid.UUID, version int64) error {
	if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('credential_id',?::uuid,'review_id',?::uuid,'version',?::bigint))`,
		tenant, hospital, subject, "workforce.credential_review."+status, staff, credential, review, version).Error; err != nil {
		return err
	}
	data, err := json.Marshal(workforcecontract.CredentialReviewChangedData{HospitalID: hospital.String(), StaffID: staff.String(), CredentialID: credential.String(), ReviewID: review.String(), Status: status, Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, CredentialReviewChanged, events.Event{Type: CredentialReviewChanged, Version: 1, TenantID: tenant, Data: data})
}

func (h *handlers) listStaffCredentials(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	ids, ok := credentialIDs(c, "id")
	if !ok {
		return
	}
	staff := ids[0]
	result := staffCredentials{Credentials: []staffCredential{}}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := enrollmentScope(tx, p, hospital); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		if err := tx.Table("workforce_enrollment_credentials").Select("id,type,number,issuer,valid_from::text AS valid_from,valid_until::text AS valid_until").
			Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staff).Order("type,issuer,number,id").Find(&result.Credentials).Error; err != nil {
			return err
		}
		reviewed, err := loadCredentialValidity(tx, p.TenantID, hospital, staff, nil, false)
		if err != nil {
			return err
		}
		pending, err := loadCredentialReviews(tx.Table("workforce_credential_reviews").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND status='pending'", p.TenantID, hospital, staff))
		if err != nil {
			return err
		}
		loc, now, err := credentialClock(tx, p.TenantID, hospital)
		if err != nil {
			return err
		}
		if loc != nil {
			zone, day := loc.String(), now.In(loc).Format(time.DateOnly)
			result.TimeZone, result.LocalDate = &zone, &day
		}
		for i := range result.Credentials {
			cred := &result.Credentials[i]
			cred.Status = "unreviewed"
			if v, ok := reviewed[cred.ID]; ok {
				cred.Reviewed = &v
				cred.Status, cred.DaysRemaining = credentialStanding(v.ValidFrom, v.ValidUntil, loc, now)
			}
			for j := range pending {
				if pending[j].CredentialID == cred.ID {
					cred.Pending = &pending[j]
				}
			}
		}
		return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.credentials.read',?,'{}'::jsonb)`, p.TenantID, hospital, p.Subject, staff).Error
	})
	if err != nil {
		respondCredentialError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) credentialReviewHistory(c *gin.Context, params pagination.Params) ([]credentialReview, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	ids, ok := credentialIDs(c, "id", "credential")
	if !ok {
		return nil, nil
	}
	staff, credential := ids[0], ids[1]
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	var rows []credentialReview
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := enrollmentScope(tx, p, hospital); err != nil {
			return err
		}
		var known bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_enrollment_credentials WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=?)
 OR EXISTS(SELECT 1 FROM workforce_credential_reviews WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND credential_id=?)`,
			p.TenantID, hospital, staff, credential, p.TenantID, hospital, staff, credential).Scan(&known).Error; err != nil {
			return err
		}
		if !known {
			return gorm.ErrRecordNotFound
		}
		q := tx.Table("workforce_credential_reviews").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND credential_id=?", p.TenantID, hospital, staff, credential).
			Order("created_at DESC, id DESC").Limit(params.Limit + 1)
		if params.After != nil {
			q = q.Where("(created_at,id)<(?,?)", params.After.CreatedAt, params.After.ID)
		}
		var err error
		rows, err = loadCredentialReviews(q)
		return err
	})
	if err != nil {
		respondCredentialError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) putCredentialReview(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	ids, ok := credentialIDs(c, "id", "credential", "review")
	if !ok {
		return
	}
	staff, credential, review := ids[0], ids[1], ids[2]
	var in credentialReviewInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if err := normalizeCredentialReview(&in); err != nil {
		respondCredentialError(c, err)
		return
	}
	var result credentialReview
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		// Same advisory key as the enrollment form, so a review and an enrollment save never interleave.
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_enrollment", staff.String()); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		existing, err := loadCredentialReviews(tx.Table("workforce_credential_reviews").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, review))
		if err != nil {
			return err
		}
		if len(existing) == 1 {
			if !sameCredentialProposal(existing[0], in, staff, credential, p.Subject) {
				return errCredentialReplay
			}
			result = existing[0]
			return nil
		}
		var enrolled struct{ Type, Issuer string }
		if err := tx.Table("workforce_enrollment_credentials").Select("type,issuer").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, credential).
			Clauses(clause.Locking{Strength: "SHARE"}).Take(&enrolled).Error; err != nil {
			return err
		}
		current, err := loadCredentialValidity(tx, p.TenantID, hospital, staff, &credential, true)
		if err != nil {
			return err
		}
		v, reviewed := current[credential]
		if v.Version != *in.ExpectedVersion {
			return errCredentialVersion
		}
		if reviewed && v.Number == in.Number && v.ValidFrom == in.ValidFrom && v.ValidUntil == in.ValidUntil {
			return errCredentialUnchanged
		}
		var pending bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_credential_reviews WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND credential_id=? AND status='pending')`, p.TenantID, hospital, staff, credential).Scan(&pending).Error; err != nil {
			return err
		}
		if pending {
			return errCredentialPending
		}
		if err := tx.Exec(`INSERT INTO workforce_credential_reviews(tenant_id,hospital_id,id,staff_id,credential_id,base_version,status,type,number,issuer,valid_from,valid_until,evidence_document_ids,reason,proposed_by_subject,decision_note)
 VALUES(?,?,?,?,?,?,'pending',?,?,?,?::date,?::date,?::uuid[],?,?,'')`,
			p.TenantID, hospital, review, staff, credential, v.Version, enrolled.Type, in.Number, enrolled.Issuer, in.ValidFrom, in.ValidUntil, uuidArrayLiteral(in.EvidenceDocumentIDs), in.Reason, p.Subject).Error; err != nil {
			return err
		}
		if err := recordCredentialReview(h, tx, p.TenantID, hospital, p.Subject, "pending", staff, credential, review, v.Version); err != nil {
			return err
		}
		if err := tx.Exec(`SET CONSTRAINTS ALL IMMEDIATE`).Error; err != nil {
			return err
		}
		result, err = loadCredentialReview(tx, p.TenantID, hospital, staff, credential, review, false)
		return err
	})
	if err != nil {
		respondCredentialError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) decideCredentialReview(c *gin.Context) {
	var in credentialDecisionInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	to := map[string]string{"approve": "approved", "reject": "rejected"}[in.Decision]
	h.transitionCredentialReview(c, in.ExpectedVersion, to, in.Note)
}

func (h *handlers) withdrawCredentialReview(c *gin.Context) {
	var in credentialWithdrawInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	h.transitionCredentialReview(c, nil, "withdrawn", in.Note)
}

// transitionCredentialReview decides a pending review once; approval writes the reviewed record and the enrollment row in the same transaction.
func (h *handlers) transitionCredentialReview(c *gin.Context, expected *int64, to, note string) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	ids, ok := credentialIDs(c, "id", "credential", "review")
	if !ok {
		return
	}
	staff, credential, review := ids[0], ids[1], ids[2]
	note = strings.TrimSpace(note)
	if !validStatutoryText(note) || utf8.RuneCountInString(note) > 500 {
		respondCredentialError(c, errCredentialInvalid)
		return
	}
	if to == "rejected" && note == "" {
		respondCredentialError(c, errCredentialNoteRequired)
		return
	}
	var result credentialReview
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_enrollment", staff.String()); err != nil {
			return err
		}
		r, err := loadCredentialReview(tx, p.TenantID, hospital, staff, credential, review, true)
		if err != nil {
			return err
		}
		if r.Status == to && r.DecidedBySubject != nil && *r.DecidedBySubject == p.Subject && r.DecisionNote == note {
			result = r
			return nil
		}
		if r.Status != "pending" {
			return errCredentialState
		}
		current, err := loadCredentialValidity(tx, p.TenantID, hospital, staff, &credential, true)
		if err != nil {
			return err
		}
		v, reviewed := current[credential]
		if v.Version != r.BaseVersion || (expected != nil && v.Version != *expected) {
			return errCredentialVersion
		}
		if to == "withdrawn" && r.ProposedBySubject != p.Subject {
			return errCredentialNotProposer
		}
		if to != "withdrawn" && r.ProposedBySubject == p.Subject {
			return errCredentialSelf
		}
		set := "status=?,decided_by_subject=?,decided_at=clock_timestamp(),decision_note=?"
		if to == "approved" {
			set += ",approved_version=base_version+1"
		}
		update := tx.Exec(`UPDATE workforce_credential_reviews SET `+set+` WHERE tenant_id=? AND hospital_id=? AND id=? AND status='pending'`, to, p.Subject, note, p.TenantID, hospital, review)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errCredentialState
		}
		version := v.Version
		if to == "approved" {
			version++
			if err := approveCredential(h, tx, p.TenantID, hospital, p.Subject, r, reviewed, version); err != nil {
				return err
			}
		}
		if err := recordCredentialReview(h, tx, p.TenantID, hospital, p.Subject, to, staff, credential, review, version); err != nil {
			return err
		}
		if err := tx.Exec(`SET CONSTRAINTS ALL IMMEDIATE`).Error; err != nil {
			return err
		}
		result, err = loadCredentialReview(tx, p.TenantID, hospital, staff, credential, review, false)
		return err
	})
	if err != nil {
		respondCredentialError(c, err)
		return
	}
	respond.OK(c, result)
}

// approveCredential copies the review into the reviewed record and the enrollment row, then bumps the staff version so stale enrollment forms get a 409.
func approveCredential(h *handlers, tx *gorm.DB, tenant string, hospital uuid.UUID, subject string, r credentialReview, reviewed bool, version int64) error {
	if err := tx.Exec(`SELECT set_config('app.workforce_credential_review','authorized',true)`).Error; err != nil {
		return err
	}
	if reviewed {
		if err := tx.Exec(`UPDATE workforce_credential_validity SET number=?,valid_from=?::date,valid_until=?::date,version=?,review_id=?,updated_at=clock_timestamp()
 WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND credential_id=? AND version=?`,
			r.Number, r.ValidFrom, r.ValidUntil, version, r.ID, tenant, hospital, r.StaffID, r.CredentialID, version-1).Error; err != nil {
			return err
		}
	} else if err := tx.Exec(`INSERT INTO workforce_credential_validity(tenant_id,hospital_id,staff_id,credential_id,type,number,issuer,valid_from,valid_until,version,review_id) VALUES(?,?,?,?,?,?,?,?::date,?::date,1,?)`,
		tenant, hospital, r.StaffID, r.CredentialID, r.Type, r.Number, r.Issuer, r.ValidFrom, r.ValidUntil, r.ID).Error; err != nil {
		return err
	}
	enrolled := tx.Exec(`UPDATE workforce_enrollment_credentials SET number=?,valid_from=?::date,valid_until=?::date WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=?`,
		r.Number, r.ValidFrom, r.ValidUntil, tenant, hospital, r.StaffID, r.CredentialID)
	if enrolled.Error != nil {
		return enrolled.Error
	}
	if enrolled.RowsAffected != 1 {
		return errCredentialRemoved
	}
	var staffVersion int64
	if err := tx.Raw(`UPDATE workforce_staff SET updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? RETURNING version`, tenant, hospital, r.StaffID).Scan(&staffVersion).Error; err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.enrollment.changed',?,jsonb_build_object('version',?::bigint))`,
		tenant, hospital, subject, r.StaffID, staffVersion).Error; err != nil {
		return err
	}
	enrollment, err := json.Marshal(workforcecontract.StaffEnrollmentChangedData{HospitalID: hospital.String(), StaffID: r.StaffID.String(), Version: staffVersion})
	if err != nil {
		return err
	}
	if err := h.bus.Publish(tx, EnrollmentChanged, events.Event{Type: EnrollmentChanged, Version: 1, TenantID: tenant, Data: enrollment}); err != nil {
		return err
	}
	if err := supersedeOlderReminders(h, tx, tenant, hospital, r.StaffID, r.CredentialID, version); err != nil {
		return err
	}
	data, err := json.Marshal(workforcecontract.CredentialValidityChangedData{HospitalID: hospital.String(), StaffID: r.StaffID.String(), CredentialID: r.CredentialID.String(), ReviewID: r.ID.String(), Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, CredentialValidityChanged, events.Event{Type: CredentialValidityChanged, Version: 1, TenantID: tenant, Data: data})
}

func (h *handlers) credentialRegister(c *gin.Context, params pagination.Params) ([]credentialRegisterEntry, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	within := -1
	if raw, set := c.GetQuery("expiring_within"); set {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 365 || strconv.Itoa(n) != raw {
			respond.Error(c, 400, "invalid_request", "Use expiring_within from 0 to 365 days.")
			return nil, nil
		}
		within = n
	}
	var rows []credentialRegisterEntry
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := enrollmentScope(tx, p, hospital); err != nil {
			return err
		}
		loc, now, err := credentialClock(tx, p.TenantID, hospital)
		if err != nil {
			return err
		}
		q := tx.Table("workforce_credential_validity v").Joins("JOIN workforce_staff s ON s.tenant_id=v.tenant_id AND s.hospital_id=v.hospital_id AND s.id=v.staff_id").
			Select("v.staff_id,s.employee_code,s.display_name,v.credential_id,v.type,v.number,v.issuer,v.valid_from::text AS valid_from,v.valid_until::text AS valid_until,v.version,v.review_id").
			Where("v.tenant_id=? AND v.hospital_id=?", p.TenantID, hospital).Order("v.valid_until, v.review_id").Limit(params.Limit + 1)
		if within >= 0 {
			// Filtering by a guessed calendar would silently drop or add credentials, so it needs the hospital zone.
			if loc == nil {
				return errCredentialZoneUnset
			}
			today, _ := time.Parse(time.DateOnly, now.In(loc).Format(time.DateOnly))
			q = q.Where("v.valid_until <= ?::date", today.AddDate(0, 0, within).Format(time.DateOnly))
		}
		if params.After != nil {
			q = q.Where("(v.valid_until,v.review_id) > (?::date,?)", params.After.CreatedAt.UTC().Format(time.DateOnly), params.After.ID)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			rows[i].Status, rows[i].DaysRemaining = credentialStanding(rows[i].ValidFrom, rows[i].ValidUntil, loc, now)
		}
		return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.credentials.register_read',?,jsonb_build_object('expiring_within',?::int))`,
			p.TenantID, hospital, p.Subject, hospital, within).Error
	})
	if err != nil {
		respondCredentialError(c, err)
		return nil, nil
	}
	return rows, nil
}

func respondCredentialError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	isPG := errors.As(err, &pg)
	switch {
	case errors.Is(err, errCredentialInvalid), isPG && pg.ConstraintName == "workforce_credential_review_period":
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Give the licence number, valid dates with the end on or after the start, a reason, and 1 to 5 different evidence files.")
	case errors.Is(err, errCredentialNoteRequired), isPG && pg.ConstraintName == "workforce_credential_review_rejection_note":
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Give a reason when rejecting a review.")
	case errors.Is(err, errCredentialSelf), isPG && pg.ConstraintName == "workforce_credential_review_separation":
		respond.ConflictWithDetail(c, "Someone other than the person who proposed the review must approve or reject it.", gin.H{"reason": "self_review"})
	case errors.Is(err, errCredentialNotProposer), isPG && pg.ConstraintName == "workforce_credential_review_withdrawn_by_proposer":
		respond.ConflictWithDetail(c, "Only the person who proposed the review can withdraw it.", gin.H{"reason": "not_proposer"})
	case errors.Is(err, errCredentialVersion):
		respond.ConflictWithDetail(c, "The reviewed credential changed since you opened it. Reload before saving.", gin.H{"reason": "version_conflict"})
	case errors.Is(err, errCredentialPending), isPG && pg.ConstraintName == "workforce_credential_review_one_pending":
		respond.ConflictWithDetail(c, "Another review of this credential is waiting for a decision.", gin.H{"reason": "review_pending"})
	case errors.Is(err, errCredentialReplay):
		respond.ConflictWithDetail(c, "This review was already saved with different details.", gin.H{"reason": "review_conflict"})
	case errors.Is(err, errCredentialUnchanged):
		respond.ConflictWithDetail(c, "The review would leave the reviewed credential as it is.", gin.H{"reason": "nothing_to_change"})
	case isPG && pg.ConstraintName == "workforce_credential_review_evidence":
		respond.ConflictWithDetail(c, "Cite only this credential's evidence files that are scanned or still scanning.", gin.H{"reason": "evidence_invalid"})
	case isPG && pg.ConstraintName == "workforce_credential_review_evidence_unavailable":
		respond.ConflictWithDetail(c, "Every evidence file must finish scanning and be released before approval.", gin.H{"reason": "evidence_unavailable"})
	case errors.Is(err, errCredentialState):
		respond.ConflictWithDetail(c, "This review has already been decided.", gin.H{"reason": "not_pending"})
	case errors.Is(err, errCredentialRemoved):
		respond.ConflictWithDetail(c, "The credential was removed from enrollment, so the review cannot be approved.", gin.H{"reason": "credential_removed"})
	case errors.Is(err, errCredentialZoneUnset):
		respond.ConflictWithDetail(c, "Set the hospital time zone in roster settings before filtering by expiry.", gin.H{"reason": "time_zone_unset"})
	case isPG && pg.Code == "23505":
		requestid.Logger(c).WarnContext(c.Request.Context(), "credential review uniqueness conflict", "constraint", pg.ConstraintName)
		respond.ConflictWithDetail(c, "Another credential of this staff member already has that number.", gin.H{"reason": "duplicate_credential"})
	default:
		respondError(c, err)
	}
}
