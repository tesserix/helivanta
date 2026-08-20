// Seeds the local Zitadel with the dev users and grants each a role via
// iam_members.
//
// Two of them are for humans: test@helivanta.dev (tenant_admin) and
// pharmacist@helivanta.dev (pharmacist only). The second user is what makes
// permission gating observable by hand — everything test@helivanta.dev can do,
// pharmacist@helivanta.dev mostly cannot.
//
// test@helivanta.dev additionally holds a *different* role (pharmacist) in a
// second hospital, which is what makes tenant switching demonstrable:
// after switching, the same person must lose every zone tenant_admin gave
// them in the first hospital. A second membership with the same role would
// look identical before and after the switch and would pass even if the
// switch did nothing.
//
// The rest are generated, one admin and one pharmacist PER PLAYWRIGHT SPEC
// FILE, and the e2e suite is required to use them rather than the human
// accounts. This is not tidiness: since #781, signing out writes a
// revocation watermark that is per SUBJECT and global across every tenant
// and device (spec D5). Two spec files sharing one account therefore
// invalidate each other's sessions the moment either one signs out, which
// under Playwright's default parallel workers fails whichever spec was
// unlucky. Deriving the accounts from the spec filenames — here and in
// e2e/tests/support/login.ts, from the same rule — means a newly added
// spec file gets its own isolated pair on the next `make seed` with
// nothing to remember.
//
// #838 Task 6 replaces the Firebase/GIP emulator seeding this file used to
// do with Zitadel's. The load-bearing difference from the old version:
// EVERY seeded account is verified by completing a REAL password check
// against Zitadel, not by trusting the creation call's status code.
// POST /v2/users/human returns 200 while silently discarding fields it
// does not recognise (docs/superpowers/spikes/2026-08-15-zitadel-spike.md
// "Task 0" — the exact trap that produced users who looked seeded and
// could not authenticate). A script that trusts the response produces
// accounts that fail later as every e2e spec timing out at the login
// form, which reads like a broken application rather than a broken seed.
//
// #854 Task 7: that verification used to drive Zitadel's hosted login UI
// with a headless Playwright browser (hostedUILogin). #854 Task 1
// repointed helivanta-web's login redirect at Helivanta's own /login — and `make up`
// runs this script (via `seed`) BEFORE the web app starts, so by the time
// this ran there was nothing at :4301 for the redirect to reach at all.
// verifyPasswordLogin (scripts/lib/zitadel.mjs) replaces the browser drive
// with a direct call to Zitadel's v2 Session API, the same credential
// check Helivanta's own backend performs — see its doc comment for the full
// reasoning. This also means seeding no longer depends on the web app, or
// any browser, existing.
//
// Usage: node scripts/seed-dev.mjs   (Zitadel + Postgres must be up and
// migrated — `make seed` runs `make dev-infra` and `make migrate` first for
// exactly this reason; dev-infra also runs scripts/zitadel-bootstrap.mjs,
// which this script depends on for the client_id and machine PAT.)
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  managementAPI,
  readLoginClientPAT,
  readMachinePAT,
  verifyPasswordLogin,
} from "./lib/zitadel.mjs";
import { generateTOTP } from "./lib/totp.mjs";

const ISSUER = process.env.ZITADEL_ISSUER_URL ?? "http://auth.tesserix.localhost:20080";
const TENANT_ID = "11111111-1111-1111-1111-111111111111";
// St Mary's, the second hospital test@helivanta.dev works at. The e2e tenant
// switch journey (e2e/tests/tenant-switch.spec.ts) hard-codes this id.
const SECOND_TENANT_ID = "22222222-2222-2222-2222-222222222222";

// password123 (the old GIP-emulator value) fails Zitadel's default
// complexity policy outright — "Password must contain upper case",
// observed live while wiring this up. HmsDev123! satisfies it (upper,
// lower, digit, symbol), mirroring the spike's own Password123! finding
// that complexity, not the recipe, was the sensitivity to watch for.
const PASSWORD = "HmsDev123!";

