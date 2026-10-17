package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
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

func (r taxDeclarationRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

var errDeclarationInvalid = errors.New("tax declaration is invalid")
var errDeclarationVersion = errors.New("tax declaration version conflict")
var errDeclarationPending = errors.New("a tax declaration proposal is already pending")
var errDeclarationState = errors.New("tax declaration proposal is not pending")
var errDeclarationSelf = errors.New("tax declaration decided by its author")
var errDeclarationNotProposer = errors.New("tax declaration withdrawn by someone else")
var errDeclarationNoteRequired = errors.New("tax declaration rejection needs a note")
var errDeclarationUnchanged = errors.New("tax declaration proposal changes nothing")
var errDeclarationNothingToKeep = errors.New("tax declaration keeps a PAN that is not in force")
var errDeclarationOwnProposal = errors.New("tax declaration proposal revealed by its author")
var errDeclarationNotRevealable = errors.New("tax declaration revision is not in force or pending")

const taxDeclarationsLock = "tax-declarations"

const taxDeclarationRevisionColumns = "id,staff_id,tax_year,sequence,base_version,status,approved_version,reason,hra_metro,home_loan_interest_paise,proposed_by_subject,decided_by_subject,decided_at,decision_note,created_at"

// Landlords and lenders may be firms or trusts, so any PAN holder type is accepted.
var thirdPartyPANShape = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`)
var declarationCodeShape = regexp.MustCompile(`^[A-Z0-9_-]{1,40}$`)

// declarationParty is one landlord or lender as stored: ciphertext and mask, never plaintext.
type declarationParty struct {
	Role          string
	Position      int
	Name          string
	Address       string
	RentPaise     *int64
	PanCiphertext *string
	PanMasked     *string
	pan           taxDeclarationPanChange
}

// declarationContext binds a party PAN to its tenant key, hospital, staff member, year, role and position; it names no revision so keep can copy it.
func declarationContext(hospital, staff uuid.UUID, year, role string, position int) []byte {
	return []byte("helivanta/workforce/tax-declaration/v1|" + hospital.String() + "|" + staff.String() + "|" + year + "|" + role + "|" + strconv.Itoa(position))
}

func declarationYearParam(c *gin.Context) (string, bool) {
	year := c.Param("year")
	if !validTaxYear(year) {
		respond.NotFound(c, "tax declaration")
		return "", false
	}
	return year, true
}

func normalizeDeclarationPAN(pan *taxDeclarationPanChange) error {
	if pan.Action != "set" {
		if pan.Value != nil {
			return errDeclarationInvalid
		}
		return nil
	}
	if pan.Value == nil {
		return errDeclarationInvalid
	}
	*pan.Value = strings.ToUpper(strings.TrimSpace(*pan.Value))
	if !thirdPartyPANShape.MatchString(*pan.Value) {
		return errDeclarationInvalid
	}
	return nil
}

func normalizeDeclarationText(value *string, limit int) bool {
	*value = collapseSpaces(*value)
	return *value != "" && len([]rune(*value)) <= limit && validStatutoryText(*value)
}

// normalizeTaxDeclaration mirrors the table checks and flattens parties so a malformed proposal is 400 before the vault or database is touched.
func normalizeTaxDeclaration(in *taxDeclarationInput) ([]declarationParty, error) {
	if !normalizeDeclarationText(&in.Reason, 500) || len(in.Claims) > 24 {
		return nil, errDeclarationInvalid
	}
	codes := map[string]bool{}
	for i := range in.Claims {
		k := &in.Claims[i]
		k.Code = strings.ToUpper(strings.TrimSpace(k.Code))
		if !declarationCodeShape.MatchString(k.Code) || codes[k.Code] || k.AmountPaise == nil || *k.AmountPaise < 1 || *k.AmountPaise > 100000000000 {
			return nil, errDeclarationInvalid
		}
		codes[k.Code] = true
	}
	var parties []declarationParty
	add := func(role string, position int, name, address *string, rent *int64, pan *taxDeclarationPanChange) error {
		if !normalizeDeclarationText(name, 150) || !normalizeDeclarationText(address, 300) || normalizeDeclarationPAN(pan) != nil {
			return errDeclarationInvalid
		}
		parties = append(parties, declarationParty{Role: role, Position: position, Name: *name, Address: *address, RentPaise: rent, pan: *pan})
		return nil
	}
	if in.Hra != nil {
		if in.Hra.Metro == nil || len(in.Hra.Landlords) < 1 || len(in.Hra.Landlords) > 4 {
			return nil, errDeclarationInvalid
		}
		for i := range in.Hra.Landlords {
			l := &in.Hra.Landlords[i]
			if l.RentPaise == nil || *l.RentPaise < 1 || *l.RentPaise > 1000000000 {
				return nil, errDeclarationInvalid
			}
			if err := add("landlord", i+1, &l.Name, &l.Address, l.RentPaise, &l.Pan); err != nil {
				return nil, err
			}
		}
	}
	if in.HomeLoan != nil {
		if in.HomeLoan.InterestPaise == nil || *in.HomeLoan.InterestPaise < 1 || *in.HomeLoan.InterestPaise > 1000000000 || len(in.HomeLoan.Lenders) < 1 || len(in.HomeLoan.Lenders) > 4 {
			return nil, errDeclarationInvalid
		}
		for i := range in.HomeLoan.Lenders {
			l := &in.HomeLoan.Lenders[i]
			if err := add("lender", i+1, &l.Name, &l.Address, nil, &l.Pan); err != nil {
				return nil, err
			}
		}
	}
	return parties, nil
}

// sealDeclarationParties encrypts every set PAN before any lock is taken; Transit errors surface as transit.ErrUnavailable.
func (h *handlers) sealDeclarationParties(ctx context.Context, tenant string, hospital, staff uuid.UUID, year string, parties []declarationParty) error {
	if !slices.ContainsFunc(parties, func(d declarationParty) bool { return d.pan.Action == "set" }) {
		return nil
	}
	if h.transit == nil {
		return transit.ErrUnavailable
	}
	key, err := staffKeyName(tenant)
	if err != nil {
		return err
	}
	for i := range parties {
		d := &parties[i]
		if d.pan.Action != "set" {
			continue
		}
		pan := *d.pan.Value
		ciphertext, err := h.transit.Encrypt(ctx, key, declarationContext(hospital, staff, year, d.Role, d.Position), []byte(pan))
		if err != nil {
			return err
		}
		d.PanCiphertext, d.PanMasked = &ciphertext, strPtr("XXXXXX"+pan[6:])
	}
	return nil
}

func loadDeclarationRevisions(tx *gorm.DB, tenant string, hospital uuid.UUID, q *gorm.DB) ([]taxDeclarationRevision, error) {
	rows := []taxDeclarationRevision{}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return rows, nil
	}
	ids := make([]uuid.UUID, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for i := range rows {
		ids[i], index[rows[i].ID] = rows[i].ID, i
		rows[i].Claims, rows[i].Parties = []taxDeclarationClaimLine{}, []taxDeclarationParty{}
	}
	var claims []struct {
		RevisionID  uuid.UUID
		Code        string
		AmountPaise int64
	}
	if err := tx.Table("workforce_staff_tax_declaration_claims").Select("revision_id,code,amount_paise").
		Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Order("revision_id,position").Find(&claims).Error; err != nil {
		return nil, err
	}
	for _, k := range claims {
		r := &rows[index[k.RevisionID]]
		r.Claims = append(r.Claims, taxDeclarationClaimLine{Code: k.Code, AmountPaise: k.AmountPaise})
	}
	var parties []struct {
		RevisionID uuid.UUID
		Role       string
		Position   int
		Name       string
		Address    string
		RentPaise  *int64
		PanMasked  *string
	}
	if err := tx.Table("workforce_staff_tax_declaration_parties").Select("revision_id,role,position,name,address,rent_paise,pan_masked").
		Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Order("revision_id,role,position").Find(&parties).Error; err != nil {
		return nil, err
	}
	for _, d := range parties {
		r := &rows[index[d.RevisionID]]
		r.Parties = append(r.Parties, taxDeclarationParty{Role: d.Role, Position: d.Position, Name: d.Name, Address: d.Address, RentPaise: d.RentPaise, PanMasked: d.PanMasked})
	}
	return rows, nil
}

func loadTaxDeclaration(tx *gorm.DB, tenant string, hospital, staff uuid.UUID, year string, lock bool) (taxDeclaration, error) {
	var parent taxDeclaration
	q := tx.Table("workforce_staff_tax_declarations").Select("staff_id,tax_year,version,created_at,updated_at").
		Where("tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=?", tenant, hospital, staff, year)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&parent).Error; err != nil {
		return parent, err
	}
	revisions, err := loadDeclarationRevisions(tx, tenant, hospital, tx.Table("workforce_staff_tax_declaration_revisions").Select(taxDeclarationRevisionColumns).
		Where("tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=? AND (status='pending' OR id=(SELECT revision_id FROM workforce_staff_tax_declarations WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=?))",
			tenant, hospital, staff, year, tenant, hospital, staff, year))
	if err != nil {
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

// inForceParties returns the in-force revision's parties with their ciphertext, keyed by role and position.
func inForceParties(tx *gorm.DB, tenant string, hospital, staff uuid.UUID, year string) (map[string]declarationParty, error) {
	var rows []declarationParty
	if err := tx.Table("workforce_staff_tax_declaration_parties p").Select("p.role,p.position,p.name,p.address,p.rent_paise,p.pan_ciphertext,p.pan_masked").
		Joins("JOIN workforce_staff_tax_declarations d ON (d.tenant_id,d.hospital_id,d.revision_id)=(p.tenant_id,p.hospital_id,p.revision_id)").
		Where("d.tenant_id=? AND d.hospital_id=? AND d.staff_id=? AND d.tax_year=?", tenant, hospital, staff, year).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]declarationParty, len(rows))
	for _, d := range rows {
		out[d.Role+strconv.Itoa(d.Position)] = d
	}
	return out, nil
}

// resolveDeclaration applies keep against the revision in force and reports whether the proposal differs from it.
func resolveDeclaration(in taxDeclarationInput, parties []declarationParty, current *taxDeclarationRevision, inForce map[string]declarationParty) (bool, error) {
	changed := current == nil
	for i := range parties {
		d := &parties[i]
		was, had := inForce[d.Role+strconv.Itoa(d.Position)]
		switch d.pan.Action {
		case "keep":
			if !had || was.PanCiphertext == nil {
				return false, errDeclarationNothingToKeep
			}
			d.PanCiphertext, d.PanMasked = was.PanCiphertext, was.PanMasked
		case "set":
			changed = true
		case "none":
			changed = changed || (had && was.PanMasked != nil)
		}
		if !had || was.Name != d.Name || was.Address != d.Address || !equalInt64(was.RentPaise, d.RentPaise) {
			changed = true
		}
	}
	if changed || current == nil {
		return true, nil
	}
	if len(parties) != len(inForce) || len(in.Claims) != len(current.Claims) {
		return true, nil
	}
	for i, k := range in.Claims {
		if current.Claims[i].Code != k.Code || current.Claims[i].AmountPaise != *k.AmountPaise {
			return true, nil
		}
	}
	var metro, interest = (*bool)(nil), (*int64)(nil)
	if in.Hra != nil {
		metro = in.Hra.Metro
	}
	if in.HomeLoan != nil {
		interest = in.HomeLoan.InterestPaise
	}
	return !equalBool(metro, current.HraMetro) || !equalInt64(interest, current.HomeLoanInterestPaise), nil
}

func equalInt64(a, b *int64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
func equalBool(a, b *bool) bool   { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func (h *handlers) getTaxDeclaration(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	year, ok := declarationYearParam(c)
	if !ok {
		return
	}
	var result taxDeclaration
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		if result, err = loadTaxDeclaration(tx, p.TenantID, hospital, staff, year, false); err != nil {
			return err
		}
		if result.Current == nil && result.Pending == nil {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		respondDeclarationError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) taxDeclarationHistory(c *gin.Context, params pagination.Params) ([]taxDeclarationRevision, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	staff, ok := staffParam(c)
	if !ok {
		return nil, nil
	}
	year, ok := declarationYearParam(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	var rows []taxDeclarationRevision
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		var err error
		rows, err = loadDeclarationRevisions(tx, p.TenantID, hospital, tenantdb.ApplyKeyset(tx.Table("workforce_staff_tax_declaration_revisions"), params).Select(taxDeclarationRevisionColumns).
			Where("tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=?", p.TenantID, hospital, staff, year))
		return err
	})
	if err != nil {
		respondDeclarationError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) putTaxDeclaration(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	year, ok := declarationYearParam(c)
	if !ok {
		return
	}
	var in taxDeclarationInput
	if !decodeSized(c, &in, 32768) {
		return
	}
	parties, err := normalizeTaxDeclaration(&in)
	if err != nil {
		respondDeclarationError(c, err)
		return
	}
	// Authorise before encrypting, so a caller without the grant never drives the vault.
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, true); err != nil {
			return err
		}
		return requireHospitalStaff(tx, p.TenantID, hospital, staff)
	})
	if err != nil {
		respondDeclarationError(c, err)
		return
	}
	if err := h.sealDeclarationParties(c.Request.Context(), p.TenantID, hospital, staff, year, parties); err != nil {
		respondDeclarationError(c, err)
		return
	}
	var result taxDeclaration
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", taxDeclarationsLock+":"+staff.String()); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		existing, err := loadTaxDeclaration(tx, p.TenantID, hospital, staff, year, true)
		switch {
		case err == nil:
			if existing.Pending != nil {
				return errDeclarationPending
			}
			if existing.Version != *in.ExpectedVersion {
				return errDeclarationVersion
			}
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		case *in.ExpectedVersion != 0:
			return errDeclarationVersion
		default:
			if err := tx.Exec(`INSERT INTO workforce_staff_tax_declarations(tenant_id,hospital_id,staff_id,tax_year,version) VALUES(?,?,?,?,0)`, p.TenantID, hospital, staff, year).Error; err != nil {
				return err
			}
		}
		inForce, err := inForceParties(tx, p.TenantID, hospital, staff, year)
		if err != nil {
			return err
		}
		changed, err := resolveDeclaration(in, parties, existing.Current, inForce)
		if err != nil {
			return err
		}
		if !changed {
			return errDeclarationUnchanged
		}
		var metro *bool
		var interest *int64
		if in.Hra != nil {
			metro = in.Hra.Metro
		}
		if in.HomeLoan != nil {
			interest = in.HomeLoan.InterestPaise
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_staff_tax_declaration_revisions(tenant_id,hospital_id,id,staff_id,tax_year,sequence,base_version,status,reason,hra_metro,home_loan_interest_paise,proposed_by_subject,decision_note)
 SELECT ?,?,?,?,?,coalesce((SELECT max(sequence) FROM workforce_staff_tax_declaration_revisions WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=?),0)+1,?,'pending',?,?,?,?,''`,
			p.TenantID, hospital, revision, staff, year, p.TenantID, hospital, staff, year, *in.ExpectedVersion, in.Reason, metro, interest, p.Subject).Error; err != nil {
			return err
		}
		if err := insertDeclarationLines(tx, p.TenantID, hospital, revision, in.Claims, parties); err != nil {
			return err
		}
		if err := recordDeclarationChange(h, tx, p.TenantID, hospital, p.Subject, "proposed", staff, year, revision, *in.ExpectedVersion); err != nil {
			return err
		}
		if err := tx.Exec(`SET CONSTRAINTS ALL IMMEDIATE`).Error; err != nil {
			return err
		}
		result, err = loadTaxDeclaration(tx, p.TenantID, hospital, staff, year, false)
		return err
	})
	if err != nil {
		respondDeclarationError(c, err)
		return
	}
	respond.OK(c, result)
}

