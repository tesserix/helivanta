package workforce

import (
	"context"
	"encoding/json"
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

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/transit"
)

func (r staffIdentifierRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

var errIdentifierInvalid = errors.New("staff identifiers are invalid")
var errIdentifierVersion = errors.New("staff identifiers version conflict")
var errIdentifierPending = errors.New("a staff identifier proposal is already pending")
var errIdentifierState = errors.New("staff identifier proposal is not pending")
var errIdentifierSelf = errors.New("staff identifiers decided by their author")
var errIdentifierNotProposer = errors.New("staff identifiers withdrawn by someone else")
var errIdentifierNoteRequired = errors.New("staff identifier rejection needs a note")
var errIdentifierUnchanged = errors.New("staff identifier proposal changes nothing")
var errIdentifierPanInUse = errors.New("pan belongs to another staff member")
var errIdentifierOwnProposal = errors.New("staff identifier proposal revealed by its author")
var errIdentifierNotRevealable = errors.New("staff identifier revision is not in force or pending")

// One lock per hospital serialises proposals and decisions, so the PAN check sees every other staff member's.
const staffIdentifiersLock = "staff-identifiers"

const staffIdentifierRevisionColumns = "id,staff_id,sequence,base_version,status,approved_version,reason,bank_account_masked,bank_ifsc,bank_holder_name,pan_masked,uan_masked,proposed_by_subject,decided_by_subject,decided_at,decision_note,created_at"

var (
	accountNumberShape = regexp.MustCompile(`^[0-9]{9,18}$`)
	ifscShape          = regexp.MustCompile(`^[A-Z]{4}0[A-Z0-9]{6}$`)
	individualPANShape = regexp.MustCompile(`^[A-Z]{3}P[A-Z][0-9]{4}[A-Z]$`)
	uanShape           = regexp.MustCompile(`^[0-9]{12}$`)
)

// sealedIdentifiers is a revision's stored form: ciphertext, masks and the PAN fingerprint, never plaintext.
type sealedIdentifiers struct {
	BankAccountCiphertext *string
	BankAccountMasked     *string
	BankIfsc              *string
	BankHolderName        *string
	PanCiphertext         *string
	PanMasked             *string
	PanIndex              *string
	UanCiphertext         *string
	UanMasked             *string
}

const sealedIdentifierColumns = "bank_account_ciphertext,bank_account_masked,bank_ifsc,bank_holder_name,pan_ciphertext,pan_masked,pan_index,uan_ciphertext,uan_masked"

func staffKeyName(tenant string) (string, error) {
	id, err := uuid.Parse(tenant)
	if err != nil {
		return "", err
	}
	return "helivanta-staff-" + id.String(), nil
}

// identifierContext binds a ciphertext to its tenant key, hospital, staff member and field; it names no revision so keep can copy it.
func identifierContext(hospital, staff uuid.UUID, field string) []byte {
	return []byte("helivanta/workforce/staff-identifier/v1|" + hospital.String() + "|" + staff.String() + "|" + field)
}

func strPtr(v string) *string { return &v }

// Mirrors the table checks so a malformed proposal is a 400 before the vault or database is touched.
func normalizeStaffIdentifiers(in *staffIdentifiersInput) error {
	in.Reason = collapseSpaces(in.Reason)
	if in.Reason == "" || !validStatutoryText(in.Reason) {
		return errIdentifierInvalid
	}
	b := &in.Bank
	if b.Action == "set" {
		if b.AccountNumber == nil || b.Ifsc == nil || b.HolderName == nil {
			return errIdentifierInvalid
		}
		*b.AccountNumber = strings.ReplaceAll(*b.AccountNumber, " ", "")
		*b.Ifsc = strings.ToUpper(strings.TrimSpace(*b.Ifsc))
		*b.HolderName = collapseSpaces(*b.HolderName)
		if !accountNumberShape.MatchString(*b.AccountNumber) || !ifscShape.MatchString(*b.Ifsc) || *b.HolderName == "" || len([]rune(*b.HolderName)) > 150 || !validStatutoryText(*b.HolderName) {
			return errIdentifierInvalid
		}
	} else if b.AccountNumber != nil || b.Ifsc != nil || b.HolderName != nil {
		return errIdentifierInvalid
	}
	for _, f := range []struct {
		change *staffIdentifierChange
		shape  *regexp.Regexp
	}{{&in.Pan, individualPANShape}, {&in.Uan, uanShape}} {
		if f.change.Action != "set" {
			if f.change.Value != nil {
				return errIdentifierInvalid
			}
			continue
		}
		if f.change.Value == nil {
			return errIdentifierInvalid
		}
		*f.change.Value = strings.ToUpper(strings.TrimSpace(*f.change.Value))
		if !f.shape.MatchString(*f.change.Value) {
			return errIdentifierInvalid
		}
	}
	if in.Bank.Action == "keep" && in.Pan.Action == "keep" && in.Uan.Action == "keep" {
		return errIdentifierUnchanged
	}
	return nil
}

// sealIdentifiers encrypts every set field before any lock is taken; Transit errors surface as transit.ErrUnavailable.
func (h *handlers) sealIdentifiers(ctx context.Context, tenant string, hospital, staff uuid.UUID, in staffIdentifiersInput) (sealedIdentifiers, error) {
	var out sealedIdentifiers
	if in.Bank.Action != "set" && in.Pan.Action != "set" && in.Uan.Action != "set" {
		return out, nil
	}
	if h.transit == nil {
		return out, transit.ErrUnavailable
	}
	key, err := staffKeyName(tenant)
	if err != nil {
		return out, err
	}
	seal := func(field, value string) (*string, error) {
		ciphertext, err := h.transit.Encrypt(ctx, key, identifierContext(hospital, staff, field), []byte(value))
		if err != nil {
			return nil, err
		}
		return &ciphertext, nil
	}
	if in.Bank.Action == "set" {
		account := *in.Bank.AccountNumber
		if out.BankAccountCiphertext, err = seal("bank_account", account); err != nil {
			return out, err
		}
		out.BankAccountMasked, out.BankIfsc, out.BankHolderName = strPtr("XXXX"+account[len(account)-4:]), in.Bank.Ifsc, in.Bank.HolderName
	}
	if in.Pan.Action == "set" {
		pan := *in.Pan.Value
		if out.PanCiphertext, err = seal("pan", pan); err != nil {
			return out, err
		}
		index, err := h.transit.HMAC(ctx, key, []byte(pan))
		if err != nil {
			return out, err
		}
		out.PanMasked, out.PanIndex = strPtr("XXXXXX"+pan[6:]), &index
	}
	if in.Uan.Action == "set" {
		uan := *in.Uan.Value
		if out.UanCiphertext, err = seal("uan", uan); err != nil {
			return out, err
		}
		out.UanMasked = strPtr("XXXXXXXX" + uan[8:])
	}
	return out, nil
}

// mergeIdentifiers applies keep from the revision in force; it reports whether anything differs from it.
func mergeIdentifiers(in staffIdentifiersInput, set, current sealedIdentifiers) (sealedIdentifiers, bool) {
	out, changed := set, false
	switch in.Bank.Action {
	case "keep":
		out.BankAccountCiphertext, out.BankAccountMasked, out.BankIfsc, out.BankHolderName = current.BankAccountCiphertext, current.BankAccountMasked, current.BankIfsc, current.BankHolderName
	case "set":
		changed = true
	case "clear":
		changed = changed || current.BankAccountCiphertext != nil
	}
	switch in.Pan.Action {
	case "keep":
		out.PanCiphertext, out.PanMasked, out.PanIndex = current.PanCiphertext, current.PanMasked, current.PanIndex
	case "set":
		changed = changed || current.PanIndex == nil || *current.PanIndex != *set.PanIndex
	case "clear":
		changed = changed || current.PanCiphertext != nil
	}
	switch in.Uan.Action {
	case "keep":
		out.UanCiphertext, out.UanMasked = current.UanCiphertext, current.UanMasked
	case "set":
		changed = true
	case "clear":
		changed = changed || current.UanCiphertext != nil
	}
	return out, changed
}

func loadStaffIdentifiers(tx *gorm.DB, tenant string, hospital, staff uuid.UUID, lock bool) (staffIdentifiers, error) {
	var parent staffIdentifiers
	q := tx.Table("workforce_staff_identifiers").Select("staff_id,version,created_at,updated_at").Where("tenant_id=? AND hospital_id=? AND staff_id=?", tenant, hospital, staff)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&parent).Error; err != nil {
		return parent, err
	}
	var revisions []staffIdentifierRevision
	if err := tx.Table("workforce_staff_identifier_revisions").Select(staffIdentifierRevisionColumns).
		Where("tenant_id=? AND hospital_id=? AND staff_id=? AND (status='pending' OR id=(SELECT revision_id FROM workforce_staff_identifiers WHERE tenant_id=? AND hospital_id=? AND staff_id=?))", tenant, hospital, staff, tenant, hospital, staff).
		Find(&revisions).Error; err != nil {
		return parent, err
	}
	for i := range revisions {
		if revisions[i].Status == "pending" {
			parent.Pending = &revisions[i]
		} else {
			parent.Current = &revisions[i]
		}
	}
	return parent, nil
}

