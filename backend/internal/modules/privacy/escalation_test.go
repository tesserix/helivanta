package privacy_test

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"testing"
)

func TestEscalationOfficerDoesNotReplacePrimaryCaseOfficer(t *testing.T) {
	r, _, _, _, path := caseFixture(t)
	op := "/v1/privacy/officers/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"role":"escalation","subject":"user-peer","display_name":"Synthetic Escalation Officer","contact_email":"dpo@example.invalid","review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())
	nominated := testutil.Do(r, "PUT", op, "admin", body)
	require.Equal(t, 201, nominated.Code, nominated.Body.String())
	require.Equal(t, 200, testutil.Do(r, "POST", op+"/accept", "peer", `{}`).Code)
	require.Equal(t, 200, testutil.Do(r, "GET", path, "officer", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", path, "peer", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", "/v1/privacy/queue", "peer", "").Code)
	result := testutil.Do(r, "PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "peer", `{"expected_version":1,"action":"start_review","message":"Escalation officer review"}`)
	require.Equal(t, 201, result.Code, result.Body.String())
	primary := testutil.Do(r, "GET", "/v1/privacy/officer", "patient", "")
	require.Contains(t, primary.Body.String(), `"role":"primary"`)
	require.NotContains(t, primary.Body.String(), "dpo@example.invalid")
	require.Equal(t, 200, testutil.Do(r, "POST", op+"/revoke", "admin", fmt.Sprintf(`{"expected_version":2,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "peer", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", path, "officer", "").Code)
}
