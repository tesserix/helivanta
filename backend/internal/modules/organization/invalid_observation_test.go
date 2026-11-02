package organization_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func TestObservationRequiresAnExplicitBoolean(t *testing.T) {
	r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"manager": testutil.TenantA}, Perms: map[string][]authz.Permission{"manager": {"organization.structure.manage", "organization.structure.read"}}, Modules: []platform.Module{organization.New()}})
	hospital := uuid.NewString()
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/organization/hospitals/" + hospital + "/registrations/" + id
	response := organizationCall(r, "PUT", path, "manager", testutil.TenantA, `{"reason":"Synthetic malformed observation test"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	var consumer events.Consumer
	for _, candidate := range organization.New().Consumers(platform.Deps{Bus: bus}) {
		if candidate.Name == "organization-hospital-observation" {
			consumer = candidate
		}
	}
	require.NotNil(t, consumer.Handle)
	for _, test := range []struct {
		name  string
		value any
	}{
		{"omitted", nil},
		{"null", json.RawMessage("null")},
		{"string_false", "false"},
		{"number_zero", 0},
		{"array", []bool{false}},
		{"object", map[string]bool{"value": false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := map[string]any{"request_id": id, "hospital_id": hospital}
			if test.value != nil {
				payload["exists"] = test.value
			}
			data, err := json.Marshal(payload)
			require.NoError(t, err)
			evt := events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: data}
			err = db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return consumer.Handle(ctx, tx, evt) })
			require.Error(t, err, "invalid boolean cannot become a fabricated negative owner observation")
			response := organizationCall(r, "GET", path, "manager", testutil.TenantA, "")
			require.Contains(t, response.Body.String(), `"status":"pending"`)
		})
	}
}
