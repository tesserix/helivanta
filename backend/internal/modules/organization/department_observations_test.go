package organization_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	organizationcontract "github.com/tesserix/helivanta/internal/modules/organization/contract"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func departmentRequestConsumer(t *testing.T, bus *events.Bus) events.Consumer {
	t.Helper()
	for _, c := range organization.New().Consumers(platform.Deps{Bus: bus}) {
		if c.Name == "organization-department-link-request" {
			return c
		}
	}
	t.Fatal("department observation consumer is not registered")
	return events.Consumer{}
}

func departmentRequest(t *testing.T, hospital string) events.Event {
	t.Helper()
	raw, err := json.Marshal(workforcecontract.DepartmentLinkRequestedData{
		RequestID: uuid.Must(uuid.NewV7()), HospitalID: uuid.MustParse(hospital),
		MasterID: uuid.New(), MasterRevisionID: uuid.New(),
		DepartmentID: uuid.Must(uuid.NewV7()), DepartmentRevisionID: uuid.Must(uuid.NewV7()),
	})
	require.NoError(t, err)
	return events.Event{Type: workforcecontract.SubjectDepartmentLinkRequested, Version: 1, TenantID: testutil.TenantA, Data: raw}
}

func TestDepartmentObservationRepairRetainsFirstAnswerAndRejectsChangedIdentity(t *testing.T) {
	r, db, bus, ctx, hospital := departmentHarnessWithBus(t, organization.New())
	c := departmentRequestConsumer(t, bus)
	evt := departmentRequest(t, hospital)
	var claim workforcecontract.DepartmentLinkRequestedData
	require.NoError(t, json.Unmarshal(evt.Data, &claim))
	apply := func(event events.Event) error {
		return db.WithTenant(ctx, event.TenantID, func(tx *gorm.DB) error { return c.Handle(ctx, tx, event) })
	}
	require.NoError(t, apply(evt))
	w := organizationCall(r, "PUT", departmentPath(hospital, claim.DepartmentID.String(), claim.DepartmentRevisionID.String()), "manager", testutil.TenantA, departmentIntent(0, "SYN-RECOVERY", "Synthetic Recovery Department", "department", "", "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, apply(evt), "repair re-publishes the retained negative answer, not a new current observation")
	changed := claim
	changed.MasterID = uuid.New()
	conflict := evt
	var err error
	conflict.Data, err = json.Marshal(changed)
	require.NoError(t, err)
	require.Error(t, apply(conflict), "the same request cannot be rebound to another HR source")
	var replies []struct{ Payload []byte }
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload FROM outbox_events WHERE subject=? AND payload->'data'->>'request_id'=? ORDER BY created_at,id`, organizationcontract.SubjectDepartmentLinkObserved, claim.RequestID.String()).Scan(&replies).Error
	}))
	require.Len(t, replies, 2)
	var first, second events.Event
	require.NoError(t, json.Unmarshal(replies[0].Payload, &first))
	require.NoError(t, json.Unmarshal(replies[1].Payload, &second))
	require.JSONEq(t, string(first.Data), string(second.Data))
	var answer organizationcontract.DepartmentLinkObservedData
	require.NoError(t, json.Unmarshal(first.Data, &answer))
	require.NotNil(t, answer.MatchesCurrent)
	require.False(t, *answer.MatchesCurrent)
	require.False(t, answer.ObservedAt.IsZero())
	newClaim := claim
	newClaim.RequestID = uuid.Must(uuid.NewV7())
	fresh := evt
	fresh.Data, err = json.Marshal(newClaim)
	require.NoError(t, err)
	require.NoError(t, apply(fresh))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var rows []struct {
			ID             uuid.UUID
			MatchesCurrent bool
		}
		if err := tx.Table("organization_department_observations").Select("id,matches_current").Order("created_at,id").Find(&rows).Error; err != nil {
			return err
		}
		require.Len(t, rows, 2)
		found := map[uuid.UUID]bool{}
		for _, row := range rows {
			found[row.ID] = row.MatchesCurrent
		}
		require.False(t, found[claim.RequestID])
		require.True(t, found[newClaim.RequestID])
		var count int64
		if err := tx.Table("audit_events").Where("action='department_link_observed' AND resource_id=?", claim.RequestID).Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, count, "repair does not create another business audit")
		return nil
	}))
}

type departmentObservationFaultModule struct {
	*organization.Module
	target string
}

func (m departmentObservationFaultModule) Migrations() []tenantdb.Migration {
	sql := `ALTER TABLE outbox_events ADD CONSTRAINT department_observation_test_fault CHECK(subject<>'helivanta.in.organization.department_link_observed.v1') NOT VALID;`
	if m.target == "audit" {
		sql = `ALTER TABLE audit_events ADD CONSTRAINT department_observation_test_fault CHECK(action<>'department_link_observed') NOT VALID;`
	}
	return append(m.Module.Migrations(), tenantdb.Migration{ID: "9001_organization_department_observation_fault", SQL: sql})
}

func TestDepartmentObservationAuditAndOutboxFailureRollbackThenRetry(t *testing.T) {
	for _, target := range []string{"audit", "outbox"} {
		t.Run(target, func(t *testing.T) {
			_, db, bus, ctx, hospital := departmentHarnessWithBus(t, departmentObservationFaultModule{organization.New(), target})
			c := departmentRequestConsumer(t, bus)
			evt := departmentRequest(t, hospital)
			apply := func() error {
				return db.WithTenant(ctx, evt.TenantID, func(tx *gorm.DB) error { return c.Handle(ctx, tx, evt) })
			}
			require.Error(t, apply())
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var counts struct{ Observations, Audits, Replies int64 }
				if err := tx.Raw(`SELECT (SELECT count(*) FROM organization_department_observations) AS observations,(SELECT count(*) FROM audit_events WHERE action='department_link_observed') AS audits,(SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.organization.department_link_observed.v1') AS replies`).Scan(&counts).Error; err != nil {
					return err
				}
				require.Zero(t, counts.Observations)
				require.Zero(t, counts.Audits)
				require.Zero(t, counts.Replies)
				return nil
			}))
			repair := `ALTER TABLE outbox_events DROP CONSTRAINT department_observation_test_fault;`
			if target == "audit" {
				repair = `ALTER TABLE audit_events DROP CONSTRAINT department_observation_test_fault;`
			}
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "9002_organization_department_observation_repair", SQL: repair}}))
			require.NoError(t, apply())
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var count int64
				if err := tx.Table("organization_department_observations").Count(&count).Error; err != nil {
					return err
				}
				require.EqualValues(t, 1, count)
				return nil
			}))
		})
	}
}

