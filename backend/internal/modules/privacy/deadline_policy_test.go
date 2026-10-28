package privacy_test

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/privacy" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"go.temporal.io/sdk/mocks"
	"strings"
	"testing"
)

func TestDeadlinePolicyNeedsAcceptedReviewAndConfiguredWorkflow(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{PrivacyDeadlinesEnabled: enabled, Workflows: new(mocks.Client), Tokens: map[string]string{"admin": testutil.TenantA, "officer": testutil.TenantA, "dpo": testutil.TenantA, "patient": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {"privacy.officer.manage", "privacy.deadline.manage"}}, Modules: []platform.Module{privacy.New()}})
			for _, role := range []string{"primary", "escalation"} {
				user := "officer"
				if role == "escalation" {
					user = "dpo"
				}
				path := "/v1/privacy/officers/" + uuid.Must(uuid.NewV7()).String()
				body := fmt.Sprintf(`{"role":%q,"subject":%q,"display_name":"Synthetic Officer","contact_email":"privacy@example.invalid","review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"}`, role, "user-"+user, uuid.Must(uuid.NewV7()).String())
				require.Equal(t, 201, testutil.Do(r, "PUT", path, "admin", body).Code)
				require.Equal(t, 200, testutil.Do(r, "POST", path+"/accept", user, `{}`).Code)
			}
			path := "/v1/privacy/deadline-policies/" + uuid.Must(uuid.NewV7()).String()
			body := fmt.Sprintf(`{"version_label":"synthetic-v1","acknowledgement_seconds":3600,"resolution_seconds":7200,"pause_while_waiting":true,"review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())
			require.Equal(t, 403, testutil.Do(r, "PUT", path, "patient", body).Code)
			created := testutil.Do(r, "PUT", path, "admin", body)
			require.Equal(t, 201, created.Code, created.Body.String())
			require.Contains(t, created.Body.String(), `"status":"draft"`)
			require.Equal(t, 403, testutil.Do(r, "POST", path+"/approve", "admin", `{}`).Code)
			approved := testutil.Do(r, "POST", path+"/approve", "dpo", `{}`)
			if !enabled {
				require.Equal(t, 503, approved.Code, approved.Body.String())
				return
			}
			require.Equal(t, 200, approved.Code, approved.Body.String())
			require.Contains(t, approved.Body.String(), `"status":"active"`)
			require.NotContains(t, approved.Body.String(), "review_reference")
			require.JSONEq(t, approved.Body.String(), testutil.Do(r, "POST", path+"/approve", "dpo", `{}`).Body.String())
		})
	}
}

func TestDeadlinePolicyRequiresExplicitPauseChoice(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {"privacy.deadline.manage"}}, Modules: []platform.Module{privacy.New()}})
	for _, choice := range []string{"", `,"pause_while_waiting":null`, `,"pause_while_waiting":false`, `,"pause_while_waiting":true`} {
		t.Run(choice, func(t *testing.T) {
			path := "/v1/privacy/deadline-policies/" + uuid.Must(uuid.NewV7()).String()
			body := fmt.Sprintf(`{"version_label":%q,"acknowledgement_seconds":3600,"resolution_seconds":7200,"review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"%s}`, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), choice)
			response := testutil.Do(r, "PUT", path, "admin", body)
			expected := 201
			if choice == "" || strings.Contains(choice, "null") {
				expected = 400
			}
			require.Equal(t, expected, response.Code, response.Body.String())
			if expected == 201 {
				require.Equal(t, 200, testutil.Do(r, "PUT", path, "admin", body).Code)
			}
		})
	}
}
