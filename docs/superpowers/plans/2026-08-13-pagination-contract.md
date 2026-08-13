# Pagination Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace six silently-truncating list endpoints with one cursor-paginated contract that a new endpoint cannot ship without.

**Architecture:** A pure `pkg/pagination` owns cursor encode/decode and limit parsing. A generic `platform.ListRoute[T]` owns the `+1` probe, the trim and the envelope, so a handler physically cannot return an unpaginated result. A shared `tenantdb` keyset helper applies `WHERE (created_at, id) < ($c, $i) ORDER BY created_at DESC, id DESC LIMIT n+1`. The frontend gains a narrow `useApiPagedQuery` and a "Load more" affordance rather than reshaping the hook every panel already uses.

**Tech Stack:** Go 1.26, Gin, GORM, Postgres 16, Next.js 16 / React 19, TanStack Query v5, Vitest, Playwright, testcontainers.

**Spec:** `docs/superpowers/specs/2026-08-13-pagination-contract-design.md`
**Issue:** #816. Branch: `feat/816-pagination-contract`.

## Global Constraints

- `docs/standards/engineering-principles.md` is binding. No minimal/MVP solutions; scope down never quality down; fail closed; enforce structurally (§4: compile error > boot failure > CI failure > convention); **verify the claim, not a proxy**.
- **Every new assertion must be observed failing before it passes.** Break the implementation, watch it go red, restore. Steps saying "prove it can fail" are not optional.
- `make lint-go` clean; `cd backend && go test -count=1 -race ./...` green; `cd backend && ./scripts/coverage-gate.sh` green (70% floor); `pnpm turbo lint type-check test build` green.
- **Envelope (D1):** `{"data": [...], "page": {"next_cursor": string|null, "has_more": bool}}`. `data` is never renamed or removed.
- **Cursor (D2):** `base64url({"t": tenant_id, "c": created_at, "i": id})`. Reject when `cursor.t != principal.TenantID` → 400.
- **Keyset (D3):** `WHERE (created_at, id) < ($c, $i) ORDER BY created_at DESC, id DESC LIMIT $limit + 1`. Never `OFFSET`.
- **Limits (D5):** default 50, max 200. Over-max, zero, negative and non-numeric are **400, never clamped**.
- **Precision:** the cursor must round-trip `created_at` at full Postgres `timestamptz` precision (microseconds). Truncating to seconds or milliseconds skips or repeats rows, and only under fast inserts.
- No schema change and no migration — all six tables already carry `(tenant_id, id, created_at)`.
- Bounded-by-nature routes keep the bare `{"data": [...]}` shape: `GET /v1/iam/roles`, `/v1/iam/me/tenants`, `/v1/iam/me/permissions`.
- Migration IDs are append-only; this plan adds none.

---

## File Structure

**New:**

| File | Responsibility |
|---|---|
| `backend/pkg/pagination/pagination.go` | `Params`, `Cursor`, encode/decode/validate, limit parsing. Pure — no gin, no GORM, no table knowledge |
| `backend/pkg/pagination/pagination_test.go` | Round-trip, precision, tamper, tenant mismatch, limit validation |
| `backend/internal/platform/listroute.go` | `ListRoute[T]` — parse, call handler, `+1` probe, trim, envelope |
| `backend/internal/platform/listroute_test.go` | Probe/trim/envelope behaviour with a fake handler |
| `backend/pkg/tenantdb/keyset.go` | `ApplyKeyset(tx *gorm.DB, p pagination.Params) *gorm.DB` |
| `backend/pkg/tenantdb/keyset_test.go` | Exact-once under concurrent inserts, same-microsecond rows, deleted anchor |
| `packages/api/src/paged.ts` | `useApiPagedQuery` — accumulates pages, exposes `loadMore`/`hasMore` |
| `packages/api/src/paged.test.tsx` | Appends, stops at the end, resets on key change |
| `packages/ui/src/load-more.tsx` | The shared "Load more" button |
| `e2e/tests/pagination.spec.ts` | A ward past one page reads to completion |

**Modified:**

| File | Change |
|---|---|
| `backend/internal/platform/router.go` | `DeclaredRoute` gains `Paginated bool`; `handle` records it |
| `backend/internal/modules/medicore/visits.go`, `module.go` | `list` → repository shape; route → `ListRoute` |
| `backend/internal/modules/pharmacy/dispenses.go`, `medications.go`, `module.go` | same, two endpoints |
| `backend/internal/modules/lab/orders.go`, `module.go` | same |
| `backend/internal/modules/reference/pings.go`, `module.go` | same |
| `backend/internal/modules/iam/module.go` | members list → `ListRoute` (500 → contract) |
| `backend/internal/archtest/arch_test.go` | Unpaginated-collection arch test + allowlist |
| `backend/scripts/new-module.sh` | Template emits `ListRoute` |
| `packages/api/src/index.ts` | Export `useApiPagedQuery` |
| `apps/medicore/components/visit-panel.tsx`, `ping-panel.tsx` | Load more |
| `apps/pharmacy/components/dispense-list.tsx`, `medications-panel.tsx` | Load more |
| `apps/lab/components/order-list.tsx` | Load more |
| `docs/standards/backend.md`, `docs/standards/frontend.md` | The contract as a binding rule |

---

## Task 1: `pkg/pagination` — cursor and limit

**Files:**
- Create: `backend/pkg/pagination/pagination.go`, `backend/pkg/pagination/pagination_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `pagination.Params{Limit int; After *Cursor}`; `pagination.Cursor{TenantID string; CreatedAt time.Time; ID uuid.UUID}`; `func Parse(c *gin.Context, tenantID string) (Params, error)`; `func (Cursor) Encode() string`; `func Decode(raw, tenantID string) (Cursor, error)`; `var ErrInvalidCursor, ErrInvalidLimit error`; `const DefaultLimit = 50`, `MaxLimit = 200`.

Note: `Parse` takes `*gin.Context` only to read query values. That is the one gin dependency; everything else in the package is pure so the precision and tamper tests need no HTTP.

- [ ] **Step 1: Write the failing precision test — the one that matters most**

Create `backend/pkg/pagination/pagination_test.go`:

```go
package pagination_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/pagination"
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
		"eyJub3QiOiJhIGN1cnNvciJ9",              // valid base64, wrong shape
		"",                                       // empty
		"eyJ0IjoiIiwiYyI6IiIsImkiOiIifQ",        // present but empty fields
	} {
		_, err := pagination.Decode(raw, testTenant)
		require.ErrorIs(t, err, pagination.ErrInvalidCursor, "input %q", raw)
	}
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
cd backend && go test ./pkg/pagination/ -run TestCursor -v
```

Expected: FAIL to compile — package does not exist.

- [ ] **Step 3: Implement `backend/pkg/pagination/pagination.go`**

```go
// Package pagination owns the cursor and limit halves of the HMS list
// contract. It is deliberately pure: no GORM, no knowledge of any table,
// and gin only to read query values — so the precision and tamper
// properties can be tested without a database or an HTTP round trip.
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// DefaultLimit is what a caller gets for omitting ?limit. A ward
	// list or dispense queue reads comfortably at this size.
	DefaultLimit = 50
	// MaxLimit bounds the worst single query on a shared db-f1-micro.
	// Exceeding it is an error, never a silent clamp: clamping teaches a
	// client that limit=5000 worked while quietly returning 200, and the
	// client then believes it has everything — the same silent-truncation
	// defect this contract exists to end.
	MaxLimit = 200
)