func TestDepartmentObservationRejectsMalformedClaimsBeforePersisting(t *testing.T) {
	_, db, bus, ctx, hospital := departmentHarnessWithBus(t, organization.New())
	c := departmentRequestConsumer(t, bus)
	valid := departmentRequest(t, hospital)
	for _, tc := range []struct {
		name   string
		change func(*events.Event)
	}{
		{"type", func(e *events.Event) { e.Type = "unexpected" }},
		{"version", func(e *events.Event) { e.Version = 2 }},
		{"directed", func(e *events.Event) { e.DestinationTenantID = testutil.TenantB }},
		{"oversized", func(e *events.Event) { e.Data = []byte(strings.Repeat(" ", 2049)) }},
		{"null", func(e *events.Event) { e.Data = []byte(`null`) }},
		{"missing identity", func(e *events.Event) { e.Data = []byte(`{}`) }},
		{"extra object", func(e *events.Event) { e.Data = append(append([]byte{}, e.Data...), []byte(` {}`)...) }},
		{"unknown field", func(e *events.Event) { e.Data = append([]byte(`{"unreviewed":true,`), e.Data[1:]...) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evt := valid
			tc.change(&evt)
			require.Error(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return c.Handle(ctx, tx, evt) }))
		})
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("organization_department_observations").Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		return nil
	}))
}
