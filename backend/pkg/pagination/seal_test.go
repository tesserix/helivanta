package pagination_test

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/pagination"
)

func TestSealedCursorHidesItsAnchorAndOpensOnlyForItsTenantAndScope(t *testing.T) {
	sealer, err := pagination.NewSealer(bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)
	at := time.Date(2026, 11, 6, 1, 2, 3, 456789000, time.UTC)
	id := uuid.MustParse("019a5c40-0000-7000-8000-0000000000aa")
	cursor := pagination.Cursor{TenantID: testTenant, Scope: "scope-a", CreatedAt: at, ID: id, Direction: pagination.OldestFirst}

	raw := sealer.Seal(cursor)
	blob, err := base64.RawURLEncoding.DecodeString(raw)
	require.NoError(t, err)
	for _, secret := range []string{id.String(), "2026-11-06", "scope-a", testTenant} {
		require.NotContains(t, string(blob), secret, "a sealed cursor must not carry its anchor or binding in readable form")
	}
	_, err = pagination.Decode(raw, testTenant)
	require.ErrorIs(t, err, pagination.ErrInvalidCursor, "a sealed cursor is not a plain cursor")
	require.NotEqual(t, raw, sealer.Seal(cursor), "every seal uses a fresh nonce")

	opened, err := sealer.Open(raw, testTenant, "scope-a")
	require.NoError(t, err)
	require.True(t, opened.CreatedAt.Equal(at))
	require.Equal(t, id, opened.ID)
	require.Equal(t, pagination.OldestFirst, opened.Direction)
	require.Equal(t, "scope-a", opened.Scope)
	require.Equal(t, testTenant, opened.TenantID)

	other, err := pagination.NewSealer(bytes.Repeat([]byte{8}, 32))
	require.NoError(t, err)
	flipped := bytes.Clone(blob)
	flipped[len(flipped)-1] ^= 1
	tampered := base64.RawURLEncoding.EncodeToString(flipped)
	for name, open := range map[string]func() (pagination.Cursor, error){
		"another tenant": func() (pagination.Cursor, error) {
			return sealer.Open(raw, "22222222-2222-2222-2222-222222222222", "scope-a")
		},
		"another scope":   func() (pagination.Cursor, error) { return sealer.Open(raw, testTenant, "scope-b") },
		"another key":     func() (pagination.Cursor, error) { return other.Open(raw, testTenant, "scope-a") },
		"a flipped byte":  func() (pagination.Cursor, error) { return sealer.Open(tampered, testTenant, "scope-a") },
		"a short value":   func() (pagination.Cursor, error) { return sealer.Open(raw[:10], testTenant, "scope-a") },
		"a plain cursor":  func() (pagination.Cursor, error) { return sealer.Open(cursor.Encode(), testTenant, "scope-a") },
		"not base64":      func() (pagination.Cursor, error) { return sealer.Open("!!", testTenant, "scope-a") },
		"an empty cursor": func() (pagination.Cursor, error) { return sealer.Open("", testTenant, "scope-a") },
	} {
		_, err := open()
		require.ErrorIs(t, err, pagination.ErrInvalidCursor, name)
	}

	_, err = pagination.NewSealer(make([]byte, 31))
	require.Error(t, err, "a short secret cannot seal cursors")
}
