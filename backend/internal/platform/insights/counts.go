// Package insights implements bounded calendar aggregates for owner-module dashboards.
package insights

import (
	"errors"
	"net/url"
	"time"
	_ "time/tzdata" // IANA zones must also work in minimal runtime images.

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Window is an inclusive calendar range with bounded output cardinality.
type Window struct {
	From, To, End  time.Time
	Interval, Zone string
	Buckets        []string
}

var errWindow = errors.New("use from/to dates between 2000 and 2199, an IANA timezone and day/week/month/year interval; maximum five years and 366 buckets")

// Parse refuses ambiguous or unbounded requests rather than guessing a scope.
func Parse(q url.Values) (Window, error) {
	for key, values := range q {
		if len(values) != 1 || (key != "from" && key != "to" && key != "timezone" && key != "interval") {
			return Window{}, errWindow
		}
	}
	zone := q.Get("timezone")
	if zone == "" || zone == "Local" || len(zone) > 100 {
		return Window{}, errWindow
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Window{}, errWindow
	}
	from, e1 := time.Parse(time.DateOnly, q.Get("from"))
	to, e2 := time.Parse(time.DateOnly, q.Get("to"))
	if e1 != nil || e2 != nil || to.Before(from) || from.Year() < 2000 || to.Year() > 2199 || to.After(from.AddDate(5, 0, -1)) {
		return Window{}, errWindow
	}
	w := Window{From: calendarBoundary(from, loc), To: calendarBoundary(to, loc), End: calendarBoundary(to.AddDate(0, 0, 1), loc), Interval: q.Get("interval"), Zone: zone}
	start := from
	switch w.Interval {
	case "day":
	case "week":
		start = from.AddDate(0, 0, -(int(from.Weekday())+6)%7)
	case "month":
		start = time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	case "year":
		start = time.Date(from.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	default:
		return Window{}, errWindow
	}
	for day := start; !day.After(to); {
		w.Buckets = append(w.Buckets, day.Format(time.DateOnly))
		if len(w.Buckets) > 366 {
			return Window{}, errWindow
		}
		switch w.Interval {
		case "day":
			day = day.AddDate(0, 0, 1)
		case "week":
			day = day.AddDate(0, 0, 7)
		case "month":
			day = day.AddDate(0, 1, 0)
		case "year":
			day = day.AddDate(1, 0, 0)
		}
	}
	return w, nil
}

// Calendar iteration stays in UTC; midnight transitions must not shift date labels.
func calendarBoundary(date time.Time, loc *time.Location) time.Time {
	var first time.Time
	for _, hours := range []time.Duration{-48, 0, 48} {
		_, offset := date.Add(hours * time.Hour).In(loc).Zone()
		candidate := date.Add(-time.Duration(offset) * time.Second)
		// Choose the earliest valid midnight, or the first instant after a skipped midnight.
		if candidate.In(loc).Format(time.DateOnly) >= date.Format(time.DateOnly) && (first.IsZero() || candidate.Before(first)) {
			first = candidate
		}
	}
	return first
}

// Point holds counts for one calendar bucket, including empty buckets.
type Point struct {
	Date   string           `json:"date"`
	Counts map[string]int64 `json:"counts"`
}

// Response contains counts only, never patient or staff records.
type Response struct {
	From     string           `json:"from"`
	To       string           `json:"to"`
	Interval string           `json:"interval"`
	Timezone string           `json:"timezone"`
	AsAt     time.Time        `json:"as_at"`
	Totals   map[string]int64 `json:"totals"`
	Series   []Point          `json:"series"`
}

// Counts resolves a fixed owner-supplied table/dimension under the active tenant.
// Table and column names are constants at call sites, never request parameters.
func Counts(db *tenantdb.DB, table, dimension string, categories []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		w, err := Parse(c.Request.URL.Query())
		if err != nil {
			respond.BadRequest(c, err)
			return
		}
		var rows []struct {
			Date, Category string
			Count          int64
		}
		asAt := time.Now().UTC()
		err = db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			if err := tx.Exec("SET LOCAL statement_timeout = '2s'").Error; err != nil {
				return err
			}
			// Explicit equality prevents future group-read RLS from widening this dashboard.
			return tx.Table(table).
				Select("date_trunc(?, created_at AT TIME ZONE ?)::date::text AS date, "+dimension+" AS category, count(*) AS count", w.Interval, w.Zone).
				Where("tenant_id = ? AND created_at >= ? AND created_at < ?", p.TenantID, w.From, w.End).
				Group("1, 2").Scan(&rows).Error
		})
		if err != nil {
			respond.InternalErr(c, err, "could not load insights")
			return
		}
		result := Response{From: c.Query("from"), To: c.Query("to"), Interval: w.Interval, Timezone: w.Zone, AsAt: asAt, Totals: map[string]int64{}, Series: make([]Point, 0, len(w.Buckets))}
		points := map[string]map[string]int64{}
		for _, date := range w.Buckets {
			counts := map[string]int64{}
			for _, category := range categories {
				counts[category] = 0
				result.Totals[category] = 0
			}
			points[date] = counts
			result.Series = append(result.Series, Point{Date: date, Counts: counts})
		}
		for _, row := range rows {
			if counts, exists := points[row.Date]; exists {
				counts[row.Category] = row.Count
				result.Totals[row.Category] += row.Count
			}
		}
		respond.OK(c, result)
	}
}
