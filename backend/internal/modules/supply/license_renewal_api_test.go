package supply_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestLicenceReadsIncludePrivatePremiseLocalRenewalWithoutChangingHistory(t *testing.T) {
	r, db := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Renewal Supplier")).Code)
	premise := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	licence := root + "/licenses/" + uuid.Must(uuid.NewV7()).String()
	w := testutil.Do(r, "PUT", licence, "owner", licenseBody(0, premise))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), `"renewal"`, "time-sensitive advisories do not enter retained write snapshots")
	w = testutil.Do(r, "GET", licence, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var detail struct {
		Renewal struct {
			TimeZone      string `json:"time_zone"`
			LocalDate     string `json:"local_date"`
			DateStatus    string `json:"date_status"`
			ObservedAt    string `json:"observed_at"`
			DaysRemaining int    `json:"days_remaining"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.Equal(t, "Asia/Kolkata", detail.Renewal.TimeZone)
	require.Equal(t, "current", detail.Renewal.DateStatus)
	require.Positive(t, detail.Renewal.DaysRemaining)
	require.NotEmpty(t, detail.Renewal.LocalDate)
	require.NotEmpty(t, detail.Renewal.ObservedAt)
	observed, err := time.Parse(time.RFC3339Nano, detail.Renewal.ObservedAt)
	require.NoError(t, err)
	zone, err := time.LoadLocation(detail.Renewal.TimeZone)
	require.NoError(t, err)
	require.Equal(t, observed.In(zone).Format(time.DateOnly), detail.Renewal.LocalDate)
	var before, after int64
	countEvents := func(target *int64) {
		require.NoError(t, partyTransaction(t, db, strings.TrimPrefix(root, "/v1/supply/parties/"), func(tx *gorm.DB) error {
			return tx.Table("outbox_events").Where("tenant_id=?", testutil.TenantA).Count(target).Error
		}))
	}
	countEvents(&before)
	w = testutil.Do(r, "GET", root+"/licenses?limit=1", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"time_zone":"Asia/Kolkata"`)
	w = testutil.Do(r, "GET", licence+"/revisions", "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), `"renewal"`)
	for _, actor := range []string{"foreign", "manager", "auditor"} {
		for _, path := range []string{licence, root + "/licenses"} {
			w = testutil.Do(r, "GET", path, actor, "")
			require.Equal(t, 404, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "Asia/Kolkata")
		}
	}
	grant := root + "/access/user-auditor/auditor"
	require.Equal(t, 200, testutil.Do(r, "PUT", grant, "owner", "").Code)
	w = testutil.Do(r, "GET", licence, "auditor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"time_zone":"Asia/Kolkata"`)
	require.Equal(t, 204, testutil.Do(r, "DELETE", grant, "owner", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", licence, "auditor", "").Code)
	countEvents(&after)
	require.Equal(t, before+2, after, "only the two access changes emit events; renewal reads emit none")
}

func TestLicenceRenewalFailsClosedWhenItsPremiseIsInaccessible(t *testing.T) {
	r, db := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Hidden Renewal Premise")).Code)
	premise := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	licence := root + "/licenses/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", licence, "owner", licenseBody(0, premise)).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_hide_renewal_premise", SQL: `ALTER POLICY tenant_isolation ON supply_premises USING(false)`}}))
	for _, path := range []string{licence, root + "/licenses"} {
		w := testutil.Do(r, "GET", path, "owner", "")
		require.Equal(t, 404, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "Asia/Kolkata")
		require.NotContains(t, w.Body.String(), "TEST-LICENSE")
	}
}
