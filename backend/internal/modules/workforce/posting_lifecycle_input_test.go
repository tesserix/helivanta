package workforce

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPostingLifecycleInputPreservesLegacyFingerprintMaterial(t *testing.T) {
	legacy := `{"expected_version":1,"expected_revision_id":"019a0000-0000-7000-8000-000000000001","decision":"approve","reason":"Synthetic management decision","sources_examined":true}`
	var in StaffPostingDecisionInput
	require.NoError(t, json.Unmarshal([]byte(legacy), &in))
	require.True(t, validPostingDecisionInput(&in))
	normalized, err := json.Marshal(in)
	require.NoError(t, err)
	require.Equal(t, legacy, string(normalized), "absent lifecycle data must not change old normalized command hashes")
}

func TestPostingLifecycleInputNormalizesMicrosecondsBeforeReplay(t *testing.T) {
	var material [][]byte
	for _, instant := range []string{"2020-01-01T00:00:00.123456789Z", "2020-01-01T05:30:00.123456001+05:30"} {
		body := fmt.Sprintf(`{"expected_version":1,"expected_revision_id":"019a0000-0000-7000-8000-000000000001","decision":"approve","reason":"Recover the exact committed command","sources_examined":true,"lifecycle_review":{"cutoff_versions":[0,2],"effective_until":%q}}`, instant)
		var in StaffPostingDecisionInput
		require.NoError(t, json.Unmarshal([]byte(body), &in))
		require.True(t, validPostingDecisionInput(&in), "static parsing must not reject an expired committed replay")
		encoded, err := json.Marshal(in)
		require.NoError(t, err)
		material = append(material, encoded)
	}
	require.Equal(t, material[0], material[1], "equivalent offset/submicrosecond instants have the same fingerprint material")
}

func TestPostingLifecycleInputRejectsNullObservations(t *testing.T) {
	for _, tc := range []struct {
		name, lifecycle string
		valid           bool
	}{
		{"valid root observation", `{"cutoff_versions":[0],"effective_until":"2027-01-01T00:00:00Z"}`, true},
		{"valid chain observation", `{"cutoff_versions":[0,2],"effective_until":"2027-01-01T00:00:00Z"}`, true},
		{"null envelope", `null`, false},
		{"null selected version", `{"cutoff_versions":[null],"effective_until":"2027-01-01T00:00:00Z"}`, false},
		{"null ancestor version", `{"cutoff_versions":[0,null],"effective_until":"2027-01-01T00:00:00Z"}`, false},
		{"unknown observation", `{"cutoff_versions":[0],"effective_until":"2027-01-01T00:00:00Z","unreviewed":true}`, false},
		{"empty observation", `{}`, false},
		{"negative version", `{"cutoff_versions":[0,-1],"effective_until":"2027-01-01T00:00:00Z"}`, false},
		{"non-absent pending cutoff", `{"cutoff_versions":[1],"effective_until":"2027-01-01T00:00:00Z"}`, false},
		{"unsafe version", `{"cutoff_versions":[0,9007199254740991],"effective_until":"2027-01-01T00:00:00Z"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			body := fmt.Sprintf(`{"expected_version":1,"expected_revision_id":"019a0000-0000-7000-8000-000000000001","decision":"approve","reason":"Synthetic examined posting sources","sources_examined":true,"lifecycle_review":%s}`, tc.lifecycle)
			c.Request = httptest.NewRequest("PUT", "/decisions/synthetic", strings.NewReader(body))
			var in StaffPostingDecisionInput
			parsed := decodeSized(c, &in, 4096)
			valid := parsed && validPostingDecisionInput(&in)
			require.Equal(t, tc.valid, valid, response.Body.String())
		})
	}
}
