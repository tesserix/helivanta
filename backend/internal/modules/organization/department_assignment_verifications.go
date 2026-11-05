package organization

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	organizationcontract "github.com/tesserix/helivanta/internal/modules/organization/contract"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func validV7(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }

// assignmentDepartmentConsumer answers from current structure; IAM keeps the first answer per request (ADR-0055 §5).
func assignmentDepartmentConsumer(bus *events.Bus) events.Consumer {
	subject := iamcontract.SubjectAssignmentDepartmentScopeRequested
	return events.Consumer{Name: "organization-assignment-department", Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		tenant, err := uuid.Parse(evt.TenantID)
		if err != nil || tenant == uuid.Nil || tenant.String() != evt.TenantID || evt.Type != subject || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 2048 {
			return errors.New("invalid assignment department envelope")
		}
		var in iamcontract.AssignmentDepartmentScopeRequestedData
		d := json.NewDecoder(bytes.NewReader(evt.Data))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil {
			return errors.New("invalid assignment department request")
		}
		var extra any
		if !errors.Is(d.Decode(&extra), io.EOF) || !validV7(in.ProposalID) || !validV7(in.RequestID) || !validV7(in.DepartmentID) || in.HospitalID == uuid.Nil {
			return errors.New("invalid assignment department identity")
		}
		eligible, err := assignmentDepartmentEligible(tx, evt.TenantID, in.HospitalID, in.DepartmentID)
		if err != nil {
			return err
		}
		action := "assignment_department_refused"
		if eligible {
			action = "assignment_department_confirmed"
		}
		if err := audit(tx, evt.TenantID, "system:iam-assignment-department-scope", action, "organization_departments", in.DepartmentID); err != nil {
			return err
		}
		raw, err := json.Marshal(organizationcontract.AssignmentDepartmentVerifiedData{ProposalID: in.ProposalID, RequestID: in.RequestID, HospitalID: in.HospitalID, DepartmentID: in.DepartmentID, Eligible: eligible})
		if err != nil {
			return err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		return bus.Publish(tx, organizationcontract.SubjectAssignmentDepartmentVerified, events.Event{ID: id.String(), Type: organizationcontract.SubjectAssignmentDepartmentVerified, Version: 1, TenantID: evt.TenantID, Data: raw})
	}}
}

// assignmentDepartmentEligible confirms only a verified hospital whose node-to-root path is active; read failures retry.
func assignmentDepartmentEligible(tx *gorm.DB, tenant string, hospital, department uuid.UUID) (bool, error) {
	err := departmentScopeHospital(tx, tenant, hospital)
	if err == nil {
		_, err = readDepartmentPath(tx, tenant, hospital, department)
	}
	if errors.Is(err, authz.ErrDepartmentReferenceHidden) || errors.Is(err, authz.ErrDepartmentReferenceChanged) {
		return false, nil
	}
	return err == nil, err
}
