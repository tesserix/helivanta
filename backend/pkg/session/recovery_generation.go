package session

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
)

type generationClaim string

func (g *generationClaim) UnmarshalJSON(raw []byte) error {
	var value string
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil || !validRecoveryGeneration(value) {
		return ErrInvalidToken
	}
	*g = generationClaim(value)
	return nil
}

func validRecoveryGeneration(raw string) bool {
	if raw == "" {
		return true
	}
	id, err := uuid.Parse(raw)
	return err == nil && id.String() == raw && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
