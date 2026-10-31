package workforce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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

const personReviewColumns = "id,revision_id,hospital_id,staff_id,version,status,staff_version,employment_epoch,identity_hash,policy_reference,finding_reference,to_char(observed_on,'YYYY-MM-DD') AS observed_on,valid_until,access_intent,declared_subject,identity_examined,known_associations_disclosed,proposed_by_subject,actor_subject,decision_reason,reviewer_identity_examined,independent_person_confirmed,created_at,updated_at,input_hash"
const personEvidenceColumns = "document_id,record_version,filename,content_type,size,sha256,source_generation,released_generation,scan_attempt,scanned_at,engine_version,definition_version,definitions_updated_at,classification_version,classification_revision_id"

var errPersonReviewConflict = errors.New("person review source or command changed")
var errPersonReviewIndependence = errors.New("person review requires an independent actor")
var errPersonReviewProvenance = errors.New("person review provenance unavailable")

type personReviewRecord struct {
	PersonReview
	InputHash string `json:"-"`
}

func (r PersonReview) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type personReviewHistory struct{ PersonReview }

func (r personReviewHistory) PageKey() (time.Time, uuid.UUID) { return r.UpdatedAt, r.ID }

func personReviewParams(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	p, hospital, staff, ok := identityCaseStaffScope(c)
	if !ok {
		return p, hospital, staff, uuid.Nil, false
	}
	c.Header("Cache-Control", "no-store")
	if c.Param("review") == "" {
		return p, hospital, staff, uuid.Nil, true
	}
	id, err := uuid.Parse(c.Param("review"))
	if err != nil || id.Version() != 7 {
		respond.NotFound(c, "person review")
		return p, hospital, staff, id, false
	}
	return p, hospital, staff, id, true
}

func (h *handlers) personReviewTransaction(c *gin.Context, p authn.Principal, hospital, staff uuid.UUID, permission authz.Permission, fn func(*gorm.DB, IdentitySource, reviewedLeaveActor, authz.ScopeGrant) error) error {
	return h.withIdentityScopeGrant(c, p, hospital, permission, func(ctx context.Context, grant authz.ScopeGrant) error {
		return h.transaction(ctx, p, hospital, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, p.TenantID, hospital).Error; err != nil {
				return err
			}
			actor, err := lockReviewedLeaveActor(tx, p, hospital)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return authz.ErrScopedCommandDenied
				}
				return err
			}
			source, err := loadIdentitySource(tx, p, hospital, staff)
			if err != nil {
				return err
			}
			mode := "manage"
			if permission == PermIdentityChecksReview {
				mode = "review"
			}
			if err := tx.Exec(`SELECT set_config('app.workforce_person_review',?,true)`, mode).Error; err != nil {
				return err
			}
			return fn(tx, source, actor, grant)
		})
	})
}

func personReviewError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			err = errPersonReviewConflict
		case "23514":
			if pg.ConstraintName == "workforce_person_review_source" {
				err = errPersonReviewConflict
			}
		case "55P03", "40P01", "40001":
			respond.Error(c, 503, "person_review_busy", "The person review is temporarily busy. Retry the same command.")
			return
		}
	}
	switch {
	case errors.Is(err, errPersonReviewConflict):
		respond.Error(c, 409, "person_review_conflict", "The person review, source or evidence changed. Reload before a new command.")
	case errors.Is(err, errPersonReviewProvenance):
		respond.Error(c, 409, "person_review_provenance_unavailable", "The required contributor history could not be verified. Resolve the source before submitting a review.")
	case errors.Is(err, errPersonReviewIndependence):
		respond.Forbidden(c, "An independent person reviewer is required.")
	default:
		respondIdentityError(c, err)
	}
}

func personReviewIntent(action string, input any) (string, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(append([]byte(action+":"), raw...))
	return hex.EncodeToString(digest[:]), nil
}

func personReviewText(value *string, maximum int, required bool) bool {
	*value = strings.TrimSpace(*value)
	return utf8.ValidString(*value) && !strings.ContainsRune(*value, '\x00') && utf8.RuneCountInString(*value) <= maximum && (!required || *value != "")
}

