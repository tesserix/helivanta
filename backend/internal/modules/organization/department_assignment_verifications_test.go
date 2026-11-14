package organization_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	organizationcontract "github.com/tesserix/helivanta/internal/modules/organization/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func TestAssignmentDepartmentVerifierAnswersFromCurrentStructure(t *testing.T) {
	r, db, bus, ctx, hospital := departmentHarnessWithBus(t, organization.New())
	s := &scopeTree{r: r, db: db, ctx: ctx, hospital: hospital, versions: map[uuid.UUID]int{}, codes: map[uuid.UUID]string{}}
	root := s.node(t, "SYN-VERIFY-ROOT", uuid.Nil)
	child := s.node(t, "SYN-VERIFY-CHILD", root)
	retired := s.node(t, "SYN-VERIFY-RETIRED", root)
	s.put(t, retired, "", root, "retired")
	var consumer events.Consumer
	for _, c := range organization.New().Consumers(platform.Deps{Bus: bus}) {
		if c.Name == "organization-assignment-department" {
			consumer = c
		}
	}
	require.Equal(t, iamcontract.SubjectAssignmentDepartmentScopeRequested, consumer.Subject, "the verifier is registered for IAM requests")
	ask := func(t *testing.T, tenant string, request iamcontract.AssignmentDepartmentScopeRequestedData) (bool, error) {
		t.Helper()
		data, err := json.Marshal(request)
		require.NoError(t, err)
		evt := events.Event{Type: consumer.Subject, Version: 1, TenantID: tenant, Data: data}
		if err := db.WithTenant(ctx, tenant, func(tx *gorm.DB) error { return consumer.Handle(ctx, tx, evt) }); err != nil {
			return false, err
		}
		var replies []struct{ Payload []byte }
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT payload FROM outbox_events WHERE subject=? AND payload->'data'->>'request_id'=?`, organizationcontract.SubjectAssignmentDepartmentVerified, request.RequestID.String()).Scan(&replies).Error
		}))
		require.Len(t, replies, 1)
		var reply events.Event
		require.NoError(t, json.Unmarshal(replies[0].Payload, &reply))
		require.Equal(t, organizationcontract.SubjectAssignmentDepartmentVerified, reply.Type)
		var answer organizationcontract.AssignmentDepartmentVerifiedData
		require.NoError(t, json.Unmarshal(reply.Data, &answer))
		require.Equal(t, request.ProposalID, answer.ProposalID)
		require.Equal(t, request.HospitalID, answer.HospitalID)
		require.Equal(t, request.DepartmentID, answer.DepartmentID)
		return answer.Eligible, nil
	}
	request := func(hospital, department uuid.UUID) iamcontract.AssignmentDepartmentScopeRequestedData {
		return iamcontract.AssignmentDepartmentScopeRequestedData{ProposalID: uuid.Must(uuid.NewV7()), RequestID: uuid.Must(uuid.NewV7()), HospitalID: hospital, DepartmentID: department}
	}

	for name, tc := range map[string]struct {
		tenant     string
		hospital   uuid.UUID
		department uuid.UUID
		eligible   bool
	}{
		"active root":           {testutil.TenantA, s.hospitalID(), root, true},
		"active sub-unit":       {testutil.TenantA, s.hospitalID(), child, true},
		"retired department":    {testutil.TenantA, s.hospitalID(), retired, false},
		"unknown department":    {testutil.TenantA, s.hospitalID(), uuid.Must(uuid.NewV7()), false},
		"unregistered hospital": {testutil.TenantA, uuid.New(), root, false},
		"foreign tenant":        {testutil.TenantB, s.hospitalID(), root, false},
	} {
		t.Run(name, func(t *testing.T) {
			eligible, err := ask(t, tc.tenant, request(tc.hospital, tc.department))
			require.NoError(t, err)
			require.Equal(t, tc.eligible, eligible)
		})
	}
	t.Run("every answer is audited under the requesting owner", func(t *testing.T) {
		audits := func(action string, department uuid.UUID) (n int64) {
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Table("audit_events").Where("actor_subject='system:iam-assignment-department-scope' AND action=? AND resource_type='organization_departments' AND resource_id=?", action, department).Count(&n).Error
			}))
			return n
		}
		confirmed, refused := audits("assignment_department_confirmed", root), audits("assignment_department_refused", retired)
		_, err := ask(t, testutil.TenantA, request(s.hospitalID(), root))
		require.NoError(t, err)
		_, err = ask(t, testutil.TenantA, request(s.hospitalID(), retired))
		require.NoError(t, err)
		require.Equal(t, confirmed+1, audits("assignment_department_confirmed", root))
		require.Equal(t, refused+1, audits("assignment_department_refused", retired))
	})
	t.Run("answers reflect the structure at delivery", func(t *testing.T) {
		live := request(s.hospitalID(), child)
		eligible, err := ask(t, testutil.TenantA, live)
		require.NoError(t, err)
		require.True(t, eligible)
		s.put(t, child, "", root, "retired")
		eligible, err = ask(t, testutil.TenantA, request(s.hospitalID(), child))
		require.NoError(t, err)
		require.False(t, eligible)
	})
	t.Run("malformed requests fail without an answer", func(t *testing.T) {
		valid := request(s.hospitalID(), root)
		raw, err := json.Marshal(valid)
		require.NoError(t, err)
		for name, evt := range map[string]events.Event{
			"wrong version":     {Type: consumer.Subject, Version: 2, TenantID: testutil.TenantA, Data: raw},
			"wrong type":        {Type: "helivanta.in.iam.other.v1", Version: 1, TenantID: testutil.TenantA, Data: raw},
			"directed":          {Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, DestinationTenantID: testutil.TenantB, Data: raw},
			"unknown field":     {Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: json.RawMessage(`{"proposal_id":"` + valid.ProposalID.String() + `","request_id":"` + valid.RequestID.String() + `","hospital_id":"` + valid.HospitalID.String() + `","department_id":"` + root.String() + `","eligible":true}`)},
			"non-v7 department": {Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: mustJSON(t, iamcontract.AssignmentDepartmentScopeRequestedData{ProposalID: valid.ProposalID, RequestID: valid.RequestID, HospitalID: valid.HospitalID, DepartmentID: uuid.New()})},
			"non-v7 request":    {Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: mustJSON(t, iamcontract.AssignmentDepartmentScopeRequestedData{ProposalID: valid.ProposalID, RequestID: uuid.New(), HospitalID: valid.HospitalID, DepartmentID: root})},
			"missing hospital":  {Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: mustJSON(t, iamcontract.AssignmentDepartmentScopeRequestedData{ProposalID: valid.ProposalID, RequestID: valid.RequestID, DepartmentID: root})},
			"trailing data":     {Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: append(append([]byte{}, raw...), []byte(`{}`)...)},
		} {
			t.Run(name, func(t *testing.T) {
				require.Error(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return consumer.Handle(ctx, tx, evt) }))
			})
		}
		var count int64
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Table("outbox_events").Where("subject=? AND payload->'data'->>'proposal_id'=?", organizationcontract.SubjectAssignmentDepartmentVerified, valid.ProposalID.String()).Count(&count).Error
		}))
		require.Zero(t, count)
	})
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}
