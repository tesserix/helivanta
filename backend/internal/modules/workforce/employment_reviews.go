package workforce

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const employmentReviewColumns = "id,revision_id,hospital_id,staff_id,version,status,staff_version,employment_epoch,identity_hash,period_id,period_revision_id,period_version,person_review_id,person_review_revision_id,person_review_version,policy_reference,reason,terms_examined,known_associations_disclosed,access_intent,declared_subject,proposed_by_subject,actor_subject,decision_reason,reviewer_terms_examined,independent_person_confirmed,created_at,updated_at,input_hash"

var errEmploymentReviewConflict = errors.New("employment submission source or command changed")
var errEmploymentReviewPerson = errors.New("current approved person finding unavailable")
var errEmploymentReviewProvenance = errors.New("complete employment contributor provenance unavailable")

type employmentReviewRecord struct {
	EmploymentReview
	IdentityEpoch *int64 `json:"-"`
	InputHash     string `json:"-"`
}
type employmentReviewHistory struct{ EmploymentReview }

func (r employmentReviewHistory) PageKey() (time.Time, uuid.UUID) { return r.UpdatedAt, r.ID }

func employmentReviewProjection(c *gin.Context, mode string) bool {
	query := c.Request.URL.Query()
	if strings.HasSuffix(c.FullPath(), "/employment-reviews/:review/history") {
		for _, key := range []string{"limit", "cursor"} {
			if len(query[key]) > 1 {
				return false
			}
			query.Del(key)
		}
	}
	return len(query) == 1 && len(query[mode]) == 1 && query.Get(mode) == "v1"
}

type employmentReviewBinding struct {
	EmploymentEvidenceBinding
	EngineVersion        string
	DefinitionVersion    int64
	DefinitionsUpdatedAt time.Time
}
type employmentReviewSelection struct {
	BindingID                uuid.UUID
	BindingRevisionID        uuid.UUID
	BindingVersion           int64
	DocumentID               uuid.UUID
	ClassificationVersion    int64
	ClassificationRevisionID string
}