func validPersonReviewSubmission(in *PersonReviewSubmissionInput) bool {
	in.ValidUntil = in.ValidUntil.UTC()
	observed, err := time.Parse(time.DateOnly, in.ObservedOn)
	if in.CommandID.Version() != 7 || in.StaffVersion < 1 || in.StaffVersion > 9007199254740990 || in.EmploymentEpoch < 1 || in.EmploymentEpoch > 9007199254740990 || !validIdentityHash(in.IdentityHash) || len(in.IdentityHash) != 64 || !in.IdentityExamined || !in.KnownAssociationsDisclosed || err != nil || observed.Year() < 1 || in.ValidUntil.Year() > 9999 || !in.ValidUntil.After(observed) || !contains(in.AccessIntent, "digital", "non_digital") {
		return false
	}
	if in.DeclaredSubject == nil || !personReviewText(&in.PolicyReference, 300, true) || !personReviewText(&in.FindingReference, 500, true) || !personReviewText(in.DeclaredSubject, 200, in.AccessIntent == "digital") || in.AccessIntent == "non_digital" && *in.DeclaredSubject != "" || strings.ContainsAny(*in.DeclaredSubject, " \t\n\r*#:") || len(in.DocumentIDs) < 1 || len(in.DocumentIDs) > 10 {
		return false
	}
	sort.Slice(in.DocumentIDs, func(i, j int) bool { return in.DocumentIDs[i].String() < in.DocumentIDs[j].String() })
	for i, id := range in.DocumentIDs {
		if id == uuid.Nil || i > 0 && id == in.DocumentIDs[i-1] {
			return false
		}
	}
	return true
}

