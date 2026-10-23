package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// countedRow lives in a scratch table under the same forced-RLS boilerplate
// as every module table, because CountedListRoute only ever runs inside a
// tenant snapshot.
type countedRow struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}

func (countedRow) TableName() string                 { return "counted_rows" }
func (r countedRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

var countedMigrations = []tenantdb.Migration{{
	ID: "0002_counted_rows",
	SQL: `
		CREATE TABLE counted_rows (
		  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		  tenant_id uuid NOT NULL,
		  kind text NOT NULL,
		  created_at timestamptz NOT NULL
		);
		ALTER TABLE counted_rows ENABLE ROW LEVEL SECURITY;
		ALTER TABLE counted_rows FORCE ROW LEVEL SECURITY;
		CREATE POLICY tenant_isolation ON counted_rows
		  USING (hms_tenant_visible(tenant_id))
		  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
}}

// countedHarness seeds 5 "keep" rows (two sharing a timestamp) and 3
// "skip" rows the filter excludes, so a total that ignored the filter
// would be 8, not 5.
func countedHarness(t *testing.T) *tenantdb.DB {
	t.Helper()
	appDSN, adminDSN, systemDSN := testutil.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, tenantdb.Migrations()))
	require.NoError(t, db.Migrate(ctx, countedMigrations))
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	tid := uuid.MustParse(testTenant)
	require.NoError(t, db.WithTenant(ctx, testTenant, func(tx *gorm.DB) error {
		for i, at := range []int{0, 1, 2, 2, 3} {
			if err := tx.Create(&countedRow{TenantID: tid, Kind: "keep", CreatedAt: base.Add(time.Duration(at) * time.Minute)}).Error; err != nil {
				return fmt.Errorf("keep %d: %w", i, err)
			}
		}
		for i := range 3 {
			if err := tx.Create(&countedRow{TenantID: tid, Kind: "skip", CreatedAt: base.Add(time.Duration(i) * time.Minute)}).Error; err != nil {
				return err
			}
		}
		return nil
	}))
	return db
}

func keepOnly(c *gin.Context) (platform.Scope, bool) {
	return func(tx *gorm.DB) *gorm.DB { return tx.Where("kind = ?", "keep") }, true
}

func countedServer(t *testing.T, db *tenantdb.DB, k tenantdb.Keyset, h platform.CountedListHandler) (*gin.Engine, *platform.Router) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "alice", TenantID: testTenant})
		c.Set("authz.permissions", authz.NewPermissionSet(listRoutePerm))
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"), alwaysMemberListRoute{})
	platform.CountedListRoute[countedRow](r, db, "/counted", listRoutePerm, k, h)
	return e, r
}

type countedPage struct {
	Data []countedRow `json:"data"`
	Page struct {
		NextCursor *string `json:"next_cursor"`
		HasMore    bool    `json:"has_more"`
		Total      *int    `json:"total"`
		Offset     *int    `json:"offset"`
	} `json:"page"`
}

func getCounted(t *testing.T, e *gin.Engine, query string) (int, countedPage) {
	t.Helper()
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/counted"+query, nil))
	var p countedPage
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
	}
	return w.Code, p
}

// #1295 D1: the total is the filtered set, and paging to the end in either
// direction reaches exactly that many rows, each once.
func TestCountedListTotalMatchesRowsToTheEnd(t *testing.T) {
	db := countedHarness(t)
	for _, dir := range []pagination.Direction{pagination.OldestFirst, pagination.NewestFirst} {
		e, r := countedServer(t, db, tenantdb.Keyset{Column: "created_at", Direction: dir}, keepOnly)
		seen := map[uuid.UUID]bool{}
		var prev time.Time
		query := "?limit=2"
		for range 10 {
			code, p := getCounted(t, e, query)
			require.Equal(t, http.StatusOK, code)
			require.NotNil(t, p.Page.Total)
			require.Equal(t, 5, *p.Page.Total)
			for _, row := range p.Data {
				require.Equal(t, "keep", row.Kind)
				require.False(t, seen[row.ID], "row repeated")
				seen[row.ID] = true
				if !prev.IsZero() {
					if dir == pagination.OldestFirst {
						require.False(t, row.CreatedAt.Before(prev))
					} else {
						require.False(t, row.CreatedAt.After(prev))
					}
				}
				prev = row.CreatedAt
			}
			if p.Page.NextCursor == nil {
				break
			}
			query = "?limit=2&cursor=" + *p.Page.NextCursor
		}
		require.Len(t, seen, 5, "direction %v", dir)
		routes := r.Declared()
		require.Len(t, routes, 1)
		require.True(t, routes[0].Paginated)
		require.True(t, routes[0].Counted)
	}
}

// A handler that refuses the request answers, and nothing touches the
// database: no count runs for a request already refused.
func TestCountedListHandlerAbortOpensNoTransaction(t *testing.T) {
	db := countedHarness(t)
	scoped := false
	e, _ := countedServer(t, db, tenantdb.Keyset{Column: "created_at"}, func(c *gin.Context) (platform.Scope, bool) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "kind must be keep")
		return func(tx *gorm.DB) *gorm.DB { scoped = true; return tx }, false
	})
	code, _ := getCounted(t, e, "")
	require.Equal(t, http.StatusBadRequest, code)
	require.False(t, scoped, "the scope ran for a refused request")
}

func TestCountedListRefusesOtherDirectionCursor(t *testing.T) {
	db := countedHarness(t)
	e, _ := countedServer(t, db, tenantdb.Keyset{Column: "created_at", Direction: pagination.OldestFirst}, keepOnly)
	newest := pagination.Cursor{TenantID: testTenant, CreatedAt: time.Now().UTC(), ID: uuid.New()}.Encode()
	code, _ := getCounted(t, e, "?cursor="+newest)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestCountedListRefusesCrossTenantCursor(t *testing.T) {
	db := countedHarness(t)
	e, _ := countedServer(t, db, tenantdb.Keyset{Column: "created_at"}, keepOnly)
	other := pagination.Cursor{TenantID: uuid.NewString(), CreatedAt: time.Now().UTC(), ID: uuid.New()}.Encode()
	code, _ := getCounted(t, e, "?cursor="+other)
	require.Equal(t, http.StatusBadRequest, code)
}

// Review Important 1: rows before a page can be removed while someone reads
// it (a queue being worked). The page reports how many rows precede it, from
// the same snapshot, so its range is right rather than computed from a page
// number that has gone stale.
func TestCountedListReportsRowsBeforeThePage(t *testing.T) {
	db := countedHarness(t)
	e, _ := countedServer(t, db, tenantdb.Keyset{Column: "created_at", Direction: pagination.OldestFirst}, keepOnly)
	_, first := getCounted(t, e, "?limit=2")
	require.Equal(t, 0, *first.Page.Offset)
	_, second := getCounted(t, e, "?limit=2&cursor="+*first.Page.NextCursor)
	require.Equal(t, 2, *second.Page.Offset)

	// Remove the first page's rows: the second page now starts at the top.
	require.NoError(t, db.WithTenant(context.Background(), testTenant, func(tx *gorm.DB) error {
		return tx.Where("id IN ?", []uuid.UUID{first.Data[0].ID, first.Data[1].ID}).Delete(&countedRow{}).Error
	}))
	_, again := getCounted(t, e, "?limit=2&cursor="+*first.Page.NextCursor)
	require.Equal(t, 0, *again.Page.Offset)
	require.Equal(t, 3, *again.Page.Total)
	require.Equal(t, second.Data[0].ID, again.Data[0].ID)
}

func guardedServer(t *testing.T, db *tenantdb.DB, h platform.GuardedListHandler[countedRow]) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "alice", TenantID: testTenant})
		c.Set("authz.permissions", authz.NewPermissionSet(listRoutePerm))
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"), alwaysMemberListRoute{})
	platform.GuardedCountedListRoute[countedRow](r, db, "/counted", listRoutePerm, tenantdb.Keyset{Column: "created_at"}, h)
	return e
}

func notFound(c *gin.Context, _ error) { respond.NotFound(c, "thing") }

// #576 D1: a guard that refuses (no outlet grant) is answered by the
// handler's own responder, and nothing is counted.
func TestGuardedListGuardErrorUsesResponder(t *testing.T) {
	db := countedHarness(t)
	scoped := false
	e := guardedServer(t, db, func(*gin.Context) (platform.CountedQuery[countedRow], bool) {
		return platform.CountedQuery[countedRow]{
			Guard: func(*gorm.DB) (platform.Scope, error) {
				return func(tx *gorm.DB) *gorm.DB { scoped = true; return tx }, gorm.ErrRecordNotFound
			},
			Fail: notFound,
		}, true
	})
	code, _ := getCounted(t, e, "")
	require.Equal(t, http.StatusNotFound, code)
	require.False(t, scoped, "the scope ran after the guard refused")
}

// The guard runs inside the snapshot: a row committed after it, before the
// count, is not counted.
func TestGuardedListGuardSeesTheSnapshot(t *testing.T) {
	db := countedHarness(t)
	tid := uuid.MustParse(testTenant)
	e := guardedServer(t, db, func(c *gin.Context) (platform.CountedQuery[countedRow], bool) {
		return platform.CountedQuery[countedRow]{
			Guard: func(tx *gorm.DB) (platform.Scope, error) {
				var one int
				if err := tx.Raw("SELECT count(*) FROM counted_rows").Scan(&one).Error; err != nil {
					return nil, err
				}
				err := db.WithTenant(c.Request.Context(), testTenant, func(other *gorm.DB) error {
					return other.Create(&countedRow{TenantID: tid, Kind: "keep", CreatedAt: time.Now().UTC()}).Error
				})
				return func(tx *gorm.DB) *gorm.DB { return tx.Where("kind = ?", "keep") }, err
			},
			Fail: notFound,
		}, true
	})
	code, p := getCounted(t, e, "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, 5, *p.Page.Total)
	require.Len(t, p.Data, 5)
}

// A projection applies to the rows only: in the shared scope it would break
// the count.
func TestGuardedListRowsProjectionOnlyOnFetch(t *testing.T) {
	db := countedHarness(t)
	e := guardedServer(t, db, func(*gin.Context) (platform.CountedQuery[countedRow], bool) {
		return platform.CountedQuery[countedRow]{
			Guard: func(*gorm.DB) (platform.Scope, error) { return keepScope, nil },
			Rows:  func(tx *gorm.DB) *gorm.DB { return tx.Select("id, tenant_id, 'projected' AS kind, created_at") },
			Fail:  notFound,
		}, true
	})
	code, p := getCounted(t, e, "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, 5, *p.Page.Total)
	require.Len(t, p.Data, 5)
	for _, row := range p.Data {
		require.Equal(t, "projected", row.Kind)
	}
}

func TestGuardedListAfterSeesThePage(t *testing.T) {
	db := countedHarness(t)
	var got int
	fail := false
	e := guardedServer(t, db, func(c *gin.Context) (platform.CountedQuery[countedRow], bool) {
		return platform.CountedQuery[countedRow]{
			Guard: func(*gorm.DB) (platform.Scope, error) { return keepScope, nil },
			After: func(_ *gorm.DB, rows []countedRow) error {
				got = len(rows)
				if fail {
					return gorm.ErrRecordNotFound
				}
				return nil
			},
			Fail: notFound,
		}, true
	})
	code, p := getCounted(t, e, "?limit=2")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, 2, got)
	require.Len(t, p.Data, 2)
	fail = true
	code, _ = getCounted(t, e, "?limit=2")
	require.Equal(t, http.StatusNotFound, code)
}

func keepScope(tx *gorm.DB) *gorm.DB { return tx.Where("kind = ?", "keep") }