func currentSealed(tx *gorm.DB, tenant string, hospital, staff uuid.UUID) (sealedIdentifiers, error) {
	var out sealedIdentifiers
	err := tx.Table("workforce_staff_identifier_revisions r").Select("r."+strings.ReplaceAll(sealedIdentifierColumns, ",", ",r.")).
		Joins("JOIN workforce_staff_identifiers s ON (s.tenant_id,s.hospital_id,s.revision_id)=(r.tenant_id,r.hospital_id,r.id)").
		Where("s.tenant_id=? AND s.hospital_id=? AND s.staff_id=?", tenant, hospital, staff).Take(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, nil
	}
	return out, err
}

func (h *handlers) getStaffIdentifiers(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	var result staffIdentifiers
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		if result, err = loadStaffIdentifiers(tx, p.TenantID, hospital, staff, false); err != nil {
			return err
		}
		if result.Current == nil && result.Pending == nil {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		respondIdentifierError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) staffIdentifierHistory(c *gin.Context, params pagination.Params) ([]staffIdentifierRevision, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	staff, ok := staffParam(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []staffIdentifierRevision{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx.Table("workforce_staff_identifier_revisions"), params).Select(staffIdentifierRevisionColumns).
			Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staff).Find(&rows).Error
	})
	if err != nil {
		respondIdentifierError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) putStaffIdentifiers(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	var in staffIdentifiersInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if err := normalizeStaffIdentifiers(&in); err != nil {
		respondIdentifierError(c, err)
		return
	}
	// Authorise before encrypting, so a caller without the grant never drives the vault.
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, true); err != nil {
			return err
		}
		return requireHospitalStaff(tx, p.TenantID, hospital, staff)
	})
	if err != nil {
		respondIdentifierError(c, err)
		return
	}
	set, err := h.sealIdentifiers(c.Request.Context(), p.TenantID, hospital, staff, in)
	if err != nil {
		respondIdentifierError(c, err)
		return
	}
	var result staffIdentifiers
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", staffIdentifiersLock); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		current, err := loadStaffIdentifiers(tx, p.TenantID, hospital, staff, true)
		switch {
		case err == nil:
			if current.Pending != nil {
				return errIdentifierPending
			}
			if current.Version != *in.ExpectedVersion {
				return errIdentifierVersion
			}
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		case *in.ExpectedVersion != 0:
			return errIdentifierVersion
		default:
			if err := tx.Exec(`INSERT INTO workforce_staff_identifiers(tenant_id,hospital_id,staff_id,version) VALUES(?,?,?,0)`, p.TenantID, hospital, staff).Error; err != nil {
				return err
			}
		}
		inForce, err := currentSealed(tx, p.TenantID, hospital, staff)
		if err != nil {
			return err
		}
		sealed, changed := mergeIdentifiers(in, set, inForce)
		if !changed {
			return errIdentifierUnchanged
		}
		if sealed.PanIndex != nil {
			var taken int64
			if err := tx.Raw(`SELECT count(*) FROM (SELECT 1 FROM workforce_staff_identifiers WHERE tenant_id=? AND hospital_id=? AND pan_index=? AND staff_id<>?
 UNION ALL SELECT 1 FROM workforce_staff_identifier_revisions WHERE tenant_id=? AND hospital_id=? AND status='pending' AND pan_index=? AND staff_id<>?) t`,
				p.TenantID, hospital, *sealed.PanIndex, staff, p.TenantID, hospital, *sealed.PanIndex, staff).Scan(&taken).Error; err != nil {
				return err
			}
			if taken > 0 {
				return errIdentifierPanInUse
			}
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_staff_identifier_revisions(tenant_id,hospital_id,id,staff_id,sequence,base_version,status,reason,proposed_by_subject,decision_note,`+sealedIdentifierColumns+`)
 SELECT ?,?,?,?,coalesce((SELECT max(sequence) FROM workforce_staff_identifier_revisions WHERE tenant_id=? AND hospital_id=? AND staff_id=?),0)+1,?,'pending',?,?,'',?,?,?,?,?,?,?,?,?`,
			p.TenantID, hospital, revision, staff, p.TenantID, hospital, staff, *in.ExpectedVersion, in.Reason, p.Subject,
			sealed.BankAccountCiphertext, sealed.BankAccountMasked, sealed.BankIfsc, sealed.BankHolderName, sealed.PanCiphertext, sealed.PanMasked, sealed.PanIndex, sealed.UanCiphertext, sealed.UanMasked).Error; err != nil {
			return err
		}
		if err := recordIdentifierChange(h, tx, p.TenantID, hospital, p.Subject, "proposed", staff, revision, *in.ExpectedVersion); err != nil {
			return err
		}
		result, err = loadStaffIdentifiers(tx, p.TenantID, hospital, staff, false)
		return err
	})
	if err != nil {
		respondIdentifierError(c, err)
		return
	}
	respond.OK(c, result)
}

// Audit rows and events carry identifiers, status and version only; values and masks stay behind the API.
func recordIdentifierChange(h *handlers, tx *gorm.DB, tenant string, hospital uuid.UUID, subject, status string, staff, revision uuid.UUID, version int64) error {
	if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('revision_id',?::uuid,'version',?::bigint))`,
		tenant, hospital, subject, "workforce.staff_identifiers."+status, staff, revision, version).Error; err != nil {
		return err
	}
	data, err := json.Marshal(workforcecontract.StaffIdentifiersChangedData{HospitalID: hospital.String(), StaffID: staff.String(), RevisionID: revision.String(), Status: status, Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, StaffIdentifiersChanged, events.Event{Type: StaffIdentifiersChanged, Version: 1, TenantID: tenant, Data: data})
}