func personReviewAudit(tx *gorm.DB, p authn.Principal, hospital, id uuid.UUID, action string, version int64, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('version',?::bigint,'assignment_id',?::text,'assignment_version',?::bigint,'scope_epoch',?::bigint,'membership_epoch',?::bigint,'actor_link_id',?::text,'actor_link_version',?::bigint))`, p.TenantID, hospital, p.Subject, action, id, version, grant.AssignmentID, grant.Version, grant.ScopeEpoch, grant.MembershipEpoch, actor.ID, actor.Version).Error
}

func (h *handlers) personReviewChanged(tx *gorm.DB, p authn.Principal, row PersonReview) error {
	data, err := json.Marshal(workforcecontract.PersonReviewChangedData{HospitalID: row.HospitalID.String(), StaffID: row.StaffID.String(), ReviewID: row.ID.String(), RevisionID: row.RevisionID.String(), Version: row.Version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, workforcecontract.SubjectPersonReviewChanged, events.Event{Type: workforcecontract.SubjectPersonReviewChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

func loadPersonReview(tx *gorm.DB, p authn.Principal, hospital, staff, id uuid.UUID, history bool, command uuid.UUID) (personReviewRecord, error) {
	var row personReviewRecord
	table := "workforce_person_reviews"
	if history {
		table = "workforce_person_review_history"
	}
	query := tx.Table(table).Select(personReviewColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staff)
	if command != uuid.Nil {
		query = query.Where("revision_id=?", command)
	} else {
		query = query.Where("id=?", id).Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Take(&row).Error; err != nil {
		return row, err
	}
	row.Evidence = []PersonReviewEvidence{}
	err := tx.Table("workforce_person_review_evidence").Select(personEvidenceColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND review_id=?", p.TenantID, hospital, staff, row.ID).Order("document_id").Find(&row.Evidence).Error
	return row, err
}

func personReviewReceipt(tx *gorm.DB, p authn.Principal, hospital, staff, id, command uuid.UUID, intent string) (personReviewRecord, bool, error) {
	row, err := loadPersonReview(tx, p, hospital, staff, id, true, command)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, false, nil
	}
	if err != nil {
		return row, false, err
	}
	if row.ID != id || row.ActorSubject != p.Subject || row.InputHash != intent {
		return row, false, errPersonReviewConflict
	}
	return row, true, nil
}

func personReviewEvidence(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, ids []uuid.UUID, sourceVersion int64) ([]PersonReviewEvidence, error) {
	bound := make([]string, len(ids))
	for i, id := range ids {
		bound[i] = id.String()
	}
	if err := tx.Exec(`SELECT set_config('app.workforce_person_review_documents',?,true)`, strings.Join(bound, ",")).Error; err != nil {
		return nil, err
	}
	result := make([]PersonReviewEvidence, 0, len(ids))
	for _, id := range ids {
		var file document
		if err := tx.Select(documentColumns).Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&file).Error; err != nil {
			return nil, err
		}
		if file.RecordKind != "enrollment" || file.RecordID != staff || file.Category != "identity_evidence" || file.RecordVersion != sourceVersion || file.Status != "available" || file.SourceGeneration == nil || file.ReleasedGeneration == nil || file.ScannedAt == nil || file.EngineVersion == nil || file.DefinitionVersion == nil || file.DefinitionsUpdatedAt == nil {
			return nil, errPersonReviewConflict
		}
		var classification struct {
			Version  int64
			Revision string
		}
		if err := tx.Table("workforce_document_classifications").Select("version,revision_id::text AS revision").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Scan(&classification).Error; err != nil {
			return nil, err
		}
		result = append(result, PersonReviewEvidence{DocumentID: id, RecordVersion: file.RecordVersion, Filename: file.Filename, ContentType: file.ContentType, Size: file.Size, Sha256: file.SHA256, SourceGeneration: *file.SourceGeneration, ReleasedGeneration: *file.ReleasedGeneration, ScanAttempt: file.ScanAttempt, ScannedAt: *file.ScannedAt, EngineVersion: *file.EngineVersion, DefinitionVersion: *file.DefinitionVersion, DefinitionsUpdatedAt: *file.DefinitionsUpdatedAt, ClassificationVersion: classification.Version, ClassificationRevisionID: classification.Revision})
	}
	return result, nil
}

func personReviewCreationCaptured(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, ids []uuid.UUID) (bool, error) {
	var files int64
	err := tx.Table("workforce_document_contributors").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND document_id IN ? AND creation_captured", p.TenantID, hospital, staff, ids).Distinct("document_id").Count(&files).Error
	return len(ids) > 0 && files == int64(len(ids)), err
}

func personReviewContributors(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, ids []uuid.UUID) ([]string, error) {
	complete, err := personReviewCreationCaptured(tx, p, hospital, staff, ids)
	if err != nil {
		return nil, err
	}
	var source int64
	if err := tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND resource_id=? AND action='workforce_staff.insert'", p.TenantID, hospital, staff).Count(&source).Error; err != nil {
		return nil, err
	}
	if !complete || source == 0 {
		return nil, errPersonReviewProvenance
	}
	var subjects []string
	err = tx.Raw(`SELECT DISTINCT subject FROM (
 SELECT ?::text AS subject UNION ALL
 SELECT actor_subject FROM workforce_audit WHERE tenant_id=? AND hospital_id=? AND resource_id=? AND action IN ('workforce_staff.insert','workforce_staff.update','workforce.enrollment.changed') UNION ALL
 SELECT subject FROM workforce_document_contributors WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND document_id IN ? UNION ALL
 SELECT actor_subject FROM workforce_document_classification_revisions WHERE tenant_id=? AND hospital_id=? AND file_id IN ?
 ) AS contributors ORDER BY subject LIMIT 201`, p.Subject, p.TenantID, hospital, staff, p.TenantID, hospital, staff, ids, p.TenantID, hospital, ids).Scan(&subjects).Error
	if err == nil && len(subjects) > 200 {
		err = errPersonReviewProvenance
	}
	return subjects, err
}

func (h *handlers) putPersonReview(c *gin.Context) {
	p, hospital, staff, id, ok := personReviewParams(c)
	if !ok {
		return
	}
	var in PersonReviewSubmissionInput
	if !decodeSized(c, &in, 8<<10) {
		return
	}
	if !validPersonReviewSubmission(&in) {
		respond.Error(c, 400, "invalid_request", "Complete the current source, policy, human findings, account intent and protected identity evidence.")
		return
	}
	intent, err := personReviewIntent("submit-person-review", in)
	if err != nil {
		personReviewError(c, err)
		return
	}
	var result PersonReview
	err = h.personReviewTransaction(c, p, hospital, staff, PermIdentityChecksManage, func(tx *gorm.DB, source IdentitySource, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		receipt, found, err := personReviewReceipt(tx, p, hospital, staff, id, in.CommandID, intent)
		if err != nil {
			return err
		}
		if found {
			result = receipt.PersonReview
			return personReviewAudit(tx, p, hospital, id, "workforce.person_review.receipt.read", result.Version, actor, grant)
		}
		var now time.Time
		if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
			return err
		}
		if source.StaffVersion != in.StaffVersion || source.EmploymentEpoch != in.EmploymentEpoch || source.IdentityHash != in.IdentityHash || !in.ValidUntil.After(now) || in.ObservedOn > now.UTC().Format(time.DateOnly) {
			return errPersonReviewConflict
		}
		if actor.StaffID == staff || p.Subject == *in.DeclaredSubject {
			return errPersonReviewIndependence
		}
		var self int64
		if err := tx.Table("workforce_login_links").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND subject=?", p.TenantID, hospital, staff, p.Subject).Count(&self).Error; err != nil {
			return err
		}
		if self != 0 {
			return errPersonReviewIndependence
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":"+hospital.String()+":person-review:"+id.String()).Error; err != nil {
			return err
		}
		evidence, err := personReviewEvidence(tx, p, hospital, staff, in.DocumentIDs, in.StaffVersion)
		if err != nil {
			return err
		}
		contributors, err := personReviewContributors(tx, p, hospital, staff, in.DocumentIDs)
		if err != nil {
			return err
		}
		values := map[string]any{"tenant_id": p.TenantID, "hospital_id": hospital, "staff_id": staff, "id": id, "revision_id": in.CommandID, "version": 1, "status": "pending", "staff_version": in.StaffVersion, "employment_epoch": in.EmploymentEpoch, "identity_hash": in.IdentityHash, "policy_reference": in.PolicyReference, "finding_reference": in.FindingReference, "observed_on": in.ObservedOn, "valid_until": in.ValidUntil, "access_intent": in.AccessIntent, "declared_subject": *in.DeclaredSubject, "identity_examined": true, "known_associations_disclosed": true, "proposed_by_subject": p.Subject, "actor_subject": p.Subject, "input_hash": intent}
		if err := tx.Table("workforce_person_reviews").Create(values).Error; err != nil {
			return err
		}
		for _, file := range evidence {
			row := personEvidenceValues(p, hospital, staff, id, file)
			if err := tx.Table("workforce_person_review_evidence").Create(row).Error; err != nil {
				return err
			}
		}
		for _, subject := range contributors {
			if err := tx.Exec(`INSERT INTO workforce_person_review_contributors(tenant_id,hospital_id,staff_id,review_id,subject) VALUES(?,?,?,?,?)`, p.TenantID, hospital, staff, id, subject).Error; err != nil {
				return err
			}
		}
		saved, err := loadPersonReview(tx, p, hospital, staff, id, false, uuid.Nil)
		if err != nil {
			return err
		}
		result = saved.PersonReview
		if err := personReviewAudit(tx, p, hospital, id, "workforce.person_review.submitted", result.Version, actor, grant); err != nil {
			return err
		}
		return h.personReviewChanged(tx, p, result)
	})
	if err != nil {
		personReviewError(c, err)
		return
	}
	respond.OK(c, result)
}

func personEvidenceValues(p authn.Principal, hospital, staff, review uuid.UUID, file PersonReviewEvidence) map[string]any {
	return map[string]any{"tenant_id": p.TenantID, "hospital_id": hospital, "staff_id": staff, "review_id": review, "document_id": file.DocumentID, "record_version": file.RecordVersion, "filename": file.Filename, "content_type": file.ContentType, "size": file.Size, "sha256": file.Sha256, "source_generation": file.SourceGeneration, "released_generation": file.ReleasedGeneration, "scan_attempt": file.ScanAttempt, "scanned_at": file.ScannedAt, "engine_version": file.EngineVersion, "definition_version": file.DefinitionVersion, "definitions_updated_at": file.DefinitionsUpdatedAt, "classification_version": file.ClassificationVersion, "classification_revision_id": file.ClassificationRevisionID}
}

func personReviewState(tx *gorm.DB, p authn.Principal, source IdentitySource, row PersonReview) (string, error) {
	if contains(row.Status, "rejected", "withdrawn", "revoked") {
		return row.Status, nil
	}
	var now time.Time
	if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
		return "", err
	}
	if !row.ValidUntil.After(now) {
		return "expired", nil
	}
	if row.IdentityHash != source.IdentityHash || row.EmploymentEpoch != source.EmploymentEpoch {
		return "source_changed", nil
	}
	ids := make([]uuid.UUID, len(row.Evidence))
	for i, file := range row.Evidence {
		ids[i] = file.DocumentID
	}
	files, err := personReviewEvidence(tx, p, row.HospitalID, row.StaffID, ids, row.StaffVersion)
	if errors.Is(err, errPersonReviewConflict) || errors.Is(err, gorm.ErrRecordNotFound) {
		return "evidence_unavailable", nil
	}
	if err != nil {
		return "", err
	}
	complete, err := personReviewCreationCaptured(tx, p, row.HospitalID, row.StaffID, ids)
	if err != nil {
		return "", err
	}
	if !complete {
		return "evidence_unavailable", nil
	}
	prior, err := json.Marshal(row.Evidence)
	if err != nil {
		return "", err
	}
	current, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	if string(prior) != string(current) || len(files) == 0 {
		return "evidence_unavailable", nil
	}
	return row.Status, nil
}

func (h *handlers) readPersonReview(c *gin.Context, p authn.Principal, hospital, staff uuid.UUID, fn func(*gorm.DB, IdentitySource, reviewedLeaveActor, authz.ScopeGrant) error) error {
	err := h.personReviewTransaction(c, p, hospital, staff, PermIdentityChecksManage, fn)
	if errors.Is(err, authz.ErrScopedCommandDenied) {
		return h.personReviewTransaction(c, p, hospital, staff, PermIdentityChecksReview, fn)
	}
	return err
}

func (h *handlers) getPersonReview(c *gin.Context) {
	p, hospital, staff, id, ok := personReviewParams(c)
	if !ok {
		return
	}
	var result PersonReviewView
	err := h.readPersonReview(c, p, hospital, staff, func(tx *gorm.DB, source IdentitySource, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		row, err := loadPersonReview(tx, p, hospital, staff, id, false, uuid.Nil)
		if err != nil {
			return err
		}
		result.Review = row.PersonReview
		result.CurrentState, err = personReviewState(tx, p, source, row.PersonReview)
		if err != nil {
			return err
		}
		return personReviewAudit(tx, p, hospital, id, "workforce.person_review.read", row.Version, actor, grant)
	})
	if err != nil {
		personReviewError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) decidePersonReview(c *gin.Context) {
	p, hospital, staff, id, ok := personReviewParams(c)
	if !ok {
		return
	}
	var in PersonReviewDecisionInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if in.CommandID.Version() != 7 || in.ExpectedVersion == nil || *in.ExpectedVersion < 1 || *in.ExpectedVersion >= 9007199254740990 || !contains(in.Decision, "approve", "reject") || !personReviewText(&in.Reason, 500, true) || !in.IndependentPersonConfirmed || in.Decision == "approve" && !in.IdentityExamined {
		respond.Error(c, 400, "invalid_request", "Enter the current review version, independent finding and decision reason.")
		return
	}
	intent, err := personReviewIntent("decide-person-review", in)
	if err != nil {
		personReviewError(c, err)
		return
	}
	var result PersonReview
	err = h.personReviewTransaction(c, p, hospital, staff, PermIdentityChecksReview, func(tx *gorm.DB, source IdentitySource, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		row, err := loadPersonReview(tx, p, hospital, staff, id, false, uuid.Nil)
		if err != nil {
			return err
		}
		if actor.StaffID == staff || row.ProposedBySubject == p.Subject || row.DeclaredSubject == p.Subject {
			return errPersonReviewIndependence
		}
		var excluded int64
		if err := tx.Raw(`SELECT (SELECT count(*) FROM workforce_person_review_contributors WHERE tenant_id=? AND hospital_id=? AND review_id=? AND subject=?) + (SELECT count(*) FROM workforce_login_links WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND subject=?)`, p.TenantID, hospital, id, p.Subject, p.TenantID, hospital, staff, p.Subject).Scan(&excluded).Error; err != nil {
			return err
		}
		if excluded > 0 {
			return errPersonReviewIndependence
		}
		receipt, found, err := personReviewReceipt(tx, p, hospital, staff, id, in.CommandID, intent)
		if err != nil {
			return err
		}
		if found {
			result = receipt.PersonReview
			return personReviewAudit(tx, p, hospital, id, "workforce.person_review.receipt.read", result.Version, actor, grant)
		}
		if row.Status != "pending" || row.Version != *in.ExpectedVersion {
			return errPersonReviewConflict
		}
		state, err := personReviewState(tx, p, source, row.PersonReview)
		if err != nil {
			return err
		}
		if in.Decision == "approve" && state != "pending" {
			return errPersonReviewConflict
		}
		status := "rejected"
		if in.Decision == "approve" {
			status = "approved"
		}
		update := tx.Table("workforce_person_reviews").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND version=? AND status='pending'", p.TenantID, hospital, staff, id, *in.ExpectedVersion).Updates(map[string]any{"status": status, "revision_id": in.CommandID, "version": row.Version + 1, "actor_subject": p.Subject, "decision_reason": in.Reason, "reviewer_identity_examined": in.IdentityExamined, "independent_person_confirmed": in.IndependentPersonConfirmed, "input_hash": intent})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errPersonReviewConflict
		}
		saved, err := loadPersonReview(tx, p, hospital, staff, id, false, uuid.Nil)
		if err != nil {
			return err
		}
		result = saved.PersonReview
		if err := personReviewAudit(tx, p, hospital, id, "workforce.person_review.decided", result.Version, actor, grant); err != nil {
			return err
		}
		return h.personReviewChanged(tx, p, result)
	})
	if err != nil {
		personReviewError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) withdrawPersonReview(c *gin.Context) { h.endPersonReview(c, true) }
func (h *handlers) revokePersonReview(c *gin.Context)   { h.endPersonReview(c, false) }

func (h *handlers) endPersonReview(c *gin.Context, withdraw bool) {
	p, hospital, staff, id, ok := personReviewParams(c)
	if !ok {
		return
	}
	var in PersonReviewActionInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if in.CommandID.Version() != 7 || in.ExpectedVersion == nil || *in.ExpectedVersion < 1 || *in.ExpectedVersion >= 9007199254740990 || !personReviewText(&in.Reason, 500, true) {
		respond.Error(c, 400, "invalid_request", "Enter the current review version and a reason.")
		return
	}
	permission, before, after := PermIdentityChecksReview, "approved", "revoked"
	if withdraw {
		permission, before, after = PermIdentityChecksManage, "pending", "withdrawn"
	}
	intent, err := personReviewIntent(after+"-person-review", in)
	if err != nil {
		personReviewError(c, err)
		return
	}
	var result PersonReview
	err = h.personReviewTransaction(c, p, hospital, staff, permission, func(tx *gorm.DB, _ IdentitySource, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		row, err := loadPersonReview(tx, p, hospital, staff, id, false, uuid.Nil)
		if err != nil {
			return err
		}
		if withdraw && row.ProposedBySubject != p.Subject {
			return authz.ErrScopedCommandDenied
		}
		receipt, found, err := personReviewReceipt(tx, p, hospital, staff, id, in.CommandID, intent)
		if err != nil {
			return err
		}
		if found {
			result = receipt.PersonReview
			return personReviewAudit(tx, p, hospital, id, "workforce.person_review.receipt.read", result.Version, actor, grant)
		}
		if row.Status != before || row.Version != *in.ExpectedVersion {
			return errPersonReviewConflict
		}
		updated := tx.Table("workforce_person_reviews").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND version=? AND status=?", p.TenantID, hospital, staff, id, *in.ExpectedVersion, before).Updates(map[string]any{"status": after, "revision_id": in.CommandID, "version": row.Version + 1, "actor_subject": p.Subject, "decision_reason": in.Reason, "input_hash": intent})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errPersonReviewConflict
		}
		saved, err := loadPersonReview(tx, p, hospital, staff, id, false, uuid.Nil)
		if err != nil {
			return err
		}
		result = saved.PersonReview
		if err := personReviewAudit(tx, p, hospital, id, "workforce.person_review."+after, result.Version, actor, grant); err != nil {
			return err
		}
		return h.personReviewChanged(tx, p, result)
	})
	if err != nil {
		personReviewError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) personReviewHistory(c *gin.Context, params pagination.Params) ([]personReviewHistory, error) {
	p, hospital, staff, id, ok := personReviewParams(c)
	if !ok {
		return nil, nil
	}
	rows := []personReviewHistory{}
	err := h.readPersonReview(c, p, hospital, staff, func(tx *gorm.DB, _ IdentitySource, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		head, err := loadPersonReview(tx, p, hospital, staff, id, false, uuid.Nil)
		if err != nil {
			return err
		}
		if err := tenantdb.ApplyOrderedKeyset(tx, params, tenantdb.Keyset{Column: "updated_at", Direction: pagination.NewestFirst}).Table("workforce_person_review_history").Select(personReviewColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			rows[i].Evidence = head.Evidence
		}
		return personReviewAudit(tx, p, hospital, id, "workforce.person_review.history.read", 0, actor, grant)
	})
	if err != nil {
		personReviewError(c, err)
		return nil, nil
	}
	return rows, nil
}
