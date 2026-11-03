package organization_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Canonical owner wire model under test.
)

func TestPostingDepartmentChoicePageFitsWireBudget(t *testing.T) {
	parent := uuid.Must(uuid.NewV7())
	hospital := uuid.New()
	rows := make([]organization.DepartmentOutput, 50)
	for i := range rows {
		rows[i] = organization.DepartmentOutput{
			ID: uuid.Must(uuid.NewV7()), HospitalID: hospital, RevisionID: uuid.Must(uuid.NewV7()),
			Version: 9007199254740991, Code: strings.Repeat("X", 38) + fmt.Sprintf("%02d", i), Name: strings.Repeat("&", 120),
			Kind: "sub_unit", ParentID: &parent, Status: "active",
			EffectiveAt: time.Date(2026, 11, 3, 1, 2, 3, 123456789, time.UTC),
			CreatedAt:   time.Date(2026, 11, 3, 1, 2, 3, 123456789, time.UTC),
		}
	}
	payload, err := json.Marshal(map[string]any{"data": rows, "page": map[string]any{"has_more": true, "next_cursor": strings.Repeat("X", 2048)}})
	require.NoError(t, err)
	require.LessOrEqual(t, len(payload), 64*1024, "maximum canonical fields, HTML escaping and continuation must fit the documented response budget")
}
