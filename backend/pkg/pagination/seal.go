package pagination

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

const sealInfo = "helivanta/pagination/cursor/v1"

// Sealer encrypts cursors that may anchor on a row the reader was never shown.
type Sealer struct{ aead cipher.AEAD }

// NewSealer derives an AES-256-GCM cursor key from secret, so one deployment secret can serve without key reuse.
func NewSealer(secret []byte) (*Sealer, error) {
	if len(secret) < 32 {
		return nil, errors.New("pagination: a cursor sealer needs at least 32 secret bytes")
	}
	key, err := hkdf.Key(sha256.New, secret, nil, sealInfo, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal binds the cursor to its tenant and scope as associated data.
func (s *Sealer) Seal(c Cursor) string {
	plain, err := c.wire()
	if err != nil {
		return ""
	}
	nonce := make([]byte, s.aead.NonceSize())
	_, _ = rand.Read(nonce)
	return base64.RawURLEncoding.EncodeToString(s.aead.Seal(nonce, nonce, plain, sealBinding(c.TenantID, c.Scope)))
}

// Open refuses, with one error, any cursor not sealed by this key for this tenant and scope.
func (s *Sealer) Open(raw, tenantID, scope string) (Cursor, error) {
	blob, err := base64.RawURLEncoding.DecodeString(raw)
	size := s.aead.NonceSize()
	if raw == "" || err != nil || len(blob) < size+s.aead.Overhead() {
		return Cursor{}, fmt.Errorf("%w: not a sealed cursor", ErrInvalidCursor)
	}
	plain, err := s.aead.Open(nil, blob[:size], blob[size:], sealBinding(tenantID, scope))
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: not sealed for this list", ErrInvalidCursor)
	}
	c, err := decodeWire(plain, tenantID)
	if err != nil {
		return Cursor{}, err
	}
	if c.Scope != scope {
		return Cursor{}, fmt.Errorf("%w: not sealed for this list", ErrInvalidCursor)
	}
	return c, nil
}

func sealBinding(tenantID, scope string) []byte {
	return []byte(sealInfo + "\x00" + tenantID + "\x00" + scope)
}