// #867 Task 6: the one account beyond the human/per-spec pairs below that
// carries a REAL, verified TOTP factor, so mfa.spec.ts can drive Zitadel's
// actual second-factor check end to end instead of mocking it. Named like
// the two human accounts above (not derived via specEmail) because it
// belongs to exactly one spec file — nothing else ever signs in as it, so
// the per-spec-account isolation specEmail exists for (see that function's
// doc comment on the revocation watermark) is satisfied trivially, and a
// name that reads as "the MFA account" is clearer than
// e2e-mfa-admin@helivanta.dev would be for a spec that is fundamentally
// about the factor check, not about roles or tenants.
const MFA_EMAIL = "mfa@helivanta.dev";

// Where the TOTP secret Zitadel hands back on enrolment gets recorded so
// e2e/tests/support/totp.ts can compute a real code at test time. Lives
// alongside the PAT files in dev/zitadel/secrets/ (gitignored — see that
// directory's .gitignore entry) and is regenerated the same way they are:
// wiped with the zitadel-db volume on `make reset`, rewritten from scratch
// the next time this script enrols the account fresh. Deliberately never
// logged to stdout — see ensureTOTPEnrolled below.
const TOTP_SECRET_PATH = fileURLToPath(
  new URL("../dev/zitadel/secrets/mfa-totp.secret", import.meta.url),
);

// The two-hospital membership set, shared by test@helivanta.dev and by every
// generated per-spec admin. Every spec's admin gets BOTH memberships, not
// just the spec that switches tenants today: a spec-scoped account whose
// role set is narrower than the human account it replaced would quietly
// remove coverage the moment a future spec needs the picker.
const ADMIN_MEMBERSHIPS = [
  { tenantId: TENANT_ID, role: "tenant_admin" },
  { tenantId: SECOND_TENANT_ID, role: "pharmacist" },
];
const PHARMACIST_MEMBERSHIPS = [{ tenantId: TENANT_ID, role: "pharmacist" }];

// The single source of the account-name rule. e2e/tests/support/login.ts
// implements the identical derivation on its side; if the two ever drift,
// login fails loudly at sign-in rather than silently falling back to a
// shared account, which is the failure mode worth having.
const specEmail = (slug, kind) => `e2e-${slug}-${kind}@helivanta.dev`;

// Reads the spec filenames rather than listing them, so adding a spec file
// is all it takes to get an isolated account pair. A missing directory is
// tolerated (someone seeding a checkout without the e2e workspace) — the
// human accounts below are what that person is after.
function specSlugs() {
  const dir = fileURLToPath(new URL("../e2e/tests", import.meta.url));
  let entries;
  try {
    entries = readdirSync(dir);
  } catch (err) {
    if (err.code !== "ENOENT") throw err;
    console.warn(`No ${dir}; skipping per-spec e2e accounts.`);
    return [];
  }
  return entries
    .filter((name) => name.endsWith(".spec.ts"))
    .map((name) => name.slice(0, -".spec.ts".length))
    .sort();
}

// Each user and the tenant memberships iam_members should hold for them.
// Unlike the old GIP-based version, no membership is "home" anymore —
// there is no tenant_id claim to seed; POST /v1/auth/login picks the
// first tenant ListRoles returns and the picker/switchTenant handle the
// rest (spec D1/D3).
const USERS = [
  { email: "test@helivanta.dev", memberships: ADMIN_MEMBERSHIPS },
  { email: "pharmacist@helivanta.dev", memberships: PHARMACIST_MEMBERSHIPS },
  { email: MFA_EMAIL, memberships: PHARMACIST_MEMBERSHIPS },
  ...specSlugs().flatMap((slug) => [
    { email: specEmail(slug, "admin"), memberships: ADMIN_MEMBERSHIPS },
    { email: specEmail(slug, "pharmacist"), memberships: PHARMACIST_MEMBERSHIPS },
  ]),
];

function profileFor(email) {
  const local = email.split("@")[0];
  return { givenName: local, familyName: "Seed" };
}