var (
	ErrInvalidCursor = errors.New("pagination: invalid cursor")
	ErrInvalidLimit  = errors.New("pagination: invalid limit")
)

// Cursor is a position in a keyset-ordered collection.
//
// TenantID is carried so a cursor from one tenant used against another
// is refused rather than silently applied as a position. It is not a
// security boundary — forced RLS already bounds every query to the
// caller's own tenant, so a hand-edited cursor can only reposition a
// caller within data they can already read. It exists to turn a
// confusing wrong-position result into a clear error.
type Cursor struct {
	TenantID  string
	CreatedAt time.Time
	ID        uuid.UUID
}

// wireCursor is the JSON shape. Field names are single letters because
// the encoded cursor appears in every next-page URL.
//
// CreatedAt is carried as RFC3339 with nanosecond precision.
// time.RFC3339Nano is NOT used: it strips trailing zeros, so a timestamp
// ending in a zero microsecond digit encodes differently from one that
// does not, and a byte-comparison of two equivalent cursors disagrees.
// The fixed ".000000000" form keeps the encoding canonical, and Postgres
// microsecond values survive it exactly.
type wireCursor struct {
	T string `json:"t"`
	C string `json:"c"`
	I string `json:"i"`
}

const cursorTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

func (c Cursor) Encode() string {
	raw, err := json.Marshal(wireCursor{
		T: c.TenantID,
		C: c.CreatedAt.UTC().Format(cursorTimeFormat),
		I: c.ID.String(),
	})
	if err != nil {
		// wireCursor is three strings; marshalling cannot fail. Returning
		// "" rather than panicking means a caller gets "no next page"
		// instead of a crashed request in the impossible case.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Decode parses raw and refuses it unless it belongs to tenantID.
//
// Every failure is the same error: a caller who supplied a broken cursor
// does not need to know which part broke, and distinguishing "malformed"
// from "wrong tenant" in the response would tell a prober which tenant a
// cursor belongs to.
func Decode(raw, tenantID string) (Cursor, error) {
	if raw == "" {
		return Cursor{}, fmt.Errorf("%w: empty", ErrInvalidCursor)
	}
	blob, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: not base64url", ErrInvalidCursor)
	}
	var w wireCursor
	if err := json.Unmarshal(blob, &w); err != nil {
		return Cursor{}, fmt.Errorf("%w: not a cursor", ErrInvalidCursor)
	}
	if w.T == "" || w.C == "" || w.I == "" {
		return Cursor{}, fmt.Errorf("%w: incomplete", ErrInvalidCursor)
	}
	if w.T != tenantID {
		return Cursor{}, fmt.Errorf("%w: belongs to another tenant", ErrInvalidCursor)
	}
	at, err := time.Parse(cursorTimeFormat, w.C)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: bad timestamp", ErrInvalidCursor)
	}
	id, err := uuid.Parse(w.I)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: bad id", ErrInvalidCursor)
	}
	return Cursor{TenantID: w.T, CreatedAt: at, ID: id}, nil
}

// Params is one page request: how many rows, and where to start.
type Params struct {
	Limit int
	// After is nil on the first page.
	After *Cursor
}

// Parse reads ?limit and ?cursor.
//
// An out-of-range or unparseable limit is an error rather than a clamp,
// for the reason on MaxLimit.
func Parse(c *gin.Context, tenantID string) (Params, error) {
	p := Params{Limit: DefaultLimit}

	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return Params{}, fmt.Errorf("%w: limit must be a number between 1 and %d", ErrInvalidLimit, MaxLimit)
		}
		if n < 1 || n > MaxLimit {
			return Params{}, fmt.Errorf("%w: limit must be between 1 and %d, got %d", ErrInvalidLimit, MaxLimit, n)
		}
		p.Limit = n
	}

	if raw := c.Query("cursor"); raw != "" {
		cur, err := Decode(raw, tenantID)
		if err != nil {
			return Params{}, err
		}
		p.After = &cur
	}
	return p, nil
}
```

- [ ] **Step 4: Run and confirm the tests pass**

```bash
cd backend && go test -race ./pkg/pagination/
```

Expected: PASS.

- [ ] **Step 5: Prove the precision test can fail — do not skip this**

Change `cursorTimeFormat` to `time.RFC3339` (second precision), run:

```bash
cd backend && go test ./pkg/pagination/ -run TestCursorRoundTripsFullMicrosecondPrecision -v
```

Expected: FAIL with "cursor lost precision: encoded 2026-08-13T06:12:09.123456Z, decoded 2026-08-13T06:12:09Z". Restore.

Then change it to `"2006-01-02T15:04:05.000Z07:00"` (millisecond) and confirm the same test fails again — millisecond truncation is the more tempting mistake and must also be caught. Restore.

- [ ] **Step 6: Write the failing limit tests**

```go
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
```

Add `ginContextForQuery(t *testing.T, query string) *gin.Context` building a `gin.Context` from `httptest.NewRequest(http.MethodGet, "/"+query, nil)` via `gin.CreateTestContext`.

- [ ] **Step 7: Run, then prove clamping would be caught**

```bash
cd backend && go test -race ./pkg/pagination/
```

Expected: PASS. Then change the `n > MaxLimit` branch to `p.Limit = MaxLimit` (silent clamp) and confirm `TestParseLimitDefaultsAndBounds` fails on `?limit=201`. Restore.

- [ ] **Step 8: Commit**

```bash
cd backend && go vet ./... && cd .. && make lint-go
git add backend/pkg/pagination/
git commit -m "feat: add pagination cursors that round-trip full timestamp precision and refuse another tenant's position (#816)"
```

---

## Task 2: the keyset helper

**Files:**
- Create: `backend/pkg/tenantdb/keyset.go`, `backend/pkg/tenantdb/keyset_test.go`

**Interfaces:**
- Consumes: `pagination.Params` (Task 1).
- Produces: `func ApplyKeyset(tx *gorm.DB, p pagination.Params) *gorm.DB`.

- [ ] **Step 1: Write the failing exact-once test**

This is T1 from the spec — the property `OFFSET` cannot provide. Create `backend/pkg/tenantdb/keyset_test.go`:

```go
// TestKeysetReturnsEveryRowExactlyOnceWhileRowsAreInserted is why this
// is a keyset and not an offset. A ward's list changes while it is read:
// with OFFSET, one insert before page 2 pushes a row from page 1 into
// page 2 (the client sees it twice) and one delete pulls a row up (the
// client never sees it). Silently dropping a patient from a list is the
// defect #816 exists to fix, reintroduced through the back door.
func TestKeysetReturnsEveryRowExactlyOnceWhileRowsAreInserted(t *testing.T) {
	db, ctx, tenantID := keysetHarness(t)
	seedRows(t, db, ctx, tenantID, 25) // ids r00..r24

	seen := map[string]int{}
	var after *pagination.Cursor
	for page := 0; page < 20; page++ {
		rows := fetchPage(t, db, ctx, tenantID, pagination.Params{Limit: 5, After: after})
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			seen[r.Name]++
		}
		// One new row per page, landing at the TOP of the DESC ordering —
		// the position OFFSET paging is most wrong about.
		insertRow(t, db, ctx, tenantID, fmt.Sprintf("inserted-%d", page))
		last := rows[len(rows)-1]
		after = &pagination.Cursor{TenantID: tenantID, CreatedAt: last.CreatedAt, ID: last.ID}
	}

	for i := range 25 {
		name := fmt.Sprintf("r%02d", i)
		require.Equal(t, 1, seen[name],
			"row %s was seen %d times; every pre-existing row must appear exactly once across pages", name, seen[name])
	}
}
```

Provide `keysetHarness` (a `testinfra.StartPostgres` + a scratch table with `tenant_id uuid, id uuid, name text, created_at timestamptz` under forced RLS, matching the boilerplate every module migration uses), `seedRows`, `insertRow`, and `fetchPage` which calls `ApplyKeyset` inside `db.WithTenant`.

- [ ] **Step 2: Run and confirm failure**

```bash
cd backend && go test ./pkg/tenantdb/ -run TestKeysetReturnsEveryRow -v
```

Expected: FAIL to compile — `ApplyKeyset` undefined.

- [ ] **Step 3: Implement `backend/pkg/tenantdb/keyset.go`**

```go
package tenantdb

