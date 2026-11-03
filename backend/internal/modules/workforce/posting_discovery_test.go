package workforce_test

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports the module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type postingDiscoveryMember struct{}

func (postingDiscoveryMember) IsMember(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestPostingScopeDiscoveryRejectsMissingAccountIntentBeforeSourceReads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "synthetic-reader", TenantID: "11111111-1111-4111-8111-111111111111"})
		c.Set("authz.permissions", authz.NewPermissionSet())
	})
	r := platform.NewRouter(e.Group("/v1"), postingDiscoveryMember{})
	workforce.New().Routes(r, platform.Deps{})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/workforce/posting-scopes", nil))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "session_changed")
}

func TestPostingWorkspacePagesFitWireBudget(t *testing.T) {
	targets := make([]workforce.StaffPostingTarget, 50)
	hospitals := make([]workforce.StaffPostingHospitalScope, 20)
	for i := range targets {
		targets[i] = workforce.StaffPostingTarget{ID: uuid.Must(uuid.NewV7()), EmployeeCode: strings.Repeat("<", 40), DisplayName: strings.Repeat("&", 150)}
	}
	for i := range hospitals {
		hospitals[i] = workforce.StaffPostingHospitalScope{HospitalID: uuid.Must(uuid.NewV7()), Name: strings.Repeat("&", 150), CanPrepare: true}
	}
	for _, rows := range []any{targets, hospitals} {
		payload, err := json.Marshal(map[string]any{"data": rows, "page": map[string]any{"has_more": true, "next_cursor": strings.Repeat("X", 2048)}})
		require.NoError(t, err)
		require.LessOrEqual(t, len(payload), 64*1024, "maximum escaped fields and continuation fit the documented wire budget")
	}
}

func TestPostingProtectedContextFitsWireBudget(t *testing.T) {
	reason := strings.Repeat("<", 500)
	row := workforce.StaffPostingProposalContext{Purpose: "review", DraftReason: reason, SubmissionReason: reason, DecisionReason: &reason}
	payload, err := json.Marshal(map[string]any{"data": row})
	require.NoError(t, err)
	require.LessOrEqual(t, len(payload), 20*1024, "three maximum escaped reasons and fixed source DTOs fit the protected-view budget")
}

func TestPostingHospitalInboxFitsWireBudget(t *testing.T) {
	end := time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)
	id := uuid.Must(uuid.NewV7())
	proposal := workforce.StaffPostingProposal{ID: id, RevisionID: id, CommandID: id, HospitalID: id, StaffID: id,
		Version: 9007199254740990, Status: "withdrawn", DraftID: id, DraftRevisionID: id, DraftVersion: 9007199254740990,
		GenerationID: id, StaffVersion: 9007199254740990, EmploymentEpoch: 9007199254740990, DepartmentID: id, DepartmentRevisionID: id,
		StartsAt: end, EndsAt: &end, EligibilityUntil: end, CreatedAt: end, RecordedAt: end}
	rows := make([]workforce.StaffPostingInboxItem, 8)
	for i := range rows {
		rows[i] = workforce.StaffPostingInboxItem{Proposal: proposal, Staff: workforce.StaffPostingTarget{ID: id, EmployeeCode: strings.Repeat("<", 40), DisplayName: strings.Repeat("<", 150)}}
	}
	encoded, err := json.Marshal(map[string]any{"data": rows, "page": map[string]any{"has_more": true, "next_cursor": strings.Repeat("X", 2048)}})
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), 20*1024, "maximum escaped labels, source pins and continuation fit the inbox budget")
}
