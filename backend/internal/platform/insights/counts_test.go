package insights_test

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/lab"
	"github.com/tesserix/helivanta/internal/modules/medicore"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/insights"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestCalendarWindows(t *testing.T) {
	for _, tc := range []struct {
		query string
		dates []string
		hours float64
	}{
		{"from=2026-03-08&to=2026-03-08&timezone=America/Havana&interval=day", []string{"2026-03-08"}, 23},
		{"from=2026-03-08&to=2026-03-08&timezone=America/New_York&interval=day", []string{"2026-03-08"}, 23},
		{"from=2026-11-01&to=2026-11-01&timezone=America/Havana&interval=day", []string{"2026-11-01"}, 25},
		{"from=2011-12-29&to=2011-12-31&timezone=Pacific/Apia&interval=day", []string{"2011-12-29", "2011-12-30", "2011-12-31"}, 48},
		{"from=2011-12-30&to=2011-12-30&timezone=Pacific/Apia&interval=day", []string{"2011-12-30"}, 0},
		{"from=2026-11-01&to=2026-11-01&timezone=America/New_York&interval=day", []string{"2026-11-01"}, 25},
		{"from=2024-02-28&to=2024-03-01&timezone=UTC&interval=day", []string{"2024-02-28", "2024-02-29", "2024-03-01"}, 72},
		{"from=2026-10-04&to=2026-10-05&timezone=UTC&interval=week", []string{"2026-09-28", "2026-10-05"}, 48},
		{"from=2026-12-31&to=2027-01-01&timezone=UTC&interval=month", []string{"2026-12-01", "2027-01-01"}, 48},
		{"from=2026-12-31&to=2027-01-01&timezone=UTC&interval=year", []string{"2026-01-01", "2027-01-01"}, 48},
	} {
		t.Run(tc.query, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			require.NoError(t, err)
			w, err := insights.Parse(q)
			require.NoError(t, err)
			require.Equal(t, tc.dates, w.Buckets)
			require.Equal(t, tc.hours, w.End.Sub(w.From).Hours())
		})
	}
}

func TestRejectsAmbiguousOrUnboundedWindows(t *testing.T) {
	base := url.Values{"from": {"2026-01-01"}, "to": {"2026-01-31"}, "timezone": {"UTC"}, "interval": {"day"}}
	for _, tc := range []struct {
		key    string
		values []string
	}{
		{"from", []string{"2026-02-30"}}, {"from", []string{"1999-12-31"}}, {"to", []string{"2200-01-01"}},
		{"to", []string{"2025-01-01"}}, {"to", []string{"2031-01-01"}}, {"to", []string{"2027-01-02"}},
		{"timezone", []string{""}}, {"timezone", []string{"Local"}}, {"timezone", []string{"Mars/Olympus"}},
		{"interval", []string{"hour"}}, {"interval", []string{"day", "year"}}, {"tenant_id", []string{testutil.TenantB}},
	} {
		t.Run(tc.key+tc.values[0], func(t *testing.T) {
			q, err := url.ParseQuery(base.Encode())
			require.NoError(t, err)
			q[tc.key] = tc.values
			_, err = insights.Parse(q)
			require.Error(t, err)
		})
	}
}

func TestOwnerEndpointsEnforcePermissionsTenancyAndCohortBoundaries(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"a": testutil.TenantA, "b": testutil.TenantB, "denied": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"a": {medicore.PermVisitRead, pharmacy.PermDispenseRead, lab.PermOrderRead}, "b": {medicore.PermVisitRead, pharmacy.PermDispenseRead, lab.PermOrderRead}},
		Modules: []platform.Module{medicore.New(), pharmacy.New(), lab.New()},
	})
	ctx = tenantdb.WithActor(ctx, "insights-fixture")
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			for _, query := range []string{
				`INSERT INTO medicore_visits (tenant_id,patient_name,department,created_at) SELECT ?, 'Synthetic private', 'IPD', stamp FROM unnest(ARRAY['2026-03-08T05:00:00Z'::timestamptz,'2026-03-09T03:59:59Z','2026-03-09T04:00:00Z']) stamp`,
				`INSERT INTO pharmacy_dispenses (tenant_id,visit_id,patient_name,status,created_at) SELECT ?, gen_random_uuid(),'Synthetic private','pending',stamp FROM unnest(ARRAY['2026-03-08T05:00:00Z'::timestamptz,'2026-03-09T03:59:59Z','2026-03-09T04:00:00Z']) stamp`,
				`INSERT INTO lab_orders (tenant_id,visit_id,patient_name,status,created_at) SELECT ?, gen_random_uuid(),'Synthetic private','pending',stamp FROM unnest(ARRAY['2026-03-08T05:00:00Z'::timestamptz,'2026-03-09T03:59:59Z','2026-03-09T04:00:00Z']) stamp`,
			} {
				if err := tx.Exec(query, tenant).Error; err != nil {
					return err
				}
			}
			return nil
		}))
	}
	for _, module := range []string{"medicore", "pharmacy", "lab"} {
		t.Run(module, func(t *testing.T) {
			path := "/v1/" + module + "/insights?from=2026-03-08&to=2026-03-08&interval=day&timezone=America%2FNew_York"
			for _, token := range []string{"a", "b"} {
				for _, zone := range []string{"America%2FNew_York", "America%2FHavana"} {
					w := testutil.Do(r, "GET", strings.Replace(path, "America%2FNew_York", zone, 1), token, "")
					require.Equal(t, 200, w.Code, w.Body.String())
					var body insights.Response
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
					var total int64
					for _, count := range body.Totals {
						total += count
					}
					require.Equal(t, int64(2), total)
					require.Len(t, body.Series, 1)
					require.Equal(t, "2026-03-08", body.From)
					require.Equal(t, "2026-03-08", body.To)
					require.Equal(t, "2026-03-08", body.Series[0].Date)
					require.NotContains(t, w.Body.String(), "Synthetic private")
				}
			}
			require.Equal(t, 403, testutil.Do(r, "GET", path, "denied", "").Code)
			require.Equal(t, 401, testutil.Do(r, "GET", path, "", "").Code)
			require.Equal(t, 400, testutil.Do(r, "GET", path+"&tenant_id="+testutil.TenantB, "a", "").Code)
			w := testutil.Do(r, "GET", "/v1/"+module+"/insights?from=2025-01-01&to=2025-01-01&interval=day&timezone=UTC", "a", "")
			require.Equal(t, 200, w.Code)
			var empty insights.Response
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &empty))
			for _, count := range empty.Totals {
				require.Zero(t, count)
			}
			require.Len(t, empty.Series, 1)
		})
	}
}

func TestCountsRejectBeforeAccessingDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		principal bool
		query     string
		status    int
	}{{false, "", 401}, {true, "", 400}, {true, "?timezone=UTC&interval=day&from=2026-01-01&to=2026-01-01", 500}} {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if tc.principal {
				c.Set("authn.principal", authn.Principal{Subject: "synthetic", TenantID: testutil.TenantA})
			}
		})
		// A real missing table exercises safe SQL failure without replacing the repository.
		_, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{})
		r.GET("/counts", insights.Counts(db, "missing_table", "status", []string{"pending"}))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/counts"+tc.query, nil))
		require.Equal(t, tc.status, w.Code)
		require.NotContains(t, w.Body.String(), "connection")
	}
}
