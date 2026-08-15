// The API-level auth round trip scripts/verify-local.sh drives (#838 Task
// 6): a real Zitadel token → POST /v1/auth/login → an HMS session that
// works on a /v1 route. Deliberately API-level, not through the shell's
// old /api/session route — apps/shell still authenticates via Firebase
// (Task 6 is dev stack/seeding only; the frontend is a separate follow-on),
// so that route no longer exists on the path a Zitadel token can take. This
// script proves the part of the chain that IS wired today: Zitadel issues a
// real token, the API verifies it and mints a real HMS session, and that
// session actually resolves a permission through OpenFGA.
//
// Usage: node scripts/zitadel-verify-login.mjs
// Env: ZITADEL_ISSUER_URL, ZITADEL_CLIENT_ID, API_URL (all have the same
// dev defaults the Makefile uses).
import { DEV_REDIRECT_URI, hostedUILogin, readClientID } from "./lib/zitadel.mjs";

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
  let clientId;
  try {
    clientId = readClientID();
  } catch (err) {
    return step("preconditions", false, err.message);
  }

  let idToken;
  try {
    idToken = await hostedUILogin({
      issuer: ISSUER,
      clientId,
      redirectUri: DEV_REDIRECT_URI,
      email: SEED_USER,
      password: SEED_PASSWORD,
    });
  } catch (err) {
    return step("zitadel hosted-UI login", false, err.message);
  }
  step("zitadel hosted-UI login", true, `got an ID token for ${SEED_USER}`);

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