func (h *handlers) decideStaffIdentifiers(c *gin.Context) {
	var in statutoryDecisionInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	to := map[string]string{"approve": "approved", "reject": "rejected"}[in.Decision]
	h.transitionIdentifiers(c, in.ProposalID, in.ExpectedVersion, to, in.Note)
}

func (h *handlers) withdrawStaffIdentifiers(c *gin.Context) {
	var in statutoryWithdrawInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	h.transitionIdentifiers(c, in.ProposalID, nil, "withdrawn", in.Note)
}

// transitionIdentifiers decides a pending proposal once; rejecting or withdrawing erases its ciphertext in the same statement.
func (h *handlers) transitionIdentifiers(c *gin.Context, proposal uuid.UUID, expected *int64, to, note string) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	if proposal == uuid.Nil {
		respond.NotFound(c, "staff identifier proposal")
		return
	}
	note = strings.TrimSpace(note)
	if !validStatutoryText(note) {
		respondIdentifierError(c, errIdentifierInvalid)
		return
	}
	if to == "rejected" && note == "" {
		respondIdentifierError(c, errIdentifierNoteRequired)
		return
	}
	var result staffIdentifiers
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", staffIdentifiersLock); err != nil {
			return err
		}
		parent, err := loadStaffIdentifiers(tx, p.TenantID, hospital, staff, true)
		if err != nil {
			return err
		}
		var r struct {
			Status            string
			ProposedBySubject string
			DecidedBySubject  *string
			DecisionNote      string
		}
		if err := tx.Table("workforce_staff_identifier_revisions").Select("status,proposed_by_subject,decided_by_subject,decision_note").
			Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, proposal).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&r).Error; err != nil {
			return err
		}
		if r.Status == to && r.DecidedBySubject != nil && *r.DecidedBySubject == p.Subject && r.DecisionNote == note {
			result = parent
			return nil
		}
		if r.Status != "pending" {
			return errIdentifierState
		}
		if expected != nil && parent.Version != *expected {
			return errIdentifierVersion
		}
		if to == "withdrawn" && r.ProposedBySubject != p.Subject {
			return errIdentifierNotProposer
		}
		if to != "withdrawn" && r.ProposedBySubject == p.Subject {
			return errIdentifierSelf
		}
		set := "status=?,decided_by_subject=?,decided_at=clock_timestamp(),decision_note=?"
		if to == "approved" {
			set += ",approved_version=base_version+1"
		} else {
			set += ",bank_account_ciphertext=NULL,pan_ciphertext=NULL,pan_index=NULL,uan_ciphertext=NULL"
		}
		update := tx.Exec(`UPDATE workforce_staff_identifier_revisions SET `+set+` WHERE tenant_id=? AND hospital_id=? AND id=? AND status='pending'`, to, p.Subject, note, p.TenantID, hospital, proposal)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errIdentifierState
		}
		version := parent.Version
		if to == "approved" {
			version++
			if err := tx.Exec(`UPDATE workforce_staff_identifiers SET version=?,revision_id=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND version=?`,
				version, proposal, p.TenantID, hospital, staff, parent.Version).Error; err != nil {
				return err
			}
		}
		if err := recordIdentifierChange(h, tx, p.TenantID, hospital, p.Subject, to, staff, proposal, version); err != nil {
			return err
		}
		result, err = loadStaffIdentifiers(tx, p.TenantID, hospital, staff, false)
		return err
	})
	if err != nil {
		respondIdentifierError(c, err)
		return
	}
	respond.OK(c, result)
}