func validEmploymentReviewSubmission(in *EmploymentReviewSubmissionInput) bool {
	for _, id := range []uuid.UUID{in.CommandID, in.PeriodID, in.PeriodRevisionID, in.PersonReviewID, in.PersonReviewRevisionID} {
		if id.Version() != 7 {
			return false
		}
	}
	for _, value := range []*int64{in.StaffVersion, in.EmploymentEpoch, in.PeriodVersion, in.PersonReviewVersion} {
		if value == nil || *value < 1 || *value > 9007199254740990 {
			return false
		}
	}
	if !validIdentityHash(in.IdentityHash) || len(in.IdentityHash) != 64 || !personReviewText(&in.PolicyReference, 300, true) || !personReviewText(&in.Reason, 500, true) || !in.TermsExamined || !in.KnownAssociationsDisclosed || len(in.Evidence) < 1 || len(in.Evidence) > 20 {
		return false
	}
	sort.Slice(in.Evidence, func(i, j int) bool { return in.Evidence[i].ID.String() < in.Evidence[j].ID.String() })
	for i, selected := range in.Evidence {
		if selected.ID.Version() != 7 || selected.RevisionID.Version() != 7 || selected.Version == nil || *selected.Version < 1 || *selected.Version > 9007199254740990 || i > 0 && selected.ID == in.Evidence[i-1].ID {
			return false
		}
	}
	return true
}
func employmentReviewError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			err = errEmploymentReviewConflict
		case "23514":
			if pg.ConstraintName == "workforce_employment_review_source" {
				err = errEmploymentReviewConflict
			}
			if pg.ConstraintName == "workforce_employment_review_independence" {
				err = errPersonReviewIndependence
			}
		case "55P03", "40P01", "40001":
			respond.Error(c, 503, "employment_review_busy", "Employment preparation is temporarily busy. Retry the same command.")
			return
		}
	}
	switch {
	case errors.Is(err, errEmploymentReviewConflict):
		respond.Error(c, 409, "employment_review_conflict", "The employment source, finding, evidence or command changed. Reload before a new command.")
	case errors.Is(err, errEmploymentReviewPerson):
		respond.Error(c, 409, "employment_person_review_unavailable", "An exact current approved person finding is required.")
	case errors.Is(err, errEmploymentReviewProvenance):
		respond.Error(c, 409, "employment_review_provenance_unavailable", "Resolve incomplete contributor history before submitting employment terms.")
	case errors.Is(err, errPersonReviewIndependence):
		respond.Forbidden(c, "Employment cannot be prepared for the actor's own or historically associated identity.")
	default:
		respondIntakeError(c, err)
	}
}
func (h *handlers) employmentReviewPreparation(c *gin.Context, p authn.Principal, hospital, staff, id uuid.UUID, fn func(*gorm.DB, IdentitySource, *intakeAuthority) error) error {
	if !employmentReviewProjection(c, hrIntakeQuery) {
		return errIntakeProjection
	}
	return h.intakeAuthorityTransaction(c, p, hospital, []authz.Permission{PermEnrollmentManage, PermDocumentManage}, true, func(tx *gorm.DB, scoped bool) error {
		value, present := c.Get(intakeAuthorityKey)
		current, ok := value.(*intakeAuthority)
		if !scoped || !present || !ok || current == nil || !current.Scoped {
			return authz.ErrScopedCommandDenied
		}
		source, err := loadIdentitySource(tx, p, hospital, staff)
		if err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_employment_review','manage',true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_employment_evidence','read',true)`, id.String()).Error; err != nil {
			return err
		}
		return fn(tx, source, current)
	})
}
func employmentReviewAudit(tx *gorm.DB, p authn.Principal, row EmploymentReview, action string, authority *intakeAuthority) error {
	if grant, ok := authority.Grants[PermEmploymentReview]; ok {
		return personReviewAudit(tx, p, row.HospitalID, row.ID, action, row.Version, reviewedLeaveActor{ID: authority.Actor.Link, StaffID: authority.Actor.Staff, Version: authority.Actor.LinkVersion}, grant)
	}
	prepare, documents := authority.Grants[PermEnrollmentManage], authority.Grants[PermDocumentManage]
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('version',?::bigint,'staff_id',?::uuid,'period_id',?::uuid,'person_review_id',?::uuid,'actor_link_id',?::uuid,'actor_link_version',?::bigint,'prepare_assignment_id',?::text,'prepare_assignment_version',?::bigint,'documents_assignment_id',?::text,'documents_assignment_version',?::bigint,'scope_epoch',?::bigint,'membership_epoch',?::bigint))`, p.TenantID, row.HospitalID, p.Subject, action, row.ID, row.Version, row.StaffID, row.PeriodID, row.PersonReviewID, authority.Actor.Link, authority.Actor.LinkVersion, prepare.AssignmentID, prepare.Version, documents.AssignmentID, documents.Version, prepare.ScopeEpoch, prepare.MembershipEpoch).Error
}
func employmentReviewPerson(tx *gorm.DB, p authn.Principal, source IdentitySource, review, revision uuid.UUID, version int64) (PersonReview, error) {
	if err := tx.Exec(`SELECT set_config('app.workforce_employment_person_review',?,true)`, review.String()).Error; err != nil {
		return PersonReview{}, err
	}
	found, err := loadPersonReview(tx, p, source.HospitalID, source.StaffID, review, false, uuid.Nil)
	if err != nil {
		return PersonReview{}, err
	}
	if found.RevisionID != revision || found.Version != version || found.Status != "approved" {
		return PersonReview{}, errEmploymentReviewPerson
	}
	state, err := personReviewState(tx, p, source, found.PersonReview)
	if err != nil {
		return PersonReview{}, err
	}
	if state != "approved" {
		return PersonReview{}, errEmploymentReviewPerson
	}
	return found.PersonReview, nil
}
func employmentReviewSponsor(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, terms EmploymentPeriodDraft) error {
	if terms.SponsorID == "" {
		return nil
	}
	var sponsor staffRecordForEmployment
	err := tx.Table("workforce_staff").Select("id").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=? AND employment_status='active'", p.TenantID, hospital, terms.SponsorID).Take(&sponsor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errEmploymentReviewConflict
	}
	return err
}
func employmentReviewFile(tx *gorm.DB, p authn.Principal, source IdentitySource, terms EmploymentPeriodDraft, selected EmploymentReviewEvidenceSelection) (employmentReviewBinding, EmploymentReviewEvidence, error) {
	var binding employmentReviewBinding
	err := tx.Table("workforce_employment_evidence_bindings").Select(employmentEvidenceColumns+",engine_version,definition_version,definitions_updated_at").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, source.HospitalID, source.StaffID, selected.ID).Take(&binding).Error
	if err != nil {
		return binding, EmploymentReviewEvidence{}, err
	}
	if binding.Status != "attached" || binding.Version != *selected.Version || binding.RevisionID != selected.RevisionID || binding.PeriodID != terms.ID || binding.PeriodVersion != terms.Version || binding.PeriodRevisionID != terms.RevisionID || binding.StaffVersion != source.StaffVersion || binding.EmploymentEpoch != source.EmploymentEpoch {
		return binding, EmploymentReviewEvidence{}, errEmploymentReviewConflict
	}
	var file document
	if err := tx.Select(documentColumns).Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, source.HospitalID, source.StaffID, binding.DocumentID).Take(&file).Error; err != nil {
		return binding, EmploymentReviewEvidence{}, err
	}
	if file.RecordKind != "enrollment" || file.RecordID != source.StaffID || file.Category != "employment_evidence" || file.Status != "available" || file.RecordVersion != binding.DocumentRecordVersion || file.Filename != binding.Filename || file.ContentType != binding.ContentType || file.Size != binding.Size || file.SHA256 != binding.Sha256 || file.SourceGeneration == nil || file.ReleasedGeneration == nil || file.ScannedAt == nil || file.EngineVersion == nil || file.DefinitionVersion == nil || file.DefinitionsUpdatedAt == nil || strconv.FormatInt(*file.SourceGeneration, 10) != binding.SourceGeneration || strconv.FormatInt(*file.ReleasedGeneration, 10) != binding.ReleasedGeneration || file.ScanAttempt != binding.ScanAttempt || !file.ScannedAt.Equal(binding.ScannedAt) || *file.EngineVersion != binding.EngineVersion || *file.DefinitionVersion != binding.DefinitionVersion || !file.DefinitionsUpdatedAt.Equal(binding.DefinitionsUpdatedAt) {
		return binding, EmploymentReviewEvidence{}, errEmploymentReviewConflict
	}
	var classification struct {
		Version  int64
		Revision string
	}
	if err := tx.Table("workforce_document_classifications").Select("version,revision_id::text AS revision").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, source.HospitalID, binding.DocumentID).Scan(&classification).Error; err != nil {
		return binding, EmploymentReviewEvidence{}, err
	}
	return binding, EmploymentReviewEvidence{Binding: employmentEvidenceUTC(binding.EmploymentEvidenceBinding), ClassificationVersion: classification.Version, ClassificationRevisionID: classification.Revision}, nil
}
func employmentReviewContributors(tx *gorm.DB, p authn.Principal, hospital, staff, id uuid.UUID) ([]string, error) {
	var subjects []string
	err := tx.Raw(`SELECT subject FROM workforce_employment_review_required_contributors(?::uuid,?::uuid,?::uuid,?::uuid) ORDER BY subject LIMIT 201`, p.TenantID, hospital, staff, id).Scan(&subjects).Error
	if err == nil && (len(subjects) < 1 || len(subjects) > 200) {
		err = errEmploymentReviewProvenance
	}
	return subjects, err
}
func loadEmploymentReview(tx *gorm.DB, p authn.Principal, hospital, staff, id, command uuid.UUID) (employmentReviewRecord, error) {
	var row employmentReviewRecord
	table := "workforce_employment_reviews"
	if command != uuid.Nil {
		table = "workforce_employment_review_history"
	}
	query := tx.Table(table).Select(employmentReviewColumns+",(to_jsonb("+table+")->>'identity_epoch')::bigint AS identity_epoch").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id)
	if command != uuid.Nil {
		query = query.Where("revision_id=?", command)
	} else {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Take(&row).Error
	if err != nil {
		return row, err
	}
	err = tx.Table("workforce_employment_period_history").Select(employmentPeriodColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND version=? AND revision_id=?", p.TenantID, hospital, staff, row.PeriodID, row.PeriodVersion, row.PeriodRevisionID).Take(&row.Terms).Error
	if err != nil {
		return row, err
	}
	var selections []employmentReviewSelection
	if err := tx.Table("workforce_employment_review_evidence").Select("binding_id,binding_revision_id,binding_version,document_id,classification_version,COALESCE(classification_revision_id::text,'') AS classification_revision_id").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND review_id=?", p.TenantID, hospital, staff, id).Order("binding_id").Find(&selections).Error; err != nil {
		return row, err
	}
	row.Evidence = []EmploymentReviewEvidence{}
	for _, selection := range selections {
		var binding EmploymentEvidenceBinding
		if err := tx.Table("workforce_employment_evidence_history").Select(employmentEvidenceColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND revision_id=? AND version=? AND document_id=?", p.TenantID, hospital, staff, selection.BindingID, selection.BindingRevisionID, selection.BindingVersion, selection.DocumentID).Take(&binding).Error; err != nil {
			return row, err
		}
		row.Evidence = append(row.Evidence, EmploymentReviewEvidence{Binding: employmentEvidenceUTC(binding), ClassificationVersion: selection.ClassificationVersion, ClassificationRevisionID: selection.ClassificationRevisionID})
	}
	return row, nil
}
func (h *handlers) putEmploymentReview(c *gin.Context) {
	p, hospital, staff, id, ok := personReviewParams(c)
	if !ok {
		return
	}
	var in EmploymentReviewSubmissionInput
	if !decodeSized(c, &in, 8<<10) {
		return
	}
	if !validEmploymentReviewSubmission(&in) {
		respond.Error(c, 400, "invalid_request", "Choose current terms, an approved person finding, released evidence and required human attestations.")
		return
	}
	intent, err := personReviewIntent("submit-employment-review", in)
	if err != nil {
		employmentReviewError(c, err)
		return
	}
	var result EmploymentReview
	err = h.employmentReviewPreparation(c, p, hospital, staff, id, func(tx *gorm.DB, source IdentitySource, authority *intakeAuthority) error {
		var receipt employmentReviewRecord
		found := tx.Table("workforce_employment_review_history").Select(employmentReviewColumns).Where("tenant_id=? AND hospital_id=? AND revision_id=?", p.TenantID, hospital, in.CommandID).Take(&receipt).Error
		if found == nil {
			if receipt.ID != id || receipt.StaffID != staff || receipt.ActorSubject != p.Subject || receipt.InputHash != intent {
				return errEmploymentReviewConflict
			}
			loaded, err := loadEmploymentReview(tx, p, hospital, staff, id, in.CommandID)
			if err != nil {
				return err
			}
			result = loaded.EmploymentReview
			return employmentReviewAudit(tx, p, result, "workforce.employment_review.receipt.read", authority)
		}
		if !errors.Is(found, gorm.ErrRecordNotFound) {
			return found
		}
		if source.StaffVersion != *in.StaffVersion || source.EmploymentEpoch != *in.EmploymentEpoch || source.IdentityHash != in.IdentityHash {
			return errEmploymentReviewConflict
		}
		finding, err := employmentReviewPerson(tx, p, source, in.PersonReviewID, in.PersonReviewRevisionID, *in.PersonReviewVersion)
		if err != nil {
			return err
		}
		if authority.Actor.Staff == staff || p.Subject == finding.DeclaredSubject {
			return errPersonReviewIndependence
		}
		var associated int64
		if err := tx.Table("workforce_login_links").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND subject=?", p.TenantID, hospital, staff, p.Subject).Count(&associated).Error; err != nil {
			return err
		}
		if associated != 0 {
			return errPersonReviewIndependence
		}
		var terms EmploymentPeriodDraft
		if err := tx.Table("workforce_employment_period_drafts").Select(employmentPeriodColumns).Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, in.PeriodID).Take(&terms).Error; err != nil {
			return err
		}
		if terms.Status != "draft" || terms.RevisionID != in.PeriodRevisionID || terms.Version != *in.PeriodVersion || terms.StaffVersion != source.StaffVersion || terms.EmploymentEpoch != source.EmploymentEpoch {
			return errEmploymentReviewConflict
		}
		if err := employmentReviewSponsor(tx, p, hospital, terms); err != nil {
			return err
		}
		evidence := make([]EmploymentReviewEvidence, 0, len(in.Evidence))
		files := make([]uuid.UUID, 0, len(in.Evidence))
		for _, selected := range in.Evidence {
			binding, file, err := employmentReviewFile(tx, p, source, terms, selected)
			if err != nil {
				return err
			}
			evidence = append(evidence, file)
			files = append(files, binding.DocumentID)
		}
		complete, err := personReviewCreationCaptured(tx, p, hospital, staff, files)
		if err != nil {
			return err
		}
		if !complete {
			return errEmploymentReviewProvenance
		}
		values := map[string]any{"tenant_id": p.TenantID, "hospital_id": hospital, "staff_id": staff, "id": id, "revision_id": in.CommandID, "version": 1, "status": "pending", "staff_version": source.StaffVersion, "employment_epoch": source.EmploymentEpoch, "identity_hash": source.IdentityHash, "period_id": terms.ID, "period_revision_id": terms.RevisionID, "period_version": terms.Version, "person_review_id": finding.ID, "person_review_revision_id": finding.RevisionID, "person_review_version": finding.Version, "policy_reference": in.PolicyReference, "reason": in.Reason, "terms_examined": true, "known_associations_disclosed": true, "access_intent": finding.AccessIntent, "declared_subject": finding.DeclaredSubject, "proposed_by_subject": p.Subject, "actor_subject": p.Subject, "input_hash": intent}
		if err := tx.Table("workforce_employment_reviews").Create(values).Error; err != nil {
			return err
		}
		for _, file := range evidence {
			var revision any
			if file.ClassificationRevisionID != "" {
				revision = file.ClassificationRevisionID
			}
			if err := tx.Table("workforce_employment_review_evidence").Create(map[string]any{"tenant_id": p.TenantID, "hospital_id": hospital, "staff_id": staff, "review_id": id, "binding_id": file.Binding.ID, "binding_revision_id": file.Binding.RevisionID, "binding_version": file.Binding.Version, "document_id": file.Binding.DocumentID, "classification_version": file.ClassificationVersion, "classification_revision_id": revision}).Error; err != nil {
				return err
			}
		}
		contributors, err := employmentReviewContributors(tx, p, hospital, staff, id)
		if err != nil {
			return err
		}
		for _, subject := range contributors {
			if err := tx.Exec(`INSERT INTO workforce_employment_review_contributors(tenant_id,hospital_id,staff_id,review_id,subject) VALUES(?,?,?,?,?)`, p.TenantID, hospital, staff, id, subject).Error; err != nil {
				return err
			}
		}
		loaded, err := loadEmploymentReview(tx, p, hospital, staff, id, uuid.Nil)
		if err != nil {
			return err
		}
		result = loaded.EmploymentReview
		if err := employmentReviewAudit(tx, p, result, "workforce.employment_review.submitted", authority); err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.EmploymentReviewChangedData{HospitalID: hospital.String(), StaffID: staff.String(), ReviewID: id.String(), RevisionID: result.RevisionID.String(), Version: result.Version})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, workforcecontract.SubjectEmploymentReviewChanged, events.Event{Type: workforcecontract.SubjectEmploymentReviewChanged, Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		employmentReviewError(c, err)
		return
	}
	respond.OK(c, result)
}
func employmentReviewState(tx *gorm.DB, p authn.Principal, source IdentitySource, row EmploymentReview) (string, error) {
	if contains(row.Status, "rejected", "withdrawn", "revoked") {
		return row.Status, nil
	}
	if source.StaffVersion != row.StaffVersion || source.EmploymentEpoch != row.EmploymentEpoch || source.IdentityHash != row.IdentityHash {
		return "source_changed", nil
	}
	if row.Status == "pending" {
		var head EmploymentPeriodDraft
		if err := tx.Table("workforce_employment_period_drafts").Select("revision_id,version,status").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, row.HospitalID, row.StaffID, row.PeriodID).Take(&head).Error; err != nil {
			return "", err
		}
		if head.Version != row.PeriodVersion || head.RevisionID != row.PeriodRevisionID || head.Status != "draft" {
			return "source_changed", nil
		}
	}
	if err := employmentReviewSponsor(tx, p, row.HospitalID, row.Terms); err != nil {
		if errors.Is(err, errEmploymentReviewConflict) {
			return "source_changed", nil
		}
		return "", err
	}
	if _, err := employmentReviewPerson(tx, p, source, row.PersonReviewID, row.PersonReviewRevisionID, row.PersonReviewVersion); err != nil {
		if errors.Is(err, errEmploymentReviewPerson) || errors.Is(err, gorm.ErrRecordNotFound) {
			return "person_unavailable", nil
		}
		return "", err
	}
	files := make([]uuid.UUID, 0, len(row.Evidence))
	for _, prior := range row.Evidence {
		version := prior.Binding.Version
		_, current, err := employmentReviewFile(tx, p, source, row.Terms, EmploymentReviewEvidenceSelection{ID: prior.Binding.ID, RevisionID: prior.Binding.RevisionID, Version: &version})
		if errors.Is(err, errEmploymentReviewConflict) || errors.Is(err, gorm.ErrRecordNotFound) {
			return "evidence_unavailable", nil
		}
		if err != nil {
			return "", err
		}
		if current.ClassificationVersion != prior.ClassificationVersion || current.ClassificationRevisionID != prior.ClassificationRevisionID {
			return "evidence_unavailable", nil
		}
		files = append(files, prior.Binding.DocumentID)
	}
	complete, err := personReviewCreationCaptured(tx, p, row.HospitalID, row.StaffID, files)
	if err != nil {
		return "", err
	}
	if !complete {
		return "evidence_unavailable", nil
	}
	return row.Status, nil
}
func (h *handlers) getEmploymentReview(c *gin.Context) {
	p, hospital, staff, id, ok := personReviewParams(c)
	if !ok {
		return
	}
	var result EmploymentReviewView
	err := h.employmentReviewRead(c, p, hospital, staff, id, func(tx *gorm.DB, source IdentitySource, authority *intakeAuthority) error {
		row, err := loadEmploymentReview(tx, p, hospital, staff, id, uuid.Nil)
		if err != nil {
			return err
		}
		result.Review = row.EmploymentReview
		result.CurrentState, err = employmentReviewState(tx, p, source, row.EmploymentReview)
		if err != nil {
			return err
		}
		_, reviewer := authority.Grants[PermEmploymentReview]
		if reviewer {
			independent, err := employmentReviewIndependent(tx, p, row.EmploymentReview, authority.Actor)
			if err != nil {
				return err
			}
			result.CanApprove = independent && row.Status == "pending" && result.CurrentState == "pending"
			result.CanReject = independent && row.Status == "pending"
			result.CanRevoke = independent && row.Status == "approved"
		} else {
			result.CanWithdraw = row.Status == "pending" && row.ProposedBySubject == p.Subject
		}
		return employmentReviewAudit(tx, p, row.EmploymentReview, "workforce.employment_review.read", authority)
	})
	if err != nil {
		employmentReviewError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) listEmploymentReviewHistory(c *gin.Context, params pagination.Params) ([]employmentReviewHistory, error) {
	p, hospital, staff, id, ok := personReviewParams(c)
	if !ok {
		return nil, nil
	}
	rows := []employmentReviewHistory{}
	err := h.employmentReviewRead(c, p, hospital, staff, id, func(tx *gorm.DB, _ IdentitySource, authority *intakeAuthority) error {
		head, err := loadEmploymentReview(tx, p, hospital, staff, id, uuid.Nil)
		if err != nil {
			return err
		}
		query := tenantdb.ApplyOrderedKeyset(tx, params, tenantdb.Keyset{Column: "updated_at", Direction: pagination.NewestFirst})
		if err := query.Table("workforce_employment_review_history").Select(employmentReviewColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			rows[i].Terms = head.Terms
			rows[i].Evidence = head.Evidence
		}
		return employmentReviewAudit(tx, p, head.EmploymentReview, "workforce.employment_review.history.read", authority)
	})
	if err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	return rows, nil
}
