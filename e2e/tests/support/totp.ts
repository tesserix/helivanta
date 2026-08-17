import { createHmac } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// RFC 6238 TOTP (SHA-1, 30-second step, 6 digits) — Zitadel's own TOTP
// enrolment parameters (spike §5), and what any real authenticator app
// would compute from the `secret` `POST /v2/users/{id}/totp` returns.
//
// scripts/lib/totp.mjs implements the IDENTICAL algorithm on the seeding
// side (used there to verify enrolment with a real code, not just the
// secret) — deliberately duplicated rather than imported across the
// scripts/ and e2e/ package boundary: two ~20-line, dependency-free
// implementations of a fixed, well-specified algorithm are less coupling
// than a relative import reaching out of this package, and either one
// drifting from the RFC fails loudly (Zitadel rejects the code) rather
// than silently.
const BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
const STEP_SECONDS = 30;
const DIGITS = 6;

function base32Decode(input: string): Buffer {
  const clean = input.toUpperCase().replace(/=+$/, "");
  let bits = "";
  for (const char of clean) {
    const value = BASE32_ALPHABET.indexOf(char);
    if (value === -1) {
      throw new Error(`invalid base32 character in TOTP secret: ${char}`);
    }
    bits += value.toString(2).padStart(5, "0");
  }
  const bytes: number[] = [];
  for (let i = 0; i + 8 <= bits.length; i += 8) {
    bytes.push(parseInt(bits.slice(i, i + 8), 2));
  }
  return Buffer.from(bytes);
}

// generateTOTP computes the 6-digit code for `secret` (base32) at `atMs`
// (defaults to now — real wall-clock time, nothing mocked, since this has
// to match what Zitadel's own server clock accepts at submit time).
export function generateTOTP(secret: string, atMs: number = Date.now()): string {
  const key = base32Decode(secret);
  const counter = Math.floor(atMs / 1000 / STEP_SECONDS);
  const counterBuf = Buffer.alloc(8);
  counterBuf.writeBigUInt64BE(BigInt(counter));
  const hmac = createHmac("sha1", key).update(counterBuf).digest();
  const offset = hmac[hmac.length - 1] & 0x0f;
  const binCode =
    ((hmac[offset] & 0x7f) << 24) |
    ((hmac[offset + 1] & 0xff) << 16) |
    ((hmac[offset + 2] & 0xff) << 8) |
    (hmac[offset + 3] & 0xff);
  return (binCode % 10 ** DIGITS).toString().padStart(DIGITS, "0");
}

// readSeededTOTPSecret reads the base32 secret scripts/seed-dev.mjs's
// ensureTOTPEnrolled() recorded for mfa@helivanta.dev after a real
// enrol-and-verify round trip against Zitadel — never a value hard-coded
// here. `resolve(process.cwd(), ...)` mirrors signout.spec.ts's own read
// of dev/zitadel/secrets/zitadel.env: `make e2e` and
// `pnpm --filter e2e exec playwright test` both run with cwd=e2e/, so this
// is relative to that, not to this file's own location.
export function readSeededTOTPSecret(): string {
  const path = resolve(process.cwd(), "../dev/zitadel/secrets/mfa-totp.secret");
  try {
    return readFileSync(path, "utf8").trim();
  } catch (err) {
    throw new Error(
      `${path} does not exist — run 'make seed' first (scripts/seed-dev.mjs enrols and ` +
        `verifies a real TOTP factor for mfa@helivanta.dev and records its secret here): ${String(err)}`,
    );
  }
}
