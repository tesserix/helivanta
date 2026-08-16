// The API-level auth round trip scripts/verify-local.sh drives (#838 Task
// 6): a real Zitadel token → POST /v1/auth/login → an HMS session that
// works on a /v1 route. Deliberately API-level, not through the shell's
// old /api/session route, which #838 deleted: the API now mints and sets
// (Task 6 is dev stack/seeding only; the frontend is a separate follow-on),
// so that route no longer exists on the path a Zitadel token can take. This
// script proves the part of the chain that IS wired today: Zitadel issues a
// real token, the API verifies it and mints a real HMS session, and that
// session actually resolves a permission through OpenFGA.
//
// #854 Task 7: obtaining that real token used to drive Zitadel's hosted
// login UI with a headless Playwright browser (hostedUILogin). #854 Task 1
// repointed hms-web's login redirect at HMS's own /login, whose markup
// (one-step Email/Password/Sign-in) that browser drive never matched.
// passwordLoginIDToken (scripts/lib/zitadel.mjs) replaces it with the same
// authorization-code-plus-PKCE exchange driven directly through Zitadel's
// v2 APIs — no browser, and no dependency on which login UI is on the
// other end of the redirect.
//
// THIS IS THE CHECK THAT MATTERS MOST IN THIS SCRIPT, and the reason it
// must never be weakened to "a callback_url was generated": every prior
// check of the #854 login chain — five task reviews, and one more manual
// check on top of those — stopped at `POST /v1/auth/login/password`
// returning a callback_url with a `code` and `state` in it, which it
// always did correctly. NONE of them drove the actual
// `POST /oauth/v2/token` exchange a real browser performs next
// (apps/shell/app/api/auth/callback/page.tsx via oidc-client-ts). That
// exchange was silently broken — Task 1's Zitadel app provisioning left
// `hms-web` unable to complete it at all (invalid_client: empty client
// secret; see zitadel-bootstrap.mjs's LOGIN_VERSION comment for the full
// story) — for the entire time between Task 1 landing and this task
// finding it, and nothing before this script actually finishing the
// exchange and asserting a real id_token came back would have caught it.
// passwordLoginIDToken throws unless Zitadel's token endpoint returns a
// genuine `id_token` (not just a 2xx), so `step("zitadel password login",
// true, ...)` below is only ever reached once that full exchange has
// actually succeeded.
//
// Usage: node scripts/zitadel-verify-login.mjs
// Env: ZITADEL_ISSUER_URL, ZITADEL_CLIENT_ID, API_URL (all have the same
// dev defaults the Makefile uses).
import {
  DEV_REDIRECT_URI,
  passwordLoginIDToken,
  readClientID,
  readLoginClientPAT,
} from "./lib/zitadel.mjs";

const ISSUER = process.env.ZITADEL_ISSUER_URL ?? "http://localhost:20080";
const API_URL = process.env.API_URL ?? "http://localhost:8080";

const SEED_USER = "test@hms.dev";
// Must match scripts/seed-dev.mjs's PASSWORD — password123 (the old
// GIP-emulator value) fails Zitadel's complexity policy outright, so any
// account seed-dev.mjs actually created uses this value instead.
const SEED_PASSWORD = "HmsDev123!";
const EXPECT_PERMISSION = "medicore.visit.read";

function step(name, ok, detail) {
  const mark = ok ? "  ok   " : "  FAIL ";
  console.log(`${mark} ${name}${detail ? `  ${detail}` : ""}`);
  return ok;
}

async function main() {
  let clientId, loginClientPAT;
  try {
    clientId = readClientID();
    loginClientPAT = readLoginClientPAT();
  } catch (err) {
    return step("preconditions", false, err.message);
  }

  let idToken;
  try {
    idToken = await passwordLoginIDToken({
      issuer: ISSUER,
      clientId,
      loginClientPAT,
      redirectUri: DEV_REDIRECT_URI,
      email: SEED_USER,
      password: SEED_PASSWORD,
    });
  } catch (err) {
    return step("zitadel password login", false, err.message);
  }
  step("zitadel password login", true, `got an ID token for ${SEED_USER}`);

  const loginRes = await fetch(`${API_URL}/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id_token: idToken }),
  });
  if (loginRes.status !== 200) {
    const body = await loginRes.text().catch(() => "");
    return step("POST /v1/auth/login", false, `HTTP ${loginRes.status} ${body}`);
  }
  const setCookie = loginRes.headers.get("set-cookie");
  const match = setCookie?.match(/hms_session=([^;]+)/);
  if (!match) {
    return step("POST /v1/auth/login", false, "200 but no hms_session cookie in the response");
  }
  step("POST /v1/auth/login", true, "minted an hms_session cookie");

  const meRes = await fetch(`${API_URL}/v1/iam/me/permissions`, {
    headers: { Cookie: `hms_session=${match[1]}` },
  });
  const meBody = await meRes.text();
  if (meRes.status !== 200) {
    return step("GET /v1/iam/me/permissions", false, `HTTP ${meRes.status} ${meBody}`);
  }
  if (!meBody.includes(EXPECT_PERMISSION)) {
    return step(
      "GET /v1/iam/me/permissions",
      false,
      `200 but ${EXPECT_PERMISSION} missing — OpenFGA tuples may not be rebuilt; run 'make seed'`,
    );
  }
  return step("GET /v1/iam/me/permissions", true, `resolved ${EXPECT_PERMISSION}`);
}

main()
  .then((ok) => process.exit(ok ? 0 : 1))
  .catch((err) => {
    console.error("  FAIL  auth round trip", err);
    process.exit(1);
  });
