package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateSessionSigningKey mints a fresh SESSION_SIGNING_KEY value in
// exactly the format SessionSigningKeySeed accepts: a base64-standard
// encoded 32-byte Ed25519 seed (#45, spec D6).
//
// It exists so that provisioning a key is a command rather than a
// runbook instruction. An operator following prose reaches for `openssl`
// and produces a PEM block or a wrong-length value; SessionSigningKeySeed
// refuses both, but it refuses them at BOOT, against the shared
// production instance, at the moment an operator is least equipped to
// debug a base64 length mismatch.
//
// crypto/rand only — never math/rand, and never a passphrase derivation.
// This value is the entropy behind every Helivanta session; anything
// predictable here is session forgery for every subject in every tenant.
func GenerateSessionSigningKey() (string, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return "", fmt.Errorf("config: generating session signing key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(seed), nil
}