func insertDeclarationLines(tx *gorm.DB, tenant string, hospital, revision uuid.UUID, claims []taxDeclarationClaim, parties []declarationParty) error {
	if len(claims) > 0 {
		values, args := make([]string, len(claims)), make([]any, 0, len(claims)*6)
		for i, k := range claims {
			values[i] = "(?,?,?,?,?,?)"
			args = append(args, tenant, hospital, revision, i+1, k.Code, *k.AmountPaise)
		}
		if err := tx.Exec(`INSERT INTO workforce_staff_tax_declaration_claims(tenant_id,hospital_id,revision_id,position,code,amount_paise) VALUES`+strings.Join(values, ","), args...).Error; err != nil {
			return err
		}
	}
	if len(parties) == 0 {
		return nil
	}
	values, args := make([]string, len(parties)), make([]any, 0, len(parties)*10)
	for i, d := range parties {
		values[i] = "(?,?,?,?,?,?,?,?,?,?)"
		args = append(args, tenant, hospital, revision, d.Role, d.Position, d.Name, d.Address, d.RentPaise, d.PanCiphertext, d.PanMasked)
	}
	return tx.Exec(`INSERT INTO workforce_staff_tax_declaration_parties(tenant_id,hospital_id,revision_id,role,position,name,address,rent_paise,pan_ciphertext,pan_masked) VALUES`+strings.Join(values, ","), args...).Error
}