var revealColumns = map[string]string{"bank_account": "bank_account_ciphertext", "pan": "pan_ciphertext", "uan": "uan_ciphertext"}

// revealStaffIdentifier audits the view and decrypts inside one transaction, so no value leaves without its audit row.
func (h *handlers) revealStaffIdentifier(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	c.Header("Cache-Control", "no-store")
	var in staffIdentifierRevealInput
	if !decodeSized(c, &in, 1024) {
		return
	}
	in.Reason = collapseSpaces(in.Reason)
	column, known := revealColumns[in.Field]
	if !known || in.RevisionID == uuid.Nil || in.Reason == "" || len([]rune(in.Reason)) > 200 || !validStatutoryText(in.Reason) {
		respond.Error(c, 400, "invalid_request", "Choose a field and give a reason of up to 200 characters for viewing it.")
		return
	}
	if h.transit == nil {
		respondIdentifierError(c, transit.ErrUnavailable)
		return
	}
	key, err := staffKeyName(p.TenantID)
	if err != nil {
		respondIdentifierError(c, err)
		return
	}
	var result staffIdentifierReveal
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", staffIdentifiersLock+":"+staff.String()); err != nil {
			return err
		}
		var r struct {
			Status            string
			ProposedBySubject string
			InForce           bool
			Ciphertext        *string
		}
		if err := tx.Table("workforce_staff_identifier_revisions r").
			Select("r.status,r.proposed_by_subject,r."+column+" AS ciphertext,EXISTS(SELECT 1 FROM workforce_staff_identifiers s WHERE (s.tenant_id,s.hospital_id,s.staff_id,s.revision_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.id)) AS in_force").
			Where("r.tenant_id=? AND r.hospital_id=? AND r.staff_id=? AND r.id=?", p.TenantID, hospital, staff, in.RevisionID).Clauses(clause.Locking{Strength: "SHARE", Table: clause.Table{Name: "r"}}).Take(&r).Error; err != nil {
			return err
		}
		switch {
		case r.Status == "pending" && r.ProposedBySubject == p.Subject:
			return errIdentifierOwnProposal
		case r.Status != "pending" && !r.InForce:
			return errIdentifierNotRevealable
		case r.Ciphertext == nil:
			return gorm.ErrRecordNotFound
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('revision_id',?::uuid,'field',?::text,'reason',?::text))`,
			p.TenantID, hospital, p.Subject, "workforce.staff_identifier.revealed", staff, in.RevisionID, in.Field, in.Reason).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.StaffIdentifierRevealedData{HospitalID: hospital.String(), StaffID: staff.String(), RevisionID: in.RevisionID.String(), Field: in.Field})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, StaffIdentifierRevealed, events.Event{Type: StaffIdentifierRevealed, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		plain, err := h.transit.Decrypt(c.Request.Context(), key, identifierContext(hospital, staff, in.Field), *r.Ciphertext)
		if err != nil {
			return err
		}
		result = staffIdentifierReveal{RevisionID: in.RevisionID, Field: in.Field, Value: string(plain)}
		return nil
	})
	if err != nil {
		respondIdentifierError(c, err)
		return
	}
	respond.OK(c, result)
}

func respondIdentifierError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	isPG := errors.As(err, &pg)
	switch {
	case errors.Is(err, transit.ErrUnavailable):
		requestid.Logger(c).WarnContext(c.Request.Context(), "identifier vault unavailable", "err", err)
		respond.Error(c, http.StatusServiceUnavailable, "identifier_vault_unavailable", "Bank and tax identifiers cannot be changed or viewed right now. Masked values are still shown; try again shortly.")
	case errors.Is(err, transit.ErrIntegrity):
		// A ciphertext that no longer authenticates was altered or moved in the database; it is never shown.
		respond.InternalErr(c, err, "The stored identifier failed its integrity check.")
	case errors.Is(err, errIdentifierInvalid):
		respond.Error(c, 400, "invalid_request", "Give a reason, a 9 to 18 digit account number with its IFSC and holder name, an individual's PAN and a 12-digit UAN, and send values only for fields you set.")
	case errors.Is(err, errIdentifierNoteRequired):
		respond.Error(c, 400, "invalid_request", "Give a reason when rejecting a proposal.")
	case errors.Is(err, errIdentifierSelf), isPG && pg.ConstraintName == "workforce_staff_identifier_separation":
		respond.Forbidden(c, "Someone other than the person who proposed the change must approve or reject it.")
	case errors.Is(err, errIdentifierNotProposer), isPG && pg.ConstraintName == "workforce_staff_identifier_withdrawn_by_proposer":
		respond.Forbidden(c, "Only the person who proposed the change can withdraw it.")
	case errors.Is(err, errIdentifierOwnProposal):
		respond.Forbidden(c, "Someone other than the person who proposed these values must check them.")
	case errors.Is(err, errIdentifierVersion):
		respond.Conflict(c, "The staff member's identifiers changed since you opened them. Reload before saving.")
	case errors.Is(err, errIdentifierPending), isPG && pg.ConstraintName == "workforce_staff_identifier_one_pending":
		respond.ConflictWithDetail(c, "Another identifier proposal for this staff member is waiting for a decision.", gin.H{"reason": "proposal_pending"})
	case errors.Is(err, errIdentifierState):
		respond.ConflictWithDetail(c, "This proposal has already been decided.", gin.H{"reason": "not_pending"})
	case errors.Is(err, errIdentifierUnchanged):
		respond.ConflictWithDetail(c, "The proposal would leave every identifier as it is.", gin.H{"reason": "nothing_to_change"})
	case errors.Is(err, errIdentifierPanInUse), isPG && (pg.ConstraintName == "workforce_staff_identifier_pan_in_use" || pg.ConstraintName == "workforce_staff_identifier_pending_pan"):
		respond.ConflictWithDetail(c, "This PAN is already recorded or proposed for another staff member in this hospital.", gin.H{"reason": "pan_in_use"})
	case errors.Is(err, errIdentifierNotRevealable):
		respond.ConflictWithDetail(c, "Only the identifiers in force or a pending proposal can be viewed.", gin.H{"reason": "not_revealable"})
	case isPG && pg.ConstraintName == "workforce_staff_identifier_staff_inactive":
		respond.ConflictWithDetail(c, "Identifiers can only be proposed for an active staff member.", gin.H{"reason": "staff_inactive"})
	default:
		respondError(c, err)
	}
}
