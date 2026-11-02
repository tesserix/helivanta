package organization_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func TestActualHospitalOwnerObservationCompletesReferenceWithoutDuplicatePublication(t *testing.T) {
	var workforce platform.Module
	for _, module := range bootstrap.Modules(nil, nil) {
		if module.Name() == "workforce" {
			workforce = module
		}
	}
	require.NotNil(t, workforce)
	router, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"manager": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"manager": {"organization.structure.manage", "organization.structure.read", "workforce.hospital.manage"}},
		Modules: []platform.Module{organization.New(), workforce},
	})
	hospital := uuid.NewString()
	request := uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(router, "PUT", "/v1/workforce/hospitals/"+hospital, "manager", `{"name":"Synthetic organization source"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	var observation events.Consumer
	for _, consumer := range workforce.Consumers(platform.Deps{Bus: bus}) {
		if consumer.Name == "workforce-organization-reference" {
			observation = consumer
		}
	}
	require.NotNil(t, observation.Handle, "the organization owner must use the actual hospital owner")
	path := "/v1/organization/hospitals/" + hospital + "/registrations/" + request
	w = organizationCall(router, "PUT", path, "manager", testutil.TenantA, `{"reason":"Synthetic source registration"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	messages := func(subject string) []events.Event {
		t.Helper()
		var encoded []string
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT payload::text FROM outbox_events WHERE tenant_id=? AND subject=? AND payload->'data'->>'request_id'=? ORDER BY created_at,id`, testutil.TenantA, subject, request).Scan(&encoded).Error
		}))
		result := make([]events.Event, len(encoded))
		for i, value := range encoded {
			require.NoError(t, json.Unmarshal([]byte(value), &result[i]))
		}
		return result
	}
	asked := messages(observation.Subject)
	require.Len(t, asked, 1)
	deliver := func(consumer events.Consumer, event events.Event) {
		t.Helper()
		require.NoError(t, db.WithTenant(ctx, event.TenantID, func(tx *gorm.DB) error { return consumer.Handle(ctx, tx, event) }))
	}
	deliver(observation, asked[0])
	deliver(observation, asked[0])
	var apply events.Consumer
	for _, consumer := range organization.New().Consumers(platform.Deps{Bus: bus}) {
		if consumer.Name == "organization-hospital-observation" {
			apply = consumer
		}
	}
	require.NotNil(t, apply.Handle)
	observed := messages(apply.Subject)
	require.Len(t, observed, 1, "a duplicate request must not resample or republish its observation")
	deliver(apply, observed[0])
	deliver(apply, observed[0])
	w = organizationCall(router, "GET", path, "manager", testutil.TenantA, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"verified"`)

	// A negative observation is a retained fact, not a fresh permission check on replay.
	hospital = uuid.NewString()
	request = uuid.Must(uuid.NewV7()).String()
	path = "/v1/organization/hospitals/" + hospital + "/registrations/" + request
	w = organizationCall(router, "PUT", path, "manager", testutil.TenantA, `{"reason":"Synthetic initially absent hospital"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	asked = messages(observation.Subject)
	require.Len(t, asked, 1)
	deliver(observation, asked[0])
	observed = messages(apply.Subject)
	require.Len(t, observed, 1)
	deliver(apply, observed[0])
	w = organizationCall(router, "GET", path, "manager", testutil.TenantA, "")
	require.Contains(t, w.Body.String(), `"status":"rejected"`)
	w = testutil.Do(router, "PUT", "/v1/workforce/hospitals/"+hospital, "manager", `{"name":"Synthetic hospital created later"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	deliver(observation, asked[0])
	require.Len(t, messages(apply.Subject), 1, "retry must retain the first observation instead of resampling a changed catalog")
	oldReply := observed[0]
	oldPath := path
	request = uuid.Must(uuid.NewV7()).String()
	path = "/v1/organization/hospitals/" + hospital + "/registrations/" + request
	w = organizationCall(router, "PUT", path, "manager", testutil.TenantA, `{"reason":"Synthetic new observation request"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	deliver(apply, oldReply)
	w = organizationCall(router, "GET", path, "manager", testutil.TenantA, "")
	require.Contains(t, w.Body.String(), `"status":"pending"`, "a delayed old reply cannot replace the newer registration")
	asked = messages(observation.Subject)
	require.Len(t, asked, 1)
	deliver(observation, asked[0])
	observed = messages(apply.Subject)
	require.Len(t, observed, 1)
	deliver(apply, observed[0])
	w = organizationCall(router, "GET", path, "manager", testutil.TenantA, "")
	require.Contains(t, w.Body.String(), `"status":"verified"`)
	w = organizationCall(router, "GET", oldPath, "manager", testutil.TenantA, "")
	require.Contains(t, w.Body.String(), `"status":"rejected"`, "old command history remains immutable")
	var conflicting map[string]any
	require.NoError(t, json.Unmarshal(observed[0].Data, &conflicting))
	conflicting["exists"] = false
	bad := observed[0]
	encodedConflict, err := json.Marshal(conflicting)
	bad.Data = encodedConflict
	require.NoError(t, err)
	err = db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return apply.Handle(ctx, tx, bad) })
	require.Error(t, err, "contradictory duplicate observations must fail closed")
	w = organizationCall(router, "GET", path, "manager", testutil.TenantA, "")
	require.Contains(t, w.Body.String(), `"status":"verified"`)
}
