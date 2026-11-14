package workforce

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type assignmentTargetReader struct{ db *tenantdb.DB }

type assignmentTargetScanRow struct {
	StaffID   uuid.UUID
	CreatedAt time.Time
}

//go:embed assignment_targets.sql
var assignmentTargetsSchema string

// AssignmentTargets supplies canonical qualification without impersonating the target account.
func (*Module) AssignmentTargets(db *tenantdb.DB) authz.GovernedAssignmentTargetReader {
	return &assignmentTargetReader{db: db}
}

func (reader *assignmentTargetReader) withRead(ctx context.Context, request authz.GovernedAssignmentSelectionRequest, action func(*gorm.DB, []byte) error) error {
	if reader.db == nil || !reader.db.GuardedCommandActive(ctx) || tenantdb.ActorFrom(ctx) != request.Principal.Subject {
		return authz.ErrScopedAuthorizationUnavailable
	}
	now := time.Now()
	deadline, bounded := ctx.Deadline()
	if !bounded || !deadline.After(now) || deadline.After(now.Add(5*time.Second)) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	proof, err := authz.AssignmentTargetChoiceProof(request, now)
	if err != nil {
		return err
	}
	deadline = minDeadline(deadline, minDeadline(request.Source.ExpiresAt, request.Principal.IdleDeadline))
	proof["read_expires_at"] = deadline
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	if err := reader.db.ConstrainGuardedCommand(ctx, request.Principal.TenantID, deadline); err != nil {
		return err
	}
	return reader.db.WithTenantCommand(ctx, request.Principal.TenantID, func(tx *gorm.DB) (err error) {
		var supported bool
		if err := tx.Raw(`SELECT CASE WHEN to_regprocedure('public.workforce_assignment_selection_support()')IS NULL THEN false ELSE public.workforce_assignment_selection_support()END`).Scan(&supported).Error; err != nil {
			return err
		}
		if !supported {
			return authz.ErrScopedAuthorizationUnavailable
		}
		var previous string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.workforce_hospital','app.workforce_subject','app.workforce_assignment_selection','app.workforce_assignment_target_staff','app.workforce_assignment_target_generation'])AS key`).Scan(&previous).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
		}()
		if err = tx.Exec(`SELECT set_config('app.workforce_hospital',?,true),set_config('app.workforce_subject',?,true),set_config('app.workforce_assignment_selection',?,true),set_config('app.workforce_assignment_target_staff','',true),set_config('app.workforce_assignment_target_generation','',true)`, request.HospitalID.String(), request.Principal.Subject, string(raw)).Error; err != nil {
			return err
		}
		var hospital struct{ ID uuid.UUID }
		if err = tx.Table("workforce_hospitals").Select("id").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND id=? AND establishment_kind='hospital'", request.Principal.TenantID, request.HospitalID).Take(&hospital).Error; err != nil {
			return err
		}
		return action(tx, raw)
	})
}

