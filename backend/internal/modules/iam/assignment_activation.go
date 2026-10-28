package iam

import (
	"context"
	_ "embed"
	"strconv"

	"gorm.io/gorm"
)

//go:embed assignment_activation.sql
var assignmentActivationSchema string

func (h *assignmentProjection) activate(ctx context.Context, tenant string, snapshot assignmentSnapshot, epoch int64) error {
	return h.db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		if err := assignmentAccess(tx); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.assignment_projected_version',?,true),set_config('app.assignment_activation_epoch',?,true)`, strconv.FormatInt(snapshot.Version, 10), strconv.FormatInt(epoch, 10)).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE iam_assignment_proposals p SET status='active',version=version+1
   WHERE p.tenant_id=? AND p.id=? AND p.version=? AND p.status='approved_pending_activation' AND p.expires_at>clock_timestamp()
   AND EXISTS(SELECT 1 FROM iam_subject_membership_epochs e WHERE e.tenant_id=p.tenant_id AND e.subject=p.subject AND e.epoch=p.membership_epoch AND e.member_count>0)
   AND EXISTS(SELECT 1 FROM iam_assignment_scope_epochs e WHERE e.tenant_id=p.tenant_id AND e.hospital_id=p.hospital_id AND e.subject=p.subject AND e.epoch=?)`, tenant, snapshot.ID, snapshot.Version, epoch).Error
	})
}