import (
	"gorm.io/gorm"

	"github.com/tesserix/hms/pkg/pagination"
)

// ApplyKeyset adds the ordering, the position predicate and the +1 probe
// to a query.
//
// The +1 is deliberate: fetching one more row than asked answers
// "is there another page" without a second query. A COUNT would be both
// slower and wrong — it races inserts between the two queries.
// platform.ListRoute owns the trim, so a handler never sees the extra row.
//
// The predicate is a row-value comparison, (created_at, id) < (?, ?),
// not two ANDed comparisons: created_at alone is not unique, and id is a
// required tiebreaker rather than a refinement. Postgres evaluates the
// row-value form as a single lexicographic comparison, which is also
// what lets it use a (created_at DESC, id DESC) index.
//
// It survives its own anchor being deleted: the comparison is on values,
// not on the row still existing, so a cursor whose row was deleted
// between pages still selects exactly the rows that follow that
// position.
func ApplyKeyset(tx *gorm.DB, p pagination.Params) *gorm.DB {
	q := tx.Order("created_at DESC, id DESC").Limit(p.Limit + 1)
	if p.After != nil {
		q = q.Where("(created_at, id) < (?, ?)", p.After.CreatedAt, p.After.ID)
	}
	return q
}
```

- [ ] **Step 4: Run and confirm it passes**

```bash
cd backend && go test -race ./pkg/tenantdb/ -run TestKeysetReturnsEveryRow -v
```

Expected: PASS.

- [ ] **Step 5: Prove it can fail — the OFFSET substitution**

Replace the `Where` clause with `q = q.Offset(p.Limit * pageNumber)` semantics — concretely, change `ApplyKeyset` to ignore `p.After` and have `fetchPage` pass an increasing `Offset`. Run the test and confirm it fails with a row seen twice. Restore.

This is the mutation that matters: it proves the test discriminates between the keyset and the plausible-looking alternative.

- [ ] **Step 6: Write the same-microsecond test (spec T2)**

```go
// TestKeysetSeparatesRowsSharingATimestamp pairs with the cursor
// precision test in pkg/pagination: that one proves the cursor carries
// microseconds, this one proves the query uses them. Both must hold —
// a precise cursor fed into a comparison that ignores id, or an id
// tiebreaker fed a truncated timestamp, each lose rows.
func TestKeysetSeparatesRowsSharingATimestamp(t *testing.T) {
	db, ctx, tenantID := keysetHarness(t)
	at := time.Date(2026, 8, 13, 6, 12, 9, 123456000, time.UTC)
	// Five rows, identical created_at, distinct ids.
	for i := range 5 {
		insertRowAt(t, db, ctx, tenantID, fmt.Sprintf("same-%d", i), at)
	}

	seen := map[string]int{}
	var after *pagination.Cursor
	for range 10 {
		rows := fetchPage(t, db, ctx, tenantID, pagination.Params{Limit: 2, After: after})
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			seen[r.Name]++
		}
		last := rows[len(rows)-1]
		after = &pagination.Cursor{TenantID: tenantID, CreatedAt: last.CreatedAt, ID: last.ID}
	}

	require.Len(t, seen, 5, "all five rows sharing a timestamp must be reachable")
	for name, n := range seen {
		require.Equal(t, 1, n, "row %s seen %d times", name, n)
	}
}
```

- [ ] **Step 7: Run, then prove the id tiebreaker is load-bearing**

```bash
cd backend && go test -race ./pkg/tenantdb/ -run TestKeysetSeparatesRows -v
```

Expected: PASS. Then change the predicate to `created_at < ?` (dropping the id tiebreaker) and confirm this test fails — four of the five rows vanish. Restore.

- [ ] **Step 8: Write the deleted-anchor test (spec T3)**

```go
// TestKeysetSurvivesItsAnchorBeingDeleted proves the predicate is a value
// comparison, not a row lookup. A reviewer will ask; this answers.
func TestKeysetSurvivesItsAnchorBeingDeleted(t *testing.T) {
	db, ctx, tenantID := keysetHarness(t)
	seedRows(t, db, ctx, tenantID, 6)

	first := fetchPage(t, db, ctx, tenantID, pagination.Params{Limit: 3})
	require.Len(t, first, 4, "the +1 probe returns limit+1 rows when more exist")
	anchor := first[2] // the last row the caller would actually be shown

	deleteRow(t, db, ctx, tenantID, anchor.Name)

	rest := fetchPage(t, db, ctx, tenantID, pagination.Params{
		Limit: 10,
		After: &pagination.Cursor{TenantID: tenantID, CreatedAt: anchor.CreatedAt, ID: anchor.ID},
	})
	names := make([]string, 0, len(rest))
	for _, r := range rest {
		names = append(names, r.Name)
	}
	require.Equal(t, []string{"r02", "r01", "r00"}, names,
		"paging must continue from the deleted anchor's position, not fail or restart")
}
```

- [ ] **Step 9: Run the package and commit**

```bash
cd backend && go test -race ./pkg/tenantdb/... && cd .. && make lint-go
git add backend/pkg/tenantdb/keyset.go backend/pkg/tenantdb/keyset_test.go
git commit -m "feat: add a keyset helper that pages by value so inserts and deletes cannot skip or repeat a row (#816)"
```

---

## Task 3: `platform.ListRoute` — the compile-enforced route

**Files:**
- Create: `backend/internal/platform/listroute.go`, `backend/internal/platform/listroute_test.go`
- Modify: `backend/internal/platform/router.go`

**Interfaces:**
- Consumes: `pagination.Params`, `pagination.Cursor`, `pagination.Parse` (Task 1).
- Produces:
  - `type ListHandler[T Keyed] func(c *gin.Context, p pagination.Params) ([]T, error)`
  - `func ListRoute[T Keyed](r *Router, path string, perm authz.Permission, h ListHandler[T])`
  - `type Keyed interface { PageKey() (time.Time, uuid.UUID) }`
  - `DeclaredRoute` gains `Paginated bool`.

`ListRoute` must build the next cursor, which means reading `created_at` and `id` off an arbitrary `T`. A `Keyed` constraint is used rather than reflection: it is a compile error for a row type to be pageable without saying how, and it keeps the platform layer free of struct-tag inspection.

- [ ] **Step 1: Add `Paginated` to `DeclaredRoute`**

In `backend/internal/platform/router.go`:

```go
type DeclaredRoute struct {
	Method     string
	Path       string
	Permission authz.Permission
	// Paginated is true for routes registered through ListRoute. The
	// arch test uses it to fail any collection GET registered the old
	// way — the one hole ListRoute's type signature cannot close.
	Paginated bool
}
```

Change `handle` to take the flag, keeping the existing verb methods passing `false`:

```go
func (r *Router) handle(method, path string, perm authz.Permission, paginated bool, h []gin.HandlerFunc) {
	*r.declared = append(*r.declared, DeclaredRoute{
		Method: method, Path: r.group.BasePath() + path, Permission: perm, Paginated: paginated,
	})
	// Everything below this line in the existing handle() body — the
	// RequireMembership insertion, authz.Require, appending h, and
	// r.group.Handle — stays exactly as it is. Only the DeclaredRoute
	// literal above and the signature change.
}
```

Update the five verb methods to pass `false`.

- [ ] **Step 2: Write the failing envelope test**

Create `backend/internal/platform/listroute_test.go`:

```go
type fakeRow struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func (r fakeRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

// TestListRouteTrimsTheProbeRowAndReportsHasMore pins the contract's
// central mechanic: the handler returns limit+1 rows and the caller must
// never see the extra one, while has_more must be true because of it.
func TestListRouteTrimsTheProbeRowAndReportsHasMore(t *testing.T) {
	rows := makeRows(t, 4) // handler will return all 4 for limit=3
	w := runListRoute(t, "?limit=3", func(_ *gin.Context, p pagination.Params) ([]fakeRow, error) {
		require.Equal(t, 3, p.Limit)
		return rows, nil
	})

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data []fakeRow `json:"data"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	require.Len(t, body.Data, 3, "the probe row must be trimmed before the client sees it")
	require.Equal(t, rows[2].ID, body.Data[2].ID, "the trim must drop the LAST row, not an arbitrary one")
	require.True(t, body.Page.HasMore)
	require.NotNil(t, body.Page.NextCursor)

	// The cursor must point at the last SHOWN row, not the probe row —
	// pointing at the probe skips it on the next page.
	cur, err := pagination.Decode(*body.Page.NextCursor, testTenant)
	require.NoError(t, err)
	require.Equal(t, rows[2].ID, cur.ID, "next_cursor must anchor on the last row the client was shown")
}

func TestListRouteLastPageHasNoCursor(t *testing.T) {
	rows := makeRows(t, 2)
	w := runListRoute(t, "?limit=3", func(_ *gin.Context, _ pagination.Params) ([]fakeRow, error) {
		return rows, nil
	})

	var body struct {
		Data []fakeRow `json:"data"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 2)
	require.False(t, body.Page.HasMore)
	require.Nil(t, body.Page.NextCursor)
}