func (reader *assignmentTargetReader) ReadAssignmentTargetCandidates(ctx context.Context, request authz.GovernedAssignmentSelectionRequest, page pagination.Params) ([]authz.AssignmentTargetCandidate, error) {
	rows := []authz.AssignmentTargetCandidate{}
	err := reader.withRead(ctx, request, func(tx *gorm.DB, proof []byte) error {
		if page.Limit < 1 || page.Limit > 20 || page.After != nil && (page.After.ID == uuid.Nil || page.After.CreatedAt.IsZero()) {
			return authz.ErrScopedAuthorizationUnavailable
		}
		query := tx.Table("workforce_staff").Select("id AS staff_id,created_at").Where("tenant_id=? AND hospital_id=?", request.Principal.TenantID, request.HospitalID)
		if page.After != nil {
			query = query.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		var scanned []assignmentTargetScanRow
		if err := query.Order("created_at DESC,id DESC").Limit(page.Limit + 1).Find(&scanned).Error; err != nil {
			return err
		}
		for _, row := range scanned {
			reference, err := reader.qualify(tx, request, row.StaffID, proof)
			if err != nil {
				return err
			}
			rows = append(rows, authz.AssignmentTargetCandidate{StaffID: row.StaffID, CreatedAt: row.CreatedAt, Reference: reference})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (reader *assignmentTargetReader) RecheckAssignmentTargets(ctx context.Context, request authz.GovernedAssignmentSelectionRequest, references []authz.AssignmentTargetReference) error {
	return reader.withRead(ctx, request, func(tx *gorm.DB, proof []byte) error {
		if len(references) > 21 {
			return authz.ErrScopedAuthorizationUnavailable
		}
		seenStaff, seenSubjects := map[uuid.UUID]bool{}, map[string]bool{}
		for _, before := range references {
			if before.StaffID == uuid.Nil || seenStaff[before.StaffID] || seenSubjects[before.Subject] {
				return authz.ErrAssignmentTargetChanged
			}
			seenStaff[before.StaffID], seenSubjects[before.Subject] = true, true
			after, err := reader.qualify(tx, request, before.StaffID, proof)
			if err != nil {
				return err
			}
			if after == nil || !sameAssignmentTargetReference(before, *after) {
				return authz.ErrAssignmentTargetChanged
			}
		}
		return nil
	})
}

func sameAssignmentTargetReference(before, after authz.AssignmentTargetReference) bool {
	return before.StaffID == after.StaffID && before.StaffVersion == after.StaffVersion && before.LinkID == after.LinkID && before.LinkVersion == after.LinkVersion && before.GenerationID == after.GenerationID && before.EmploymentEpoch == after.EmploymentEpoch && before.Subject == after.Subject && before.EligibleUntil.Equal(after.EligibleUntil)
}

func (reader *assignmentTargetReader) qualify(tx *gorm.DB, request authz.GovernedAssignmentSelectionRequest, staff uuid.UUID, proof []byte) (*authz.AssignmentTargetReference, error) {
	if staff == uuid.Nil {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	if err := tx.Exec(`SELECT set_config('app.workforce_assignment_target_staff',?,true),set_config('app.workforce_assignment_target_generation','',true)`, staff.String()).Error; err != nil {
		return nil, err
	}
	var source struct {
		Version          int64
		EmploymentStatus string
	}
	p := request.Principal
	if err := tx.Table("workforce_staff").Select("version,employment_status").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, request.HospitalID, staff).Take(&source).Error; err != nil {
		return nil, err
	}
	var result *authz.AssignmentTargetReference
	if source.EmploymentStatus == "active" {
		var links []struct {
			ID, GenerationID uuid.UUID
			Subject          string
			Version          int64
		}
		if err := tx.Table("workforce_login_links").Select("id,subject,version,employment_generation_id AS generation_id").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND status='active' AND employment_generation_id IS NOT NULL", p.TenantID, request.HospitalID, staff).Limit(2).Find(&links).Error; err != nil {
			return nil, err
		}
		if len(links) > 1 {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		if len(links) == 1 {
			link := links[0]
			if !validLoginSubject(link.Subject) || link.Version < 2 || link.GenerationID == uuid.Nil {
				return nil, authz.ErrScopedAuthorizationUnavailable
			}
			if err := tx.Exec(`SELECT set_config('app.workforce_assignment_target_generation',?,true)`, link.GenerationID.String()).Error; err != nil {
				return nil, err
			}
			generation, state, until, err := readProtectedStaffGenerationState(tx, p, request.HospitalID, staff, staff, link.ID, link.GenerationID)
			if err != nil {
				return nil, err
			}
			if state == "effective" && generation.AccessIntent == "digital" && generation.DeclaredSubject == link.Subject && until.After(time.Now()) {
				result = &authz.AssignmentTargetReference{StaffID: staff, StaffVersion: source.Version, LinkID: link.ID, LinkVersion: link.Version, GenerationID: link.GenerationID, EmploymentEpoch: generation.EmploymentEpoch, Subject: link.Subject, EligibleUntil: until}
			}
		}
	}
	var linkID, generationID any
	if result != nil {
		linkID, generationID = result.LinkID.String(), result.GenerationID.String()
	}
	err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)VALUES(?,?,?,'workforce.assignment_target.qualification.read',?,jsonb_build_object('authority',?::jsonb,'staff_version',?::bigint,'eligible',?::boolean,'link_id',?::uuid,'generation_id',?::uuid))`, p.TenantID, request.HospitalID, p.Subject, staff, string(proof), source.Version, result != nil, linkID, generationID).Error
	return result, err
}
