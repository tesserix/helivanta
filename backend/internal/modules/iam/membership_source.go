package iam

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

//go:embed membership_projection_reader.sql
var membershipProjectionReaderSchema string

func currentMemberships(ctx context.Context, db *tenantdb.DB, tenant, subject string) ([]authz.Membership, error) {
	id, err := uuid.Parse(tenant)
	if err != nil || id == uuid.Nil || id.String() != tenant || !proposalSubject(subject) {
		return nil, errors.New("current membership scope invalid")
	}
	var rows []struct {
		ID          uuid.UUID
		RoleKey     authz.Role
		Epoch       int64
		MemberCount int64
	}
	bound := len(authz.SystemRoles())
	err = db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		return tx.Table("iam_members AS m").Select("m.id,m.role_key,e.epoch,e.member_count").Joins("LEFT JOIN iam_subject_membership_epochs e ON e.tenant_id=m.tenant_id AND e.subject=m.subject").Where("m.tenant_id=? AND m.subject=?", tenant, subject).Order("m.role_key").Limit(bound + 1).Find(&rows).Error
	})
	if err != nil {
		return nil, fmt.Errorf("read current IAM memberships: %w", err)
	}
	if len(rows) > bound {
		return nil, errors.New("current membership role bound exceeded")
	}
	result := make([]authz.Membership, 0, len(rows))
	for _, row := range rows {
		m := authz.Membership{ID: row.ID.String(), TenantID: tenant, Subject: subject, Role: row.RoleKey, Epoch: row.Epoch}
		if _, err := m.Object(); err != nil || row.MemberCount != int64(len(rows)) {
			return nil, errors.New("current membership binding invalid")
		}
		result = append(result, m)
	}
	return result, nil
}
