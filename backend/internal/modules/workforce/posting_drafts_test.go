package workforce_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestStaffPostingDraftFailsClosedWithoutCanonicalDepartmentOwner(t *testing.T) {
	h := newRequestHarness(t, func(opts *testutil.HarnessOptions) {
		opts.Perms["admin"] = append(opts.Perms["admin"], "workforce.posting.prepare")
	})
	staff := h.staff("SYN-POSTING-OWNER-OUTAGE")
	path := "/staff/" + staff + "/posting-drafts/" + uuid.Must(uuid.NewV7()).String() + "/commands/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"expected_version":0,"staff_version":1,"employment_epoch":1,"department_id":%q,"department_revision_id":%q,"starts_at":"2027-01-01T00:00:00Z","reason":"Synthetic posting preparation"}`, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String())
	code, out := h.do("PUT", h.base+path, "admin", body)
	require.Equal(t, 503, code, out)
	require.Contains(t, out, `"error":"posting_source_unavailable"`)
	code, _ = h.do("GET", h.base+"/staff/"+staff+"/posting-drafts", "admin", "")
	require.Equal(t, 503, code, "unavailable source must not fabricate an empty current collection")
}
