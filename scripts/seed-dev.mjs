// Seeds the local Zitadel with the dev users and grants each a role via
// iam_members.
//
// Two of them are for humans: test@hms.dev (tenant_admin) and
// pharmacist@hms.dev (pharmacist only). The second user is what makes
// permission gating observable by hand — everything test@hms.dev can do,
// pharmacist@hms.dev mostly cannot.
//
// test@hms.dev additionally holds a *different* role (pharmacist) in a
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
// repointed helivanta-web's login redirect at HMS's own /login — and `make up`
// runs this script (via `seed`) BEFORE the web app starts, so by the time
// this ran there was nothing at :4301 for the redirect to reach at all.
// verifyPasswordLogin (scripts/lib/zitadel.mjs) replaces the browser drive
// with a direct call to Zitadel's v2 Session API, the same credential
// check HMS's own backend performs — see its doc comment for the full
// reasoning. This also means seeding no longer depends on the web app, or
// any browser, existing.
//
// Usage: node scripts/seed-dev.mjs   (Zitadel + Postgres must be up and
// migrated — `make seed` runs `make dev-infra` and `make migrate` first for
// exactly this reason; dev-infra also runs scripts/zitadel-bootstrap.mjs,
// which this script depends on for the client_id and machine PAT.)
import { readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  managementAPI,
  readLoginClientPAT,
  readMachinePAT,
  verifyPasswordLogin,
} from "./lib/zitadel.mjs";

const ISSUER = process.env.ZITADEL_ISSUER_URL ?? "http://localhost:20080";
const TENANT_ID = "11111111-1111-1111-1111-111111111111";
// St Mary's, the second hospital test@hms.dev works at. The e2e tenant
// switch journey (e2e/tests/tenant-switch.spec.ts) hard-codes this id.
const SECOND_TENANT_ID = "22222222-2222-2222-2222-222222222222";

// password123 (the old GIP-emulator value) fails Zitadel's default
// complexity policy outright — "Password must contain upper case",
// observed live while wiring this up. HmsDev123! satisfies it (upper,
// lower, digit, symbol), mirroring the spike's own Password123! finding
// that complexity, not the recipe, was the sensitivity to watch for.
const PASSWORD = "HmsDev123!";

// The two-hospital membership set, shared by test@hms.dev and by every
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
const specEmail = (slug, kind) => `e2e-${slug}-${kind}@hms.dev`;

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
  { email: "test@hms.dev", memberships: ADMIN_MEMBERSHIPS },
  { email: "pharmacist@hms.dev", memberships: PHARMACIST_MEMBERSHIPS },
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

async function main() {
  const pat = readMachinePAT();
  const loginClientPAT = readLoginClientPAT();
  const { Client } = await import("pg");
  const pg = new Client({
    connectionString:
      process.env.ADMIN_DATABASE_URL ??
      "postgres://hms:hms@localhost:5432/hms?sslmode=disable",
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
    }
  } finally {
    await pg.end();
  }

  console.log(`Seeded and login-verified:
  test@hms.dev       / ${PASSWORD}  (tenant_admin in ${TENANT_ID} — sees every zone;
                                    pharmacist in ${SECOND_TENANT_ID} — switch to see Pharmacy only)
  pharmacist@hms.dev / ${PASSWORD}  (pharmacist — sees Pharmacy only)
  plus one e2e-<spec>-admin@hms.dev and one e2e-<spec>-pharmacist@hms.dev per
  Playwright spec file, so no two specs share a revocation subject.`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
