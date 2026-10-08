package pagination_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/pagination"
)

const testTenant = "11111111-1111-1111-1111-111111111111"

// TestCursorRoundTripsFullMicrosecondPrecision is the assertion this
// whole contract rests on. Postgres timestamptz stores microseconds. A
// cursor that round-trips created_at at second or millisecond precision
// skips or repeats every row sharing the truncated timestamp with the
// anchor — and only under fast inserts, which is exactly when a ward is
// busy and nobody is watching.
//
// If someone "simplifies" the encoding to time.RFC3339 (second
// precision) or drops sub-millisecond digits, this is what fails.
func TestCursorRoundTripsFullMicrosecondPrecision(t *testing.T) {
	// 123456 microseconds: every digit matters, and the trailing 6 dies
	// first under a millisecond-precision format.
	at := time.Date(2026, 8, 13, 6, 12, 9, 123456000, time.UTC)
	id := uuid.MustParse("7c9e6679-7425-40de-944b-e07fc1f90ae7")

	got, err := pagination.Decode(pagination.Cursor{
		TenantID: testTenant, CreatedAt: at, ID: id,
	}.Encode(), testTenant)
	require.NoError(t, err)

	require.True(t, got.CreatedAt.Equal(at),
		"cursor lost precision: encoded %s, decoded %s — rows sharing a truncated timestamp will be skipped or repeated",
		at.Format(time.RFC3339Nano), got.CreatedAt.Format(time.RFC3339Nano))
	require.Equal(t, id, got.ID)
	require.Equal(t, testTenant, got.TenantID)
}

func TestDecodeRejectsAnotherTenantsCursor(t *testing.T) {
	const otherTenant = "22222222-2222-2222-2222-222222222222"
	raw := pagination.Cursor{
		TenantID: otherTenant, CreatedAt: time.Now().UTC(), ID: uuid.New(),
	}.Encode()

	_, err := pagination.Decode(raw, testTenant)
	require.ErrorIs(t, err, pagination.ErrInvalidCursor,
		"a cursor issued for another tenant must be refused, not silently used as a position")
}

func TestDecodeRejectsMalformedCursors(t *testing.T) {
	for _, raw := range []string{
		"not-base64!!",
		"eyJub3QiOiJhIGN1cnNvciJ9",       // valid base64, wrong shape
		"",                               // empty
		"eyJ0IjoiIiwiYyI6IiIsImkiOiIifQ", // present but empty fields
	} {
		_, err := pagination.Decode(raw, testTenant)
		require.ErrorIs(t, err, pagination.ErrInvalidCursor, "input %q", raw)
	}
}

// TestDecodeRejectsWellFormedButUnparseableFields closes a coverage gap
// found while implementing this package: every case above trips the
// "fields present but empty" check before reaching the timestamp or UUID
// parse, so those two error branches were never exercised — ignoring
// time.Parse's and uuid.Parse's errors entirely broke nothing in the
// existing suite. All three fields here are non-empty, so decoding must
// fail specifically on the timestamp or the id, not on emptiness.
func TestDecodeRejectsWellFormedButUnparseableFields(t *testing.T) {
	encode := func(tenant, createdAt, id string) string {
		raw, err := json.Marshal(map[string]string{"t": tenant, "c": createdAt, "i": id})
		require.NoError(t, err)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	validID := "7c9e6679-7425-40de-944b-e07fc1f90ae7"

	_, err := pagination.Decode(encode(testTenant, "not-a-timestamp", validID), testTenant)
	require.ErrorIs(t, err, pagination.ErrInvalidCursor, "bad timestamp with otherwise well-formed fields")

	_, err = pagination.Decode(encode(testTenant, "2026-08-13T06:12:09.123456000Z", "not-a-uuid"), testTenant)
	require.ErrorIs(t, err, pagination.ErrInvalidCursor, "bad id with otherwise well-formed fields")
}

// ginContextForQuery builds a *gin.Context whose request has the given
// raw query string (including the leading "?", or empty for none), so
// pagination.Parse can be exercised without a running router.
func ginContextForQuery(t *testing.T, query string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/"+query, nil)
	return c
}

// TestParseLimitDefaultsAndBounds is D5: limits are rejected, never
// clamped. A silent clamp teaches a client that an out-of-range limit
// "worked" while quietly returning fewer rows than asked — the same
// silent-truncation defect #816 exists to end.
func TestParseLimitDefaultsAndBounds(t *testing.T) {
	cases := []struct {
		query string
		want  int
		err   bool
	}{
		{"", pagination.DefaultLimit, false},
		{"?limit=1", 1, false},
		{"?limit=200", 200, false},
		{"?limit=201", 0, true},
		{"?limit=0", 0, true},
		{"?limit=-3", 0, true},
		{"?limit=abc", 0, true},
		{"?limit=1.5", 0, true},
	}
	for _, tc := range cases {
		c := ginContextForQuery(t, tc.query)
		got, err := pagination.Parse(c, testTenant)
		if tc.err {
			require.ErrorIs(t, err, pagination.ErrInvalidLimit, "query %q must be refused, never clamped", tc.query)
			continue
		}
		require.NoError(t, err, tc.query)
		require.Equal(t, tc.want, got.Limit, tc.query)
	}
}

// TestParseRejectsMalformedCursor closes another coverage gap found
// while implementing this package: nothing exercised Parse's own
// ?cursor= handling — swallowing Decode's error there entirely (never
// surfacing 400 for a bad cursor reaching Parse from an actual request)
// broke nothing in the existing suite.
func TestParseRejectsMalformedCursor(t *testing.T) {
	c := ginContextForQuery(t, "?cursor=not-a-valid-cursor")
	_, err := pagination.Parse(c, testTenant)
	require.ErrorIs(t, err, pagination.ErrInvalidCursor)
}
