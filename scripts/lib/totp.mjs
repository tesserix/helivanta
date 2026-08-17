// Minimal RFC 6238 TOTP (SHA-1, 30-second step, 6 digits) — the same
// parameters Zitadel's own `POST /v2/users/{id}/totp` enrolment uses (spike
// §5), and what any real authenticator app would compute from the returned
// `secret`. No dependency added for this: the algorithm is ~20 lines of
// HMAC-SHA1 and base32 decoding, both of which node:crypto already does the
// hard part of.
//
// scripts/seed-dev.mjs uses this to VERIFY the TOTP enrolment it creates for
// mfa@helivanta.dev (POST /v2/users/{id}/totp/verify needs a real code, not
// just the secret). e2e/tests/support/totp.ts implements the identical
// algorithm on the Playwright side, to compute the code a real sign-in
// submits (#867 Task 6) — the two are deliberately NOT shared via an import
// across the scripts/ and e2e/ package boundary; see that file's header
// comment for why duplicating ~20 lines here beats a cross-package coupling.
import { createHmac } from "node:crypto";

const BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
const STEP_SECONDS = 30;
const DIGITS = 6;

function base32Decode(input) {
  const clean = input.toUpperCase().replace(/=+$/, "");
  let bits = "";
  for (const char of clean) {
    const value = BASE32_ALPHABET.indexOf(char);
    if (value === -1) {
      throw new Error(`invalid base32 character in TOTP secret: ${char}`);
    }
    bits += value.toString(2).padStart(5, "0");
  }
  const bytes = [];
  for (let i = 0; i + 8 <= bits.length; i += 8) {
    bytes.push(parseInt(bits.slice(i, i + 8), 2));
  }
  return Buffer.from(bytes);
}

// generateTOTP computes the 6-digit code for `secret` (base32, as returned
// by Zitadel's enrolment call) at `atMs` (defaults to now — real wall
// clock, nothing mocked, since this must match what Zitadel's own server
// clock accepts).
export function generateTOTP(secret, atMs = Date.now()) {
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