// TestListRouteExactBoundary is the case a naive implementation gets
// wrong: exactly limit rows exist, so the probe finds nothing extra and
// this IS the last page — even though the page is full.
func TestListRouteExactBoundary(t *testing.T) {
	rows := makeRows(t, 3)
	w := runListRoute(t, "?limit=3", func(_ *gin.Context, _ pagination.Params) ([]fakeRow, error) {
		return rows, nil
	})

	var body struct {
		Data []fakeRow `json:"data"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 3)
	require.False(t, body.Page.HasMore, "a full page with nothing after it is still the last page")
	require.Nil(t, body.Page.NextCursor)
}

func TestListRouteEmptyCollectionIsAnArrayNotNull(t *testing.T) {
	w := runListRoute(t, "", func(_ *gin.Context, _ pagination.Params) ([]fakeRow, error) {
		return nil, nil
	})
	require.Contains(t, w.Body.String(), `"data":[]`,
		"an empty collection must marshal to [] — panels branch on the array, and null crashes them")
}

func TestListRouteRejectsBadLimitAndCursor(t *testing.T) {
	for _, q := range []string{"?limit=201", "?limit=0", "?limit=abc", "?cursor=not-a-cursor"} {
		w := runListRoute(t, q, func(_ *gin.Context, _ pagination.Params) ([]fakeRow, error) {
			t.Fatalf("handler must not run for invalid input %q", q)
			return nil, nil
		})
		require.Equal(t, http.StatusBadRequest, w.Code, q)
		require.Contains(t, w.Body.String(), "invalid_request", q)
	}
}
```

`runListRoute` builds a `Router` with a stub membership checker and a principal on the context (follow `router_test.go`'s existing helpers), registers the handler through `ListRoute`, and issues the request. `makeRows(t, n)` returns `n` rows with strictly decreasing `CreatedAt`.

- [ ] **Step 3: Run and confirm failure**

```bash
cd backend && go test ./internal/platform/ -run TestListRoute -v
```

Expected: FAIL to compile — `ListRoute` undefined.

- [ ] **Step 4: Implement `backend/internal/platform/listroute.go`**

```go
package platform

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/pagination"
)

// Keyed is what a row must provide to be pageable: the two values the
// keyset orders by.
//
// A constraint rather than reflection over struct tags, so a row type
// that cannot say where it sits in the ordering is a compile error at
// the ListRoute call site rather than a runtime surprise on an empty
// cursor.
type Keyed interface {
	PageKey() (time.Time, uuid.UUID)
}

// ListHandler returns one page of rows. It receives the parsed params
// and returns up to Limit+1 rows — the extra row is the probe, and
// ListRoute trims it.
type ListHandler[T Keyed] func(c *gin.Context, p pagination.Params) ([]T, error)

// ListRoute registers a paginated collection endpoint.
//
// The platform layer owns everything a handler could forget: parsing and
// validating limit and cursor, trimming the probe row, deciding
// has_more, building the next cursor, and shaping the envelope. A
// handler cannot return a bare slice, cannot invent its own limit, and
// cannot report has_more from a stale count — none of those are
// expressible through this signature.
//
// This is the same guarantee *Router's unexported gin group gives for
// permissions (docs/standards/engineering-principles.md §4: prefer
// "impossible to express" over "documented convention").
//
// A package-level function rather than a method because Go methods
// cannot take type parameters.
func ListRoute[T Keyed](r *Router, path string, perm authz.Permission, h ListHandler[T]) {
	r.handle("GET", path, perm, true, []gin.HandlerFunc{func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		params, err := pagination.Parse(c, p.TenantID)
		if err != nil {
			// Both invalid limits and invalid cursors are client errors.
			// The handler never runs — a bad page request must not reach
			// the database.
			respond.BadRequest(c, err)
			return
		}

		rows, err := h(c, params)
		if err != nil {
			respond.InternalErr(c, err, "could not list "+path)
			return
		}

		// The probe: the handler fetched Limit+1. More than Limit rows
		// means another page exists; the extra row is trimmed so the
		// client is never shown a row it did not ask for and the cursor
		// anchors on the last row it WAS shown. Anchoring on the probe
		// row instead would skip it on the next page.
		hasMore := len(rows) > params.Limit
		if hasMore {
			rows = rows[:params.Limit]
		}

		var next *string
		if hasMore && len(rows) > 0 {
			at, id := rows[len(rows)-1].PageKey()
			encoded := pagination.Cursor{TenantID: p.TenantID, CreatedAt: at, ID: id}.Encode()
			next = &encoded
		}

		// A nil slice marshals to null; panels branch on an array. Make
		// it empty rather than letting the zero value through.
		if rows == nil {
			rows = []T{}
		}
		respond.OK(c, gin.H{
			"data": rows,
			"page": gin.H{"next_cursor": next, "has_more": hasMore},
		})
	}})
}
```

Import only what the file uses: `time`, `uuid`, `gin`, `respond`, `authn`, `authz`, `pagination`. No `errors`.

- [ ] **Step 5: Run and confirm the tests pass**

```bash
cd backend && go test -race ./internal/platform/ -run TestListRoute -v
```

Expected: all five PASS.

- [ ] **Step 6: Prove three assertions can fail**

1. Change `rows = rows[:params.Limit]` to not trim; confirm `TestListRouteTrimsTheProbeRowAndReportsHasMore` fails on the length assertion. Restore.
2. Change the cursor to anchor on `rows[len(rows)-1]` **before** the trim (i.e. the probe row); confirm the same test fails on "next_cursor must anchor on the last row the client was shown". Restore.
3. Change `hasMore := len(rows) > params.Limit` to `>=`; confirm `TestListRouteExactBoundary` fails. Restore.

Assertion 2 is the important one — anchoring on the probe row silently skips exactly one row per page, which is the defect this contract exists to prevent, in miniature.

- [ ] **Step 7: Run the whole package and commit**

```bash
cd backend && go test -race ./internal/platform/... && cd .. && make lint-go
git add backend/internal/platform/
git commit -m "feat: add ListRoute so a collection handler cannot return an unpaginated result (#816)"
```

---

## Task 4: migrate the six endpoints

**Files:**
- Modify: `backend/internal/modules/medicore/visits.go` + `module.go`; `backend/internal/modules/pharmacy/dispenses.go`, `medications.go` + `module.go`; `backend/internal/modules/lab/orders.go` + `module.go`; `backend/internal/modules/reference/pings.go` + `module.go`; `backend/internal/modules/iam/module.go`
- Modify: the corresponding `*_test.go` in each module

**Interfaces:**
- Consumes: `platform.ListRoute`, `platform.Keyed`, `tenantdb.ApplyKeyset`, `pagination.Params`.
- Produces: nothing new. Six endpoints change shape.

Each row type gains a `PageKey` method; each `list` handler becomes a `ListHandler`; each route registration moves from `g.GET` to `platform.ListRoute`.

- [ ] **Step 1: Write the failing integration test for one endpoint first**

Do `medicore` first and completely, so the pattern is proven before it is repeated five times. In `backend/internal/modules/medicore/module_test.go`:

```go
func TestVisitsArePaginated(t *testing.T) {
	r, db, _, ctx := harness(t) // the module's existing harness helper
	seedVisits(t, db, ctx, 7)

	w := testutil.Do(r, http.MethodGet, "/v1/medicore/visits?limit=3", "tok-doctor", "")
	require.Equal(t, http.StatusOK, w.Code)

	var page1 struct {
		Data []struct {
			ID          string `json:"id"`
			PatientName string `json:"patient_name"`
		} `json:"data"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page1))
	require.Len(t, page1.Data, 3)
	require.True(t, page1.Page.HasMore)
	require.NotNil(t, page1.Page.NextCursor)

	// Walk to exhaustion and assert every seeded visit appears once.
	seen := map[string]int{}
	for _, v := range page1.Data {
		seen[v.PatientName]++
	}
	cursor := *page1.Page.NextCursor
	for range 10 {
		w := testutil.Do(r, http.MethodGet, "/v1/medicore/visits?limit=3&cursor="+url.QueryEscape(cursor), "tok-doctor", "")
		require.Equal(t, http.StatusOK, w.Code)
		var next struct {
			Data []struct {
				PatientName string `json:"patient_name"`
			} `json:"data"`
			Page struct {
				NextCursor *string `json:"next_cursor"`
				HasMore    bool    `json:"has_more"`
			} `json:"page"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &next))
		for _, v := range next.Data {
			seen[v.PatientName]++
		}
		if !next.Page.HasMore {
			require.Nil(t, next.Page.NextCursor)
			break
		}
		cursor = *next.Page.NextCursor
	}

	require.Len(t, seen, 7)
	for name, n := range seen {
		require.Equal(t, 1, n, "visit %s appeared %d times across pages", name, n)
	}
}

func TestVisitsRejectAnotherTenantsCursor(t *testing.T) {
	r, _, _, _ := harness(t)
	foreign := pagination.Cursor{
		TenantID:  "22222222-2222-2222-2222-222222222222",
		CreatedAt: time.Now().UTC(),
		ID:        uuid.New(),
	}.Encode()

	w := testutil.Do(r, http.MethodGet, "/v1/medicore/visits?cursor="+url.QueryEscape(foreign), "tok-doctor", "")

	require.Equal(t, http.StatusBadRequest, w.Code,
		"a cursor issued for another tenant must be refused, not applied as a position")
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
cd backend && go test ./internal/modules/medicore/ -run TestVisits -v
```

Expected: FAIL — the response has no `page` object, so `HasMore` is false and `NextCursor` nil.

- [ ] **Step 3: Migrate medicore**

In `visits.go`, add the key method beside the model:

```go
// PageKey is the keyset position of this row, satisfying platform.Keyed.
func (v visit) PageKey() (time.Time, uuid.UUID) { return v.CreatedAt, v.ID }
```

Replace `list` with a `ListHandler`:

```go
// list returns one page of this tenant's visits, newest first.
//
// It returns whatever ApplyKeyset yields — up to Limit+1 rows — and does
// not trim, does not decide has_more and does not build a cursor.
// platform.ListRoute owns all three, so they cannot be got wrong per
// endpoint.
func (h *visitHandlers) list(c *gin.Context, p pagination.Params) ([]visit, error) {
	principal, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []visit
	err := h.db.WithTenant(c.Request.Context(), principal.TenantID, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, p).Find(&rows).Error
	})
	return rows, err
}
```

In `module.go`, replace the route:

```go
	platform.ListRoute(g, "/visits", PermVisitRead, visits.list)
```

- [ ] **Step 4: Run and confirm both tests pass**

```bash
cd backend && go test -race ./internal/modules/medicore/ -v
```

Expected: PASS, including the module's pre-existing tests.

- [ ] **Step 5: Prove the exact-once assertion can fail**

Change `ApplyKeyset` usage in `list` to ignore the cursor (call `tx.Order("created_at DESC").Limit(p.Limit + 1)` directly). Confirm `TestVisitsArePaginated` fails with rows appearing more than once. Restore.

- [ ] **Step 6: Repeat for the remaining five**

Apply the identical pattern to:

| Module | Row type | Handler | Route | Permission |
|---|---|---|---|---|
| `pharmacy` | `dispense` | `dispenses.list` | `/dispenses` | `PermDispenseRead` |
| `pharmacy` | `medication` | `meds.list` | `/medications` | `PermMedicationRead` |
| `lab` | `order` | `orders.list` | `/orders` | `PermOrderRead` |
| `reference` | `ping` | `pings.list` | `/pings` | `authz.Public` |
| `iam` | `member` | `members.list` | `/members` | `PermMemberManage` |

Each gets: a `PageKey` method, the handler signature change, the `ListRoute` registration, and a paginated integration test mirroring Step 1's shape. **Do not skip the per-module test** — the `iam` one matters most, because that endpoint governs who has access to a hospital and it is dropping from a 500-row limit.

Leave `GET /pings/:id` on plain `g.GET` — it returns one object.

- [ ] **Step 7: Full backend suite**

```bash
cd backend && go test -count=1 -race ./... && ./scripts/coverage-gate.sh && cd .. && make lint-go
```

Expected: green. The frontend is now broken against these endpoints — it still reads `{data}` which still works, but shows 50 rows instead of 100. Task 6 fixes the affordance.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/modules/
git commit -m "feat: page all six collection endpoints through ListRoute (#816)"
```

---

## Task 5: close the hole — arch test and generator

**Files:**
- Modify: `backend/internal/archtest/arch_test.go`, `backend/scripts/new-module.sh`
- Modify: `docs/standards/backend.md`

**Interfaces:**
- Consumes: `platform.DeclaredRoute.Paginated` (Task 3).

- [ ] **Step 1: Write the failing arch test**

```go
// unpaginatedGETAllowlist is every GET route permitted to return a
// collection-shaped body without a cursor. Two kinds qualify:
//
//   - single-item reads, which return one object
//   - collections bounded by construction rather than by tenant data:
//     a fixed role set, a person's memberships, a resolved permission set
//
// Anything else is a collection that will truncate silently once a
// tenant has enough rows, which is #816. Adding an entry here is a
// decision a reviewer sees.
var unpaginatedGETAllowlist = map[string]string{
	"GET /reference/pings/:id":  "single item, not a collection",
	"GET /iam/roles":            "bounded: the fixed system role set",
	"GET /iam/me/tenants":       "bounded: one person's memberships",
	"GET /iam/me/permissions":   "bounded: one resolved permission set",
}

func TestEveryCollectionGETIsPaginated(t *testing.T) {
	var offenders []string
	for _, rt := range allDeclaredRoutes(t) {
		if rt.Method != http.MethodGet || rt.Paginated {
			continue
		}
		key := rt.Method + " " + rt.Path
		if _, allowed := unpaginatedGETAllowlist[key]; allowed {
			continue
		}
		offenders = append(offenders, key)
	}
	require.Empty(t, offenders,
		"these GET routes return a collection without a cursor and will truncate silently (#816): register them through platform.ListRoute, or add them to unpaginatedGETAllowlist with a reason")
}
```

`allDeclaredRoutes(t)` registers every module against a router and returns `[]platform.DeclaredRoute` — extend the existing `routesDeclaring` helper rather than duplicating its route-walking.

- [ ] **Step 2: Run it**

```bash
cd backend && go test ./internal/archtest/ -run TestEveryCollectionGETIsPaginated -v
```

Expected: PASS, since Task 4 migrated all six. If it fails, a migration was missed — that is the test doing its job.

- [ ] **Step 3: Prove it can fail**

Temporarily revert `medicore`'s route to `g.GET("/visits", PermVisitRead, ...)`. Confirm the test fails naming `GET /medicore/visits`. Restore.

- [ ] **Step 4: Update the generator**

In `backend/scripts/new-module.sh`, the template's list route becomes:

```go
	platform.ListRoute(g, "/items", PermItemRead, items.list)
```

and the template's `list` handler becomes the `ListHandler` shape, with the model gaining:

```go
// PageKey is the keyset position of this row, satisfying platform.Keyed.
func (i item) PageKey() (time.Time, uuid.UUID) { return i.CreatedAt, i.ID }
```

Add `pagination` and `tenantdb` to the template's imports.

- [ ] **Step 5: Prove the generator output is correct**

```bash
cd backend && ./scripts/new-module.sh scratchpg && go build ./... && go test ./internal/modules/scratchpg/
```

Expected: builds and its generated test passes. Then register it in `cmd/api/main.go` and `internal/archtest/arch_test.go`'s `allModules()` temporarily and run the arch test — it must pass without an allowlist entry, proving a new module is paginated by default. Remove the registration and `rm -rf internal/modules/scratchpg` afterwards.

- [ ] **Step 6: Document the rule**

Add to `docs/standards/backend.md`, in the routes section:

```markdown
**Collection endpoints are registered through `platform.ListRoute`, never
`g.GET`.** It owns the limit clamp, the keyset, the `+1` probe, the trim and
the `{"data": [...], "page": {...}}` envelope, so a handler cannot forget any
of them. Default page size 50, maximum 200; an out-of-range `limit` is a 400,
never a silent clamp. Row types implement `platform.Keyed` (`PageKey() (time.Time,
uuid.UUID)`). `TestEveryCollectionGETIsPaginated` fails any collection GET that
is neither paginated nor in `unpaginatedGETAllowlist` with a reason — the two
kinds that qualify are single-item reads and collections bounded by
construction rather than by tenant data.
```

- [ ] **Step 7: Commit**

```bash
cd backend && go test -race ./internal/archtest/ && cd .. && make lint-go
git add backend/internal/archtest/ backend/scripts/new-module.sh docs/standards/backend.md
git commit -m "test: fail CI on any collection GET that is not paginated, and generate new modules paginated (#816)"
```

---

## Task 6: the frontend affordance

**Files:**
- Create: `packages/api/src/paged.ts`, `packages/api/src/paged.test.tsx`, `packages/ui/src/load-more.tsx`
- Modify: `packages/api/src/index.ts`, `packages/ui/src/index.ts`
- Modify: `apps/medicore/components/visit-panel.tsx`, `apps/medicore/components/ping-panel.tsx`, `apps/pharmacy/components/dispense-list.tsx`, `apps/pharmacy/components/medications-panel.tsx`, `apps/lab/components/order-list.tsx`
- Modify: `docs/standards/frontend.md`

**Interfaces:**
- Produces: `useApiPagedQuery<T>(key: unknown[], path: string): { items: T[]; hasMore: boolean; loadMore: () => void; isPending: boolean; isFetchingMore: boolean }`; `<LoadMore onClick hasMore isLoading />`.

**Why a new hook rather than changing `useApiQuery`:** `useApiQuery` is a thin `useQuery` wrapper used by every panel and by `permissions.tsx`. Reshaping it to `useInfiniteQuery` changes the contract for all of them. A narrow sibling leaves the common case untouched.

- [ ] **Step 1: Write the failing hook test**

```tsx
it("appends the next page and stops when has_more is false", async () => {
  const pages = [
    { data: [{ id: "1" }, { id: "2" }], page: { next_cursor: "c1", has_more: true } },
    { data: [{ id: "3" }], page: { next_cursor: null, has_more: false } },
  ];
  fetchMock.mockImplementation(async (url: string) => jsonResponse(url.includes("cursor=c1") ? pages[1] : pages[0]));

  const { result } = renderHook(() => useApiPagedQuery<{ id: string }>(["rows"], "/rows"), { wrapper });

  await waitFor(() => expect(result.current.items).toHaveLength(2));
  expect(result.current.hasMore).toBe(true);

  act(() => result.current.loadMore());

  await waitFor(() => expect(result.current.items).toHaveLength(3));
  expect(result.current.items.map((r) => r.id)).toEqual(["1", "2", "3"]);
  expect(result.current.hasMore).toBe(false);
});

it("does not duplicate rows when loadMore is called twice in a row", async () => {
  // A double-click on a slow connection. The second call must not fetch
  // the same cursor again and append the same page twice — which is
  // exactly the duplicate-row symptom #816 exists to remove, arriving
  // from the client side instead of the query.
  let resolveSecond: (v: unknown) => void = () => {};
  const secondInFlight = new Promise((r) => (resolveSecond = r));

  fetchMock.mockImplementation(async (url: string) => {
    if (url.includes("cursor=c1")) {
      await secondInFlight;
      return jsonResponse({ data: [{ id: "3" }], page: { next_cursor: null, has_more: false } });
    }
    return jsonResponse({ data: [{ id: "1" }, { id: "2" }], page: { next_cursor: "c1", has_more: true } });
  });

  const { result } = renderHook(() => useApiPagedQuery<{ id: string }>(["rows"], "/rows"), { wrapper });
  await waitFor(() => expect(result.current.items).toHaveLength(2));

  act(() => result.current.loadMore());
  act(() => result.current.loadMore()); // second click while the first is in flight
  act(() => resolveSecond(null));

  await waitFor(() => expect(result.current.hasMore).toBe(false));
  expect(result.current.items.map((r) => r.id)).toEqual(["1", "2", "3"]);

  const cursorCalls = fetchMock.mock.calls.filter(([url]) => String(url).includes("cursor=c1"));
  expect(cursorCalls).toHaveLength(1);
});
```

Follow the existing `packages/api/src/hooks.test.tsx` for the wrapper and fetch mocking.

- [ ] **Step 2: Run and confirm failure**

```bash
pnpm --filter @hms/api test -- paged
```

Expected: FAIL — `useApiPagedQuery` is not exported.

- [ ] **Step 3: Implement `packages/api/src/paged.ts`**

```tsx
"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { apiFetch } from "./client";

/** The paginated envelope every collection endpoint returns (#816). */
export type Page<T> = {
  data: T[];
  page: { next_cursor: string | null; has_more: boolean };
};

/**
 * useApiPagedQuery reads a cursor-paginated collection, accumulating
 * pages as the caller asks for more.
 *
 * A sibling of useApiQuery rather than a replacement: useApiQuery is used
 * by every panel and by permissions.tsx, and reshaping it to
 * useInfiniteQuery would change the contract for all of them.
 *
 * `hasMore` comes from the server's explicit has_more flag, never from
 * "was the last page full" — a full final page is indistinguishable from
 * a truncated one by length alone, which is the defect #816 exists to fix.
 */
export function useApiPagedQuery<T>(key: unknown[], path: string) {
  const query = useInfiniteQuery({
    queryKey: key,
    initialPageParam: null as string | null,
    queryFn: ({ pageParam }) =>
      apiFetch<Page<T>>(pageParam ? `${path}?cursor=${encodeURIComponent(pageParam)}` : path),
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
  });

  return {
    items: query.data?.pages.flatMap((p) => p.data) ?? [],
    hasMore: query.hasNextPage,
    // Guarded: react-query ignores a fetchNextPage while one is in
    // flight, but calling it during a fetch would still be a request the
    // user did not get feedback for. The button also disables itself.
    loadMore: () => {
      if (query.hasNextPage && !query.isFetchingNextPage) void query.fetchNextPage();
    },
    isPending: query.isPending,
    isFetchingMore: query.isFetchingNextPage,
  };
}
```

- [ ] **Step 4: Run, then prove `hasMore` is server-driven**

```bash
pnpm --filter @hms/api test -- paged
```

Expected: PASS. Then change `getNextPageParam` to infer from length (`last.data.length === 50 ? last.page.next_cursor : undefined`) and confirm a test with a full final page fails. Add that test if it does not already exist — it is the frontend mirror of the exact-boundary case. Restore.

- [ ] **Step 5: Implement `packages/ui/src/load-more.tsx`**

```tsx
"use client";

import { Button } from "@tesserix/web";

/**
 * LoadMore renders nothing when there is nothing more.
 *
 * That absence is the point: it is a positive statement that the client
 * has the whole collection, which is what #816's silently-truncated
 * lists could never say. A button that stayed visible and did nothing
 * would leave the same ambiguity.
 */
export function LoadMore({
  hasMore,
  isLoading,
  onClick,
}: {
  hasMore: boolean;
  isLoading: boolean;
  onClick: () => void;
}) {
  if (!hasMore) return null;
  return (
    <div className="border-t px-5 py-3">
      <Button variant="outline" size="sm" onClick={onClick} disabled={isLoading}>
        {isLoading ? "Loading…" : "Load more"}
      </Button>
    </div>
  );
}
```

- [ ] **Step 6: Migrate the five panels**

For each, swap the query and append the control. `visit-panel.tsx` becomes:

```tsx
  const visits = useApiPagedQuery<Visit>(["visits"], "/medicore/visits");
```

with `visits.data?.data` reads becoming `visits.items`, and `<LoadMore hasMore={visits.hasMore} isLoading={visits.isFetchingMore} onClick={visits.loadMore} />` rendered after the list. Repeat for `ping-panel`, `dispense-list`, `medications-panel`, `order-list`.

Mutations that `invalidate: [["visits"]]` keep working unchanged — react-query invalidates the whole infinite query.

- [ ] **Step 7: Add a panel test (spec T10)**

Per the frontend standard, every changed panel needs a Vitest test using `renderWithProviders`. Add one asserting the button appears with `has_more: true`, appends on click, and disappears when the last page arrives.

- [ ] **Step 8: Document the rule**

Add to `docs/standards/frontend.md`:

```markdown
**Collection endpoints are read with `useApiPagedQuery`, never `useApiQuery`.**
They return `{data, page:{next_cursor, has_more}}` (#816); `useApiQuery` would
show the first page and silently hide the rest. Render `<LoadMore>` after the
list — it renders nothing when `has_more` is false, so its absence is a
positive statement that the list is complete. Never infer "there is more" from
a full page: a full final page and a truncated one are indistinguishable by
length.
```

- [ ] **Step 9: Gates and commit**

```bash
pnpm turbo lint type-check test build
git add packages/ apps/ docs/standards/frontend.md
git commit -m "feat: read collections with useApiPagedQuery and a Load more control (#816)"
```

---

## Task 7: end-to-end verification and PR

- [ ] **Step 1: Write the E2E (spec T11)**

Create `e2e/tests/pagination.spec.ts`. Per the e2e account rule, this spec gets its own seeded account (`scripts/seed-dev.mjs` derives one per spec file — no action needed beyond adding the file).

```ts
test("a ward with more than one page reads to completion", async ({ page }) => {
  await login(page);
  await page.goto("/medicore/opd");

  // Seed past one page through the UI's own create flow, so the test
  // exercises the same path a clerk does.
  for (let i = 0; i < 55; i++) {
    await page.getByLabel("Patient name").fill(`Page Patient ${i}`);
    await page.getByRole("button", { name: "Create visit" }).click();
  }

  const loadMore = page.getByRole("button", { name: "Load more" });
  await expect(loadMore).toBeVisible();

  await loadMore.click();
  await expect(loadMore).toBeHidden();

  // The first-created visit is on the last page; seeing it proves the
  // whole collection was reachable, not just the first 50.
  await expect(page.getByText("Page Patient 0")).toBeVisible();
});
```

- [ ] **Step 2: Run the full stack and the E2E**

```bash
export HMS_PG_PORT=15432 HMS_NATS_PORT=14222 HMS_NATS_MONITOR_PORT=18222 \
  HMS_REDIS_PORT=16379 HMS_OPENFGA_PORT=18090 HMS_GIP_PORT=19099 \
  HMS_API_PORT=18080 NODE_AUTH_TOKEN=$(gh auth token)
make up && ./scripts/verify-local.sh
pnpm --filter e2e exec playwright test
```

`verify-local.sh` must pass before any E2E result is trusted. Note: `pnpm turbo build` overwrites the dev servers' `.next` output — build first, restart `make dev-web`, then run E2E.

- [ ] **Step 3: Prove the E2E can fail**

Set `DefaultLimit` to 100 so 55 visits fit on one page, and confirm the test fails at `expect(loadMore).toBeVisible()`. Restore.

- [ ] **Step 4: Full gates**

```bash
cd backend && go build ./... && go vet ./... && go test -count=1 -race ./... && ./scripts/coverage-gate.sh
cd .. && make lint-go
pnpm turbo lint type-check test build
pnpm --filter e2e exec playwright test --reporter=list
```

Run the E2E suite at least twice — parallel-worker races are not proven absent by one green run.

- [ ] **Step 5: Update the spec status and open the PR**

Change the spec header from `approved` to `implemented` and correct anything the implementation decided differently.

```bash
git add -A && git commit -m "docs: mark the pagination contract design implemented"
git push -u origin feat/816-pagination-contract
```

PR body must carry: link to #816, the six endpoints and their old limits, why keyset rather than offset (with the exact-once test as evidence), the timestamp-precision trap and its test, the compile-enforcement argument plus the arch-test hole it cannot close, the page-size change from 100/500 to 50 and the "Load more" affordance that makes it an improvement rather than a regression, the limitations below, and which assertions were observed failing. Close with `Closes #816`.

**Do not merge.**

---

## Known limitations (carry into the PR body)

- **Cursors are position, not a snapshot.** A row edited between pages is returned with its new values; a row whose `created_at` changes moves. Not a defect for append-mostly clinical lists, and a snapshot would need server-side state (rejected in D2).
- **No total count.** `has_more` says whether more exists, not how much. A count on a tenant-scoped table under RLS is a full scan and no screen displays one.
- **Ordering is fixed** at `created_at DESC, id DESC`. Sorting by other columns needs a cursor per sort key and is out of scope.
- **Six panels change page size from 100 to 50** (members from 500) and gain "Load more".
- **Filtering and sorting** as a general contract remain unbuilt — #666.
- The `unpaginatedGETAllowlist` is a documented convention guarded by CI, which §4 ranks below "impossible to express". Nothing prevents someone adding an entry without justification beyond review.
