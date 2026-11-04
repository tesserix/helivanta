package workforce

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPostingManagementContextRequiresExplicitReviewOptIn(t *testing.T) {
	selector := "purpose=review&expected_version=1&expected_revision_id=019a0000-0000-7000-8000-000000000001"
	for _, tc := range []struct {
		name, query       string
		valid, management bool
	}{
		{"legacy review", selector, true, false},
		{"retained supervision", selector + "&supervision_context=v1", true, false},
		{"explicit management review", selector + "&supervision_context=v1&management_review=current", true, true},
		{"no supervision opt-in", selector + "&management_review=current", false, false},
		{"submission cannot request management", "purpose=submission&expected_version=1&expected_revision_id=019a0000-0000-7000-8000-000000000001&supervision_context=v1&management_review=current", false, false},
		{"repeated management mode", selector + "&supervision_context=v1&management_review=current&management_review=current", false, false},
		{"unknown management mode", selector + "&supervision_context=v1&management_review=all", false, false},
		{"empty management mode", selector + "&supervision_context=v1&management_review=", false, false},
		{"unrecognized extra selector", selector + "&supervision_context=v1&management_review=current&extra=true", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("GET", "/context?"+tc.query, nil)
			in, ok := postingContextRequest(c)
			require.Equal(t, tc.valid, ok, response.Body.String())
			if !tc.valid {
				require.Equal(t, 400, response.Code)
				return
			}
			body, err := json.Marshal(in)
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, json.Unmarshal(body, &fields))
			if tc.management {
				require.Equal(t, "current", fields["management_review"])
			} else {
				require.NotContains(t, fields, "management_review")
			}
		})
	}
}

func TestPostingChainContextRequiresExclusiveReviewOptIn(t *testing.T) {
	base := "purpose=review&expected_version=1&expected_revision_id=019a0000-0000-7000-8000-000000000001"
	for _, tc := range []struct {
		name, query string
		valid       bool
	}{
		{"current chain", base + "&supervision_context=v1&chain_review=current", true},
		{"missing supervision", base + "&chain_review=current", false},
		{"mixed mode", base + "&supervision_context=v1&chain_review=current&management_review=current", false},
		{"repeated", base + "&supervision_context=v1&chain_review=current&chain_review=current", false},
		{"unknown", base + "&supervision_context=v1&chain_review=all", false},
		{"submission", "purpose=submission&expected_version=1&expected_revision_id=019a0000-0000-7000-8000-000000000001&supervision_context=v1&chain_review=current", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("GET", "/context?"+tc.query, nil)
			in, ok := postingContextRequest(c)
			require.Equal(t, tc.valid, ok, response.Body.String())
			if ok {
				require.Equal(t, "current", *in.ChainReview)
			} else {
				require.Equal(t, 400, response.Code)
			}
		})
	}
}
