// Package pagination owns the cursor and limit halves of the Helivanta list
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