async function findUserId(pat, email) {
  const { result } = await managementAPI(ISSUER, pat, "/v2/users", {
    queries: [{ userNameQuery: { userName: email, method: "TEXT_QUERY_METHOD_EQUALS" } }],
  });
  return result?.[0]?.userId ?? null;
}

// ensureUser creates email as a Zitadel human user (isVerified + a
// complexity-satisfying password + changeRequired:false — both fields
// load-bearing, see the module doc comment and the spike's Task 0), or
// resolves its existing userId if it already exists. Returns the userId —
// which becomes authn.Principal.Subject and must match the `subject`
// written into iam_members below, or the seeded user has no permissions.
async function ensureUser(pat, email) {
  try {
    const created = await managementAPI(ISSUER, pat, "/v2/users/human", {
      username: email,
      profile: profileFor(email),
      email: { email, isVerified: true },
      password: { password: PASSWORD, changeRequired: false },
    });
    return created.userId;
  } catch (err) {
    if (!/already exists/i.test(err.message)) throw err;
    const existing = await findUserId(pat, email);
    if (!existing) {
      throw new Error(
        `${email} reported "already exists" but a search for it found nothing: ${err.message}`,
      );
    }
    return existing;
  }
}

// hasVerifiedTOTP reads Zitadel's live state for the user rather than
// trusting anything this script wrote earlier — the source of truth for
// "is TOTP already enrolled" is Zitadel itself, not a file on disk. GET,
// not POST (unlike every other call in this file): Zitadel's v2 API takes
// this one as a plain GET, so it bypasses managementAPI (which always
// POSTs — see that function's own doc comment).
async function hasVerifiedTOTP(pat, userId) {
  const res = await fetch(`${ISSUER}/v2/users/${userId}/authentication_methods`, {
    headers: { Authorization: `Bearer ${pat}` },
  });
  const json = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(
      `GET authentication_methods for user ${userId} failed: HTTP ${res.status} ${JSON.stringify(json)}`,
    );
  }
  return (json.authMethodTypes ?? []).includes("AUTHENTICATION_METHOD_TYPE_TOTP");
}

// ensureTOTPEnrolled gives MFA_EMAIL a REAL, Zitadel-verified TOTP factor
// (#867 Task 6) using the seed PAT throughout — never the login-client
// PAT, which the spike (docs/superpowers/spikes/2026-08-17-zitadel-login-
// client-mfa.md §6) found can create a user but cannot delete one (403),
// which is how that spike run left a user behind. Enrolment and cleanup
// both belong to setup, not to the runtime login-client flow, so both use
// the same PAT class as every other call in this script.
//
// Idempotent against Zitadel's OWN state (hasVerifiedTOTP), not against
// this file's cached secret: Zitadel returns the `secret` from
// `POST /v2/users/{id}/totp` exactly once, at enrolment. So if the account
// already carries a verified TOTP method, this reuses the secret recorded
// on the run that created it (TOTP_SECRET_PATH) rather than trying to
// fetch it again — an account seeded once and left alone across ordinary
// `make seed` re-runs keeps working without re-enrolling. A FULL
// `make reset` wipes the zitadel-db volume, so the live check below goes
// false again and this re-enrols from scratch, overwriting the stale file
// with the freshly issued secret.
//
// Enrolment verification (POST /v2/users/{id}/totp/verify — spelled
// exactly that way, NOT /totp/_verify, which 404s per the spike's §5)
// needs a REAL code, not just the secret, so this computes one with the
// same TOTP algorithm scripts/lib/totp.mjs implements — proving, before
// the e2e suite ever runs, that the recorded secret actually produces
// codes Zitadel accepts.
async function ensureTOTPEnrolled(pat, userId, email) {
  if (await hasVerifiedTOTP(pat, userId)) {
    let secret;
    try {
      secret = readFileSync(TOTP_SECRET_PATH, "utf8").trim();
    } catch (err) {
      if (err.code !== "ENOENT") throw err;
      throw new Error(
        `${email} already has a verified TOTP method in Zitadel, but ${TOTP_SECRET_PATH} is ` +
          `missing — Zitadel never re-exposes an enrolled secret, so mfa.spec.ts cannot compute ` +
          `a code for this account. Run 'make down && RESET_YES=1 make reset && make up' to ` +
          `re-provision it from scratch.`,
      );
    }
    console.log(`${email} already has a verified TOTP method; reusing the recorded secret.`);
    return secret;
  }

  const enrolled = await managementAPI(ISSUER, pat, `/v2/users/${userId}/totp`, {});
  const secret = enrolled.secret;
  if (!secret) {
    throw new Error(
      `POST /v2/users/${userId}/totp for ${email} returned no secret: ${JSON.stringify(enrolled)}`,
    );
  }

  const code = generateTOTP(secret);
  await managementAPI(ISSUER, pat, `/v2/users/${userId}/totp/verify`, { code });

  // Not printed unconditionally: this is a real, working credential for a
  // dev-only account, and stdout from `make seed` / `make up` can end up
  // in CI logs or a pasted terminal. Written to the same gitignored
  // secrets directory the PAT files live in, with the same restrictive
  // mode.
  writeFileSync(TOTP_SECRET_PATH, `${secret}\n`, { mode: 0o600 });
  console.log(`Enrolled and verified TOTP for ${email} (secret recorded, not printed)`);
  return secret;
}