// Audit rows and events carry identifiers, year, status and version only; amounts, parties and PANs stay behind the API.
func recordDeclarationChange(h *handlers, tx *gorm.DB, tenant string, hospital uuid.UUID, subject, status string, staff uuid.UUID, year string, revision uuid.UUID, version int64) error {
	if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('tax_year',?::text,'revision_id',?::uuid,'version',?::bigint))`,
		tenant, hospital, subject, "workforce.tax_declaration."+status, staff, year, revision, version).Error; err != nil {
		return err
	}
	data, err := json.Marshal(workforcecontract.TaxDeclarationChangedData{HospitalID: hospital.String(), StaffID: staff.String(), TaxYear: year, RevisionID: revision.String(), Status: status, Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, TaxDeclarationChanged, events.Event{Type: TaxDeclarationChanged, Version: 1, TenantID: tenant, Data: data})
}

func (h *handlers) decideTaxDeclaration(c *gin.Context) {
	var in statutoryDecisionInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	to := map[string]string{"approve": "approved", "reject": "rejected"}[in.Decision]
	h.transitionDeclaration(c, in.ProposalID, in.ExpectedVersion, to, in.Note)
}

func (h *handlers) withdrawTaxDeclaration(c *gin.Context) {
	var in statutoryWithdrawInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	h.transitionDeclaration(c, in.ProposalID, nil, "withdrawn", in.Note)
}

// transitionDeclaration decides a pending proposal once; rejecting or withdrawing erases its party PAN ciphertext in the same transaction.
func (h *handlers) transitionDeclaration(c *gin.Context, proposal uuid.UUID, expected *int64, to, note string) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	year, ok := declarationYearParam(c)
	if !ok {
		return
	}
	if proposal == uuid.Nil {
		respond.NotFound(c, "tax declaration proposal")
		return
	}
	note = strings.TrimSpace(note)
	if !validStatutoryText(note) {
		respondDeclarationError(c, errDeclarationInvalid)
		return
	}
	if to == "rejected" && note == "" {
		respondDeclarationError(c, errDeclarationNoteRequired)
		return
	}
	var result taxDeclaration
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", taxDeclarationsLock+":"+staff.String()); err != nil {
			return err
		}
		parent, err := loadTaxDeclaration(tx, p.TenantID, hospital, staff, year, true)
		if err != nil {
			return err
		}
		var r struct {
			Status            string
			ProposedBySubject string
			DecidedBySubject  *string
			DecisionNote      string
		}
		if err := tx.Table("workforce_staff_tax_declaration_revisions").Select("status,proposed_by_subject,decided_by_subject,decision_note").
			Where("tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=? AND id=?", p.TenantID, hospital, staff, year, proposal).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&r).Error; err != nil {
			return err
		}
		if r.Status == to && r.DecidedBySubject != nil && *r.DecidedBySubject == p.Subject && r.DecisionNote == note {
			result = parent
			return nil
		}
		if r.Status != "pending" {
			return errDeclarationState
		}
		if expected != nil && parent.Version != *expected {
			return errDeclarationVersion
		}
		if to == "withdrawn" && r.ProposedBySubject != p.Subject {
			return errDeclarationNotProposer
		}
		if to != "withdrawn" && r.ProposedBySubject == p.Subject {
			return errDeclarationSelf
		}
		set := "status=?,decided_by_subject=?,decided_at=clock_timestamp(),decision_note=?"
		if to == "approved" {
			set += ",approved_version=base_version+1"
		}
		update := tx.Exec(`UPDATE workforce_staff_tax_declaration_revisions SET `+set+` WHERE tenant_id=? AND hospital_id=? AND id=? AND status='pending'`, to, p.Subject, note, p.TenantID, hospital, proposal)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errDeclarationState
		}
		version := parent.Version
		if to == "approved" {
			version++
			if err := tx.Exec(`UPDATE workforce_staff_tax_declarations SET version=?,revision_id=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=? AND version=?`,
				version, proposal, p.TenantID, hospital, staff, year, parent.Version).Error; err != nil {
				return err
			}
		} else if err := tx.Exec(`UPDATE workforce_staff_tax_declaration_parties SET pan_ciphertext=NULL WHERE tenant_id=? AND hospital_id=? AND revision_id=? AND pan_ciphertext IS NOT NULL`, p.TenantID, hospital, proposal).Error; err != nil {
			return err
		}
		if err := recordDeclarationChange(h, tx, p.TenantID, hospital, p.Subject, to, staff, year, proposal, version); err != nil {
			return err
		}
		// Surface the deferred landlord and shape checks here, as a mapped error, rather than at commit.
		if err := tx.Exec(`SET CONSTRAINTS ALL IMMEDIATE`).Error; err != nil {
			return err
		}
		result, err = loadTaxDeclaration(tx, p.TenantID, hospital, staff, year, false)
		return err
	})
	if err != nil {
		respondDeclarationError(c, err)
		return
	}
	respond.OK(c, result)
}

// revealTaxDeclarationParty audits the view and decrypts inside one transaction, so no PAN leaves without its audit row.
func (h *handlers) revealTaxDeclarationParty(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	year, ok := declarationYearParam(c)
	if !ok {
		return
	}
	c.Header("Cache-Control", "no-store")
	var in taxDeclarationRevealInput
	if !decodeSized(c, &in, 1024) {
		return
	}
	in.Reason = collapseSpaces(in.Reason)
	if in.RevisionID == uuid.Nil || in.Reason == "" || len([]rune(in.Reason)) > 200 || !validStatutoryText(in.Reason) {
		respond.Error(c, 400, "invalid_request", "Choose a landlord or lender and give a reason of up to 200 characters for viewing their PAN.")
		return
	}
	if h.transit == nil {
		respondDeclarationError(c, transit.ErrUnavailable)
		return
	}
	key, err := staffKeyName(p.TenantID)
	if err != nil {
		respondDeclarationError(c, err)
		return
	}
	var result taxDeclarationReveal
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", taxDeclarationsLock+":"+staff.String()); err != nil {
			return err
		}
		var r struct {
			Status            string
			ProposedBySubject string
			InForce           bool
		}
		if err := tx.Table("workforce_staff_tax_declaration_revisions r").
			Select("r.status,r.proposed_by_subject,EXISTS(SELECT 1 FROM workforce_staff_tax_declarations d WHERE (d.tenant_id,d.hospital_id,d.staff_id,d.tax_year,d.revision_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.tax_year,r.id)) AS in_force").
			Where("r.tenant_id=? AND r.hospital_id=? AND r.staff_id=? AND r.tax_year=? AND r.id=?", p.TenantID, hospital, staff, year, in.RevisionID).Clauses(clause.Locking{Strength: "SHARE", Table: clause.Table{Name: "r"}}).Take(&r).Error; err != nil {
			return err
		}
		switch {
		case r.Status == "pending" && r.ProposedBySubject == p.Subject:
			return errDeclarationOwnProposal
		case r.Status != "pending" && !r.InForce:
			return errDeclarationNotRevealable
		}
		var party struct{ PanCiphertext *string }
		if err := tx.Table("workforce_staff_tax_declaration_parties").Select("pan_ciphertext").
			Where("tenant_id=? AND hospital_id=? AND revision_id=? AND role=? AND position=?", p.TenantID, hospital, in.RevisionID, in.Role, in.Position).Take(&party).Error; err != nil {
			return err
		}
		if party.PanCiphertext == nil {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('tax_year',?::text,'revision_id',?::uuid,'role',?::text,'position',?::int,'reason',?::text))`,
			p.TenantID, hospital, p.Subject, "workforce.tax_declaration.party_revealed", staff, year, in.RevisionID, in.Role, in.Position, in.Reason).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.TaxDeclarationPartyRevealedData{HospitalID: hospital.String(), StaffID: staff.String(), TaxYear: year, RevisionID: in.RevisionID.String(), Role: in.Role, Position: in.Position})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, TaxDeclarationPartyRevealed, events.Event{Type: TaxDeclarationPartyRevealed, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		plain, err := h.transit.Decrypt(c.Request.Context(), key, declarationContext(hospital, staff, year, in.Role, in.Position), *party.PanCiphertext)
		if err != nil {
			return err
		}
		result = taxDeclarationReveal{RevisionID: in.RevisionID, Role: in.Role, Position: in.Position, Value: string(plain)}
		return nil
	})
	if err != nil {
		respondDeclarationError(c, err)
		return
	}
	respond.OK(c, result)
}

