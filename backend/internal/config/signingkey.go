package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// DevSessionSigningKey is the well-known Ed25519 seed the local dev
// stack (Makefile's dev-api target) sets SESSION_SIGNING_KEY to. It
// buys nothing: it is committed to source control and every developer
// and CI runner shares it, so a session it mints can be forged by
// anyone who has cloned this repo. That is fine ONLY because
// SessionSigningKeySeed refuses to honor it outside HMS_ENV=dev — see
// the guard there, which mirrors NewGIPVerifier's
// FIREBASE_AUTH_EMULATOR_HOST guard in pkg/authn/gip.go for the same
// reason: a control that is silently unlockable by an env var needs a
// second, independent check tying it to "this is a developer machine",
// not just "a value was supplied".
const DevSessionSigningKey = "X5yoi73f6FRR8XH2ZfRBjanOZLm/bkae0QV7wGJRuf8="

// ErrNoSessionSigningKey is returned by SessionSigningKeySeed when
// SESSION_SIGNING_KEY is unset. See the function's doc comment for why
// this refuses rather than generating or defaulting a key.
var ErrNoSessionSigningKey = errors.New("config: SESSION_SIGNING_KEY is not set")

// ErrDevSessionSigningKeyOutsideDev is returned when the well-known dev
// key (DevSessionSigningKey) is configured outside HMS_ENV=dev.
var ErrDevSessionSigningKeyOutsideDev = errors.New("config: SESSION_SIGNING_KEY is the well-known dev key outside HMS_ENV=dev")

// ErrMalformedSessionSigningKey is returned when SESSION_SIGNING_KEY is
// set but is not valid base64, or does not decode to exactly a
// 32-byte Ed25519 seed.
var ErrMalformedSessionSigningKey = errors.New("config: SESSION_SIGNING_KEY is not a valid ed25519 seed")

// SessionSigningKeySeed decodes and validates the Ed25519 seed HMS
// signs and verifies its own session tokens with (spec D5, plan Task 2).
//
// # Why this refuses rather than falls back to anything
//
// docs/standards/engineering-principles.md §3 draws the line this
// package's other env loading (getenv, getenvInt, getenvDuration)
// deliberately sits on the other side of: LOG_LEVEL, the rate limits,
// and now SessionTTL are CAPACITY controls, and a capacity control
// fails OPEN — a mistyped value must not stop a hospital's API from
// booting, so each of those falls back to a sane default and logs the
// mistype.
//
// A signing key is not a capacity control. It is a DATA / IDENTITY
// control, and it fails CLOSED, for two concrete reasons that are not
// obvious just from having watched LOG_LEVEL fall back to "info":
//
//  1. Generating an ephemeral key at boot when none is configured would
//     let the process start looking healthy, but every session minted
//     against the OLD key becomes unverifiable the moment the process
//     restarts (a deploy, a crash, an autoscale event) — every clinician
//     logged out at once, with no error anywhere, and nothing that looks
//     like an outage except the flood of "please log in again" reports.
//  2. Falling back to ANY fixed default key compiled into the binary
//     would be a forged-session vulnerability: this repository is
//     public to anyone who can read the source (and DevSessionSigningKey
//     a few lines up is proof that default keys are exactly that
//     readable), so a default key is a default admission ticket —
//     anyone can mint a session for any subject in any tenant.
//
// So there is no default here, generated or fixed. An absent or
// malformed key is a boot failure (compile error > BOOT FAILURE > CI
// failure > convention, §4) — the second-strongest control this
// codebase has, chosen deliberately over "log a warning and hope
// someone notices" because a control that needs a human to notice a log
// line is not a control.
//
// # Key format
//
// SESSION_SIGNING_KEY is a base64-standard-encoded Ed25519 SEED (32
// bytes), not a PEM block and not the 64-byte expanded private key.
// Ed25519 has a 32-byte seed as its actual entropy — the 64-byte form
// jwt/v5 and crypto/ed25519's Sign functions want is deterministically
// derived from it via ed25519.NewKeyFromSeed. A PEM block is the more
// common wire format for a private key, but it is a multi-line,
// multi-field envelope (headers, base64 body, footer) that is awkward
// to hold in a single Makefile/compose/Secret-Manager env var value
// without either embedding literal newlines or having callers re-derive
// PEM structure just to unwrap 32 bytes. A bare base64 seed is exactly
// as strong (same entropy, same algorithm) and is one flat string, so
// it is what every caller of this function — the Makefile's dev-api
// target, docker-compose, and eventually the secret manager per #45 —
// gets to treat uniformly.
//
// A malformed value (not base64, or the wrong decoded length) is
// refused outright rather than truncated, padded, or hashed into
// something 32 bytes long: any of those would silently turn "the
// operator's input was wrong" into "here is a key that works, but isn't
// the one anybody configured" — a half-parsed key that LOOKS like a
// working Signer is worse than no Signer at all, because the boot
// refusal that should have caught the misconfiguration never fires.
func (c Config) SessionSigningKeySeed() ([]byte, error) {
	raw := strings.TrimSpace(c.SessionSigningKey)
	if raw == "" {
		return nil, fmt.Errorf(
			"%w: refusing to boot without one — an ephemeral generated key would "+
				"invalidate every session on the next restart, and a default key "+
				"would be a forged-session vulnerability; set SESSION_SIGNING_KEY to "+
				"a base64 ed25519 seed (32 bytes)",
			ErrNoSessionSigningKey,
		)
	}

	if raw == DevSessionSigningKey && !c.IsDev() {
		return nil, fmt.Errorf(
			"%w: SESSION_SIGNING_KEY is set to the well-known dev key committed to "+
				"this repository, which means anyone who has cloned it can mint a "+
				"session for any subject in any tenant; unset it and provision a "+
				"real key, or set HMS_ENV=dev if this really is a developer machine",
			ErrDevSessionSigningKeyOutsideDev,
		)
	}

	seed, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: SESSION_SIGNING_KEY is not valid base64: %v", ErrMalformedSessionSigningKey, err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf(
			"%w: SESSION_SIGNING_KEY decodes to %d bytes, want exactly %d (an ed25519 seed, not the expanded private key)",
			ErrMalformedSessionSigningKey, len(seed), ed25519.SeedSize,
		)
	}
	return seed, nil
}