async function main() {
  const pat = readMachinePAT();
  const loginClientPAT = readLoginClientPAT();
  const { Client } = await import("pg");
  const pg = new Client({
    connectionString:
      process.env.ADMIN_DATABASE_URL ??
      "postgres://helivanta:helivanta@localhost:5432/helivanta?sslmode=disable",
  });
  await pg.connect();

  try {
    for (const { email, memberships } of USERS) {
      const userId = await ensureUser(pat, email);
      console.log(`Seeded ${email} / ${PASSWORD} (userId=${userId})`);

      // Bootstrap: the first tenant_admin cannot be granted through a
      // route that requires iam.member.manage, so the seed writes the
      // membership row directly for every seeded user. The API's
      // reconciler turns it into tuples on next boot; the iam-fga-sync
      // consumer does it immediately for later grants.
      for (const { tenantId, role } of memberships) {
        await pg.query(
          `INSERT INTO iam_members (tenant_id, subject, role_key)
           VALUES ($1, $2, $3)
           ON CONFLICT (tenant_id, subject, role_key) DO NOTHING`,
          [tenantId, userId, role],
        );
        console.log(`Granted ${role} to ${email} in tenant ${tenantId}`);
      }

      // THE control this rewrite exists for: prove the account can
      // actually authenticate, not that the creation call returned 200.
      // Throws (and this script exits non-zero) on any failure — an
      // account that cannot log in is not seeded, whatever the API said.
      await verifyPasswordLogin(ISSUER, loginClientPAT, email, PASSWORD);
      console.log(`Verified ${email} completes a real password check`);

      // #867 Task 6: only MFA_EMAIL gets a second factor — every other
      // seeded account stays password-only, which is what the twelve
      // existing e2e specs assume. Enrolment uses the seed PAT (pat), not
      // loginClientPAT, for the reason ensureTOTPEnrolled's own doc
      // comment gives.
      if (email === MFA_EMAIL) {
        await ensureTOTPEnrolled(pat, userId, email);
      }
    }
  } finally {
    await pg.end();
  }

  console.log(`Seeded and login-verified:
  test@helivanta.dev       / ${PASSWORD}  (tenant_admin in ${TENANT_ID} — sees every zone;
                                    pharmacist in ${SECOND_TENANT_ID} — switch to see Pharmacy only)
  pharmacist@helivanta.dev / ${PASSWORD}  (pharmacist — sees Pharmacy only)
  ${MFA_EMAIL}       / ${PASSWORD}  (pharmacist — TOTP-enrolled; e2e/tests/mfa.spec.ts's own
                                    account, secret recorded in ${TOTP_SECRET_PATH})
  plus one e2e-<spec>-admin@helivanta.dev and one e2e-<spec>-pharmacist@helivanta.dev per
  Playwright spec file, so no two specs share a revocation subject.`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