func respondDeclarationError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	isPG := errors.As(err, &pg)
	switch {
	case errors.Is(err, transit.ErrUnavailable):
		requestid.Logger(c).WarnContext(c.Request.Context(), "identifier vault unavailable", "err", err)
		respond.Error(c, http.StatusServiceUnavailable, "identifier_vault_unavailable", "Landlord and lender PANs cannot be changed or viewed right now. Masked values are still shown; try again shortly.")
	case errors.Is(err, transit.ErrIntegrity):
		// A ciphertext that no longer authenticates was altered or moved in the database; it is never shown.
		respond.InternalErr(c, err, "The stored PAN failed its integrity check.")
	case errors.Is(err, errDeclarationInvalid), isPG && pg.ConstraintName == "workforce_tax_declaration_shape":
		respond.Error(c, 400, "invalid_request", "Give a reason, unique claim codes with amounts, 1 to 4 landlords with name, address and rent for HRA, 1 to 4 lenders for home loan interest, and a 10-character PAN only where you set one.")
	case isPG && pg.ConstraintName == "workforce_tax_declaration_landlord_pan_required":
		respond.Error(c, 400, "landlord_pan_required", "Give every landlord's PAN: rent paid crosses the threshold in force, or no threshold is set for this year.")
	case errors.Is(err, errDeclarationNoteRequired):
		respond.Error(c, 400, "invalid_request", "Give a reason when rejecting a proposal.")
	case errors.Is(err, errDeclarationSelf), isPG && pg.ConstraintName == "workforce_tax_declaration_separation":
		respond.Forbidden(c, "Someone other than the person who proposed the declaration must approve or reject it.")
	case errors.Is(err, errDeclarationNotProposer), isPG && pg.ConstraintName == "workforce_tax_declaration_withdrawn_by_proposer":
		respond.Forbidden(c, "Only the person who proposed the declaration can withdraw it.")
	case errors.Is(err, errDeclarationOwnProposal):
		respond.Forbidden(c, "Someone other than the person who proposed these values must check them.")
	case errors.Is(err, errDeclarationVersion):
		respond.Conflict(c, "The declaration changed since you opened it. Reload before saving.")
	case errors.Is(err, errDeclarationPending), isPG && pg.ConstraintName == "workforce_tax_declaration_one_pending":
		respond.ConflictWithDetail(c, "Another declaration proposal for this staff member and year is waiting for a decision.", gin.H{"reason": "proposal_pending"})
	case errors.Is(err, errDeclarationState):
		respond.ConflictWithDetail(c, "This proposal has already been decided.", gin.H{"reason": "not_pending"})
	case errors.Is(err, errDeclarationUnchanged):
		respond.ConflictWithDetail(c, "The proposal would leave the declaration as it is.", gin.H{"reason": "nothing_to_change"})
	case errors.Is(err, errDeclarationNothingToKeep):
		respond.ConflictWithDetail(c, "There is no PAN in force for that landlord or lender to keep.", gin.H{"reason": "nothing_to_keep"})
	case errors.Is(err, errDeclarationNotRevealable):
		respond.ConflictWithDetail(c, "Only the declaration in force or a pending proposal can be viewed.", gin.H{"reason": "not_revealable"})
	case isPG && pg.ConstraintName == "workforce_tax_declaration_staff_inactive":
		respond.ConflictWithDetail(c, "Declarations can only be proposed for an active staff member.", gin.H{"reason": "staff_inactive"})
	default:
		respondError(c, err)
	}
}
