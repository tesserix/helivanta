// Shared Zitadel helpers for scripts/seed-dev.mjs, scripts/zitadel-bootstrap.mjs
// and scripts/zitadel-verify-login.mjs — one place holding the machine-API
// call convention and the real password-login drive, so the three scripts
// cannot drift on how either is done.
//
// #854 Task 7: this used to drive Zitadel's REAL hosted login UI with a
// headless Playwright browser (hostedUILogin/hostedUILoginOnce/
// fillAndSubmit, now removed). #854 Task 1 repointed helivanta-web's login
// redirect at Helivanta's own /login, which broke that approach for every
// caller — see verifyPasswordLogin's and passwordLoginIDToken's doc
// comments for the two different ways it broke and what replaced it.
// Neither replacement needs a browser at all, so this file no longer
// depends on Playwright.
import crypto from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const SECRETS_DIR = fileURLToPath(new URL("../../dev/zitadel/secrets/", import.meta.url));

// The redirect_uri registered on the helivanta-web app (scripts/zitadel-
// bootstrap.mjs). apps/shell/app/api/auth/callback/page.tsx (#838 Task 7)
// is the real handler; passwordLoginIDToken below never actually
// navigates a browser there — it reads the authorization code out of the
// finalize call's callbackUrl instead — so this redirect_uri does not need
// the shell app to be reachable at all. There used to be a second
// exported constant here, DEV_SILENT_RENEW_REDIRECT_URI, for the
// hidden-iframe renewal flow's silent-renew page — deleted along with
// that page and its Zitadel-app registration by #916 (design spec D1):
// renewal no longer touches Zitadel through the browser at all.
// helivanta.localhost, not localhost (#916 Task 4, design spec D6): dev
// serves the app and the IdP on different registrable domains so the
// cross-site condition production runs under is actually exercised.
//
// DERIVED from HELIVANTA_WEB_HOST, not written out. Zitadel rejects an
// authorization request whose redirect_uri is not registered
// byte-for-byte, so these two strings must equal the ones
// scripts/zitadel-bootstrap.mjs registers — and that script builds its
// own from HELIVANTA_WEB_HOST. These used to be literals under a comment
// asking the reader to "keep them in step", which is a documented
// convention, the weakest rung of this repo's enforcement ladder: an
// override of HELIVANTA_WEB_HOST moved the registration and left THESE
// behind, so scripts/zitadel-verify-login.mjs (their only consumer) would
// fail its token exchange on a host mismatch. It also made README.md's
// and .env.example's claim that "every consumer reads these variables
// rather than a literal ... so an override is honoured end to end"
// untrue of exactly one consumer. Deriving it makes the claim true and
// removes the thing to remember.
//
// The port stays literal: 4301 is apps/shell's fixed dev port (package
// scripts, the Makefile, e2e/tests/support/hosts.ts's SHELL_PORT), not a
// configurable.
const WEB_HOST = process.env.HELIVANTA_WEB_HOST ?? "helivanta.localhost";
const DEV_WEB_ORIGIN = `http://${WEB_HOST}:4301`;

export const DEV_REDIRECT_URI = `${DEV_WEB_ORIGIN}/api/auth/callback`;
export const DEV_POST_LOGOUT_REDIRECT_URI = `${DEV_WEB_ORIGIN}/login`;

// readClientID prefers an explicit ZITADEL_CLIENT_ID from the environment
// (an operator override), otherwise reads it straight from
// dev/zitadel/secrets/zitadel.env — the file scripts/zitadel-bootstrap.mjs
// writes. Reading the file directly, rather than trusting a Make-exported
// env var, matters for `make up` specifically: `up`'s `seed` prerequisite
// runs in the SAME top-level make process as `dev-infra`, whose -include of
// this file was already parsed (and, on a fresh clone, empty) before
// dev-infra's recipe ever wrote it. A file read at actual script run time
// has no such staleness window.
export function readClientID() {
  if (process.env.ZITADEL_CLIENT_ID) return process.env.ZITADEL_CLIENT_ID;
  let contents;
  try {
    contents = readFileSync(`${SECRETS_DIR}zitadel.env`, "utf8");
  } catch (err) {
    if (err.code === "ENOENT") {
      throw new Error(
        `${SECRETS_DIR}zitadel.env does not exist — run 'make dev-infra' first ` +
          `so scripts/zitadel-bootstrap.mjs can provision the Helivanta app`,
      );
    }
    throw err;
  }
  const match = contents.match(/^ZITADEL_CLIENT_ID=(.+)$/m);
  if (!match) {
    throw new Error(`${SECRETS_DIR}zitadel.env has no ZITADEL_CLIENT_ID line`);
  }
  return match[1].trim();
}

// readMachinePAT reads the helivanta-seed-bot IAM_OWNER PAT Zitadel itself wrote
// at first boot (docker-compose.dev.yml's zitadel service,
// FirstInstance.Org.Machine.Pat) — the credential scripts/seed-dev.mjs and
// scripts/zitadel-bootstrap.mjs use for every management-API call.
export function readMachinePAT() {
  try {
    return readFileSync(`${SECRETS_DIR}helivanta-seed.pat`, "utf8").trim();
  } catch (err) {
    if (err.code === "ENOENT") {
      throw new Error(
        `${SECRETS_DIR}helivanta-seed.pat does not exist — Zitadel writes it at first ` +
          `boot; check 'docker compose -f docker-compose.dev.yml logs zitadel' ` +
          `for a setup failure (the masterkey-length trap is the most likely ` +
          `cause; see docker-compose.dev.yml's zitadel service comment)`,
      );
    }
    throw err;
  }
}

// readLoginClientPAT reads the SAME login-client PAT
// backend/internal/modules/iam/loginclient/client.go authenticates its
// Session API calls with in production (docker-compose.dev.yml's zitadel
// service, FirstInstance.Org.LoginClient.PatPath; Makefile's
// ZITADEL_LOGIN_CLIENT_PAT_FILE). verifyPasswordLogin below uses it,
// deliberately, rather than the helivanta-seed-bot IAM_OWNER PAT readMachinePAT
// returns: proving a seeded account authenticates with the SAME credential
// class and the SAME Zitadel v2 Session API Helivanta's own backend uses is a
// closer analog to production than an IAM_OWNER PAT would be, even though
// an IAM_OWNER PAT can call the same endpoint.
export function readLoginClientPAT() {
  try {
    return readFileSync(`${SECRETS_DIR}login-client.pat`, "utf8").trim();
  } catch (err) {
    if (err.code === "ENOENT") {
      throw new Error(
        `${SECRETS_DIR}login-client.pat does not exist — Zitadel writes it at ` +
          `first boot; check 'docker compose -f docker-compose.dev.yml logs zitadel' ` +
          `for a setup failure (the masterkey-length trap is the most likely ` +
          `cause; see docker-compose.dev.yml's zitadel service comment)`,
      );
    }
    throw err;
  }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// managementAPI ISSUER PAT PATH BODY — a machine-authenticated POST against
// Zitadel's v1/v2 management surface. Every caller in this codebase uses
// POST (including "_search" list endpoints, which Zitadel's own API takes
// as POST with a JSON filter body) so this does not need a method
// parameter.
//
// Retries a 503 a bounded number of times: observed live (#838 Task 6)
// that the FIRST call against a just-booted instance — even one whose
// /debug/healthz already answers "ok", which is what dev-infra's wait
// loop polls — can 503 with "connection refused" from Zitadel's own
// gRPC-gateway dialing its not-yet-ready backend internally. The instance
// logs "server is listening" and starts accepting HTTP a moment before
// that internal backend is reachable, so healthz (a shallow check) and
// the management API (which needs the full stack) can disagree for well
// under a second. This is the same shape of gap as the OTHER direction
// already documented on docker-compose.dev.yml's zitadel service — a
// healthcheck lying about readiness — just discovered on first use rather
// than in the spike, and in the opposite direction (falsely ready, not
// falsely unready).
async function managementAPIOnce(issuer, pat, path, body) {
  const res = await fetch(`${issuer}${path}`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${pat}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body ?? {}),
  });
  const json = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(`${path} failed: HTTP ${res.status} ${JSON.stringify(json)}`);
    err.status = res.status;
    throw err;
  }
  return json;
}

export async function managementAPI(issuer, pat, path, body) {
  const maxAttempts = 8;
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      return await managementAPIOnce(issuer, pat, path, body);
    } catch (err) {
      if (err.status !== 503 || attempt === maxAttempts) throw err;
      await sleep(500 * attempt);
    }
  }
}

// setLoginV2BaseUri used to live here as a SEPARATE partial PUT
// (`{loginVersion: {...}}}` only) issued after zitadel-bootstrap.mjs's main
// oidc_config PUT. Removed (#854 Task 7): `PUT .../oidc_config` is a FULL
// REPLACE, not a patch, contrary to what this function's old doc comment
// claimed — any field omitted from a PUT body resets to its default, and
// that includes `authMethodType`, whose default requires a client secret.
// The separate call silently reverted `authMethodType` away from the
// public/PKCE `OIDC_AUTH_METHOD_TYPE_NONE` the main PUT had just set,
// breaking the token exchange (`/oauth/v2/token` →
// `invalid_client: empty client secret`) for every real login on this
// stack, seeded or human, for the lifetime of #854 Tasks 1 through 6 — see
// zitadel-bootstrap.mjs's LOGIN_VERSION comment for the full account of how
// this was found and what replaced it: loginVersion is now set in the SAME
// PUT as every other oidc_config field, so there is no second call left to
// omit anything from.

function b64url(buf) {
  return buf.toString("base64").replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// verifyPasswordLogin proves a seeded account can actually authenticate by
// checking loginName+password against Zitadel's v2 Session API (POST
// /v2/sessions) — the SAME call and the SAME class of credential (the
// login-client PAT, via readLoginClientPAT) Helivanta's own backend uses
// (backend/internal/modules/iam/loginclient/client.go's
// CreatePasswordSession) to check a password on every real sign-in.
//
// This still verifies a REAL credential check, not a status code: POST
// /management/v1/users/human/_import and /v2/users/human both return HTTP
// 200 while silently discarding fields they do not recognise
// (docs/superpowers/spikes/2026-08-15-zitadel-spike.md "Task 0"), so a 200
// from account creation is not evidence the account can authenticate.
// POST /v2/sessions with a password check is direct proof the password
// Zitadel actually stored matches — it succeeds (200, sessionId +
// sessionToken) only when the credential is genuinely correct, distinctly
// fails on a wrong password (400, COMMAND-3M0fs) and on an unknown user
// (404, QUERY-Dfbg2) — both observed live against this stack (2026-08-16)
// — so this function still throws on exactly the cases a broken seed
// should fail on.
//
// Replaces the old browser-driven hostedUILogin/hostedUILoginOnce/
// fillAndSubmit (#854 Task 7, removed — see git history if the old
// Playwright-driven approach is ever needed again). Those drove Zitadel's
// REAL hosted login UI with a headless browser; #854 Task 1 repointed
// helivanta-web's login redirect at Helivanta's own /login instead, which broke both
// of that trio's callers the same way, for two different reasons:
//
//  - scripts/seed-dev.mjs runs during `make up`'s `seed` step, which is
//    BEFORE the web app starts (`up` is `dev-infra seed`, then `dev-api
//    dev-web`) — so the redirect target did not exist yet to drive at all.
//  - scripts/zitadel-verify-login.mjs runs after the whole stack (`make
//    verify-local`), so the redirect target DID exist, but its markup is
//    Helivanta's own one-step Email/Password/Sign-in form, not Zitadel's
//    two-step Loginname→next→Password→continue fillAndSubmit drove.
//
// The Session API sidesteps both: it is a direct HTTP call against
// Zitadel with no redirect through anything Helivanta renders, so it needs
// neither the web app to be up nor to know that app's markup. It also
// drops Playwright, a headless Chromium launch, and ~20s of browser-driven
// waiting per account from both callers.
async function verifyPasswordLoginOnce(issuer, pat, email, password) {
  const res = await fetch(`${issuer}/v2/sessions`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${pat}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      checks: { user: { loginName: email }, password: { password } },
    }),
  });
  const json = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(
      `password check for ${email} failed: HTTP ${res.status} ${JSON.stringify(json)} — ` +
        `the account may not exist, or the password is wrong`,
    );
  }
  if (!json.sessionId || !json.sessionToken) {
    throw new Error(
      `password check for ${email} returned HTTP 200 with no sessionId/sessionToken: ${JSON.stringify(json)}`,
    );
  }
  return { id: json.sessionId, token: json.sessionToken };
}

// verifyPasswordLogin wraps verifyPasswordLoginOnce with a bounded retry: a
// genuinely broken account (wrong password, unknown user) fails every
// attempt identically, so retrying weakens nothing about what is being
// proven — it only absorbs a transient failure against a freshly booted
// Zitadel instance under load from a `make up` also running migrations and
// provisioning in parallel. Mirrors the same judgment
// e2e/tests/support/login.ts's own retry makes, for a different specific
// race.
export async function verifyPasswordLogin(issuer, pat, email, password) {
  const maxAttempts = 2;
  let lastErr;
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      return await verifyPasswordLoginOnce(issuer, pat, email, password);
    } catch (err) {
      lastErr = err;
    }
  }
  throw lastErr;
}

// passwordLoginIDToken completes a REAL, unmocked authorization-code-plus-
// PKCE exchange and returns the resulting ID token — the same property
// hostedUILogin used to guarantee (see verifyPasswordLogin's doc comment on
// why a completed login, not a 200 status, is what proves an account
// works), but driven end to end through Zitadel's own v2 APIs instead of a
// browser:
//
//  1. GET /oauth/v2/authorize with a PKCE challenge, followed manually
//     (redirect: "manual") to read the auth request id off the Location
//     header — this is EXACTLY the request a browser's first hop makes;
//     the difference starts only at step 2.
//  2. POST /v2/sessions with loginName+password (verifyPasswordLoginOnce)
//     — the SAME password check Helivanta's own backend performs, and the SAME
//     one a human typing into Helivanta's /login form triggers.
//  3. POST /v2/oidc/auth_requests/{id} with that session — the SAME
//     "finalize" call backend/internal/modules/iam/loginclient/client.go's
//     (unexported) finalize makes — which returns a callbackUrl carrying a
//     real authorization code, exactly as Helivanta's own POST
//     /v1/auth/login/password does for a real browser.
//  4. POST /oauth/v2/token with that code and the PKCE verifier — the same
//     token exchange step 1's PKCE challenge exists to make possible.
//
// clientId must belong to a PUBLIC client (authMethodType NONE) — the one
// scripts/zitadel-bootstrap.mjs provisions — since step 4 performs PKCE
// with no client secret, exactly as a browser would. loginClientPAT
// authenticates steps 2 and 3 (readLoginClientPAT) — this is a machine
// credential, not the end user's, exactly as it is for Helivanta's real login
// flow.
export async function passwordLoginIDToken({
  issuer,
  clientId,
  loginClientPAT,
  redirectUri,
  email,
  password,
}) {
  const verifier = b64url(crypto.randomBytes(32));
  const challenge = b64url(crypto.createHash("sha256").update(verifier).digest());
  const state = b64url(crypto.randomBytes(16));

  const authUrl = new URL(`${issuer}/oauth/v2/authorize`);
  authUrl.searchParams.set("client_id", clientId);
  authUrl.searchParams.set("redirect_uri", redirectUri);
  authUrl.searchParams.set("response_type", "code");
  authUrl.searchParams.set("scope", "openid profile email");
  authUrl.searchParams.set("code_challenge", challenge);
  authUrl.searchParams.set("code_challenge_method", "S256");
  authUrl.searchParams.set("state", state);

  const authorizeRes = await fetch(authUrl.toString(), { redirect: "manual" });
  const location = authorizeRes.headers.get("location");
  const authRequestID = location && new URL(location, issuer).searchParams.get("authRequest");
  if (!authRequestID) {
    throw new Error(
      `GET /oauth/v2/authorize did not redirect with an authRequest id: ` +
        `HTTP ${authorizeRes.status}, Location=${location ?? "(none)"}`,
    );
  }

  const session = await verifyPasswordLoginOnce(issuer, loginClientPAT, email, password);

  const finalizeRes = await fetch(
    `${issuer}/v2/oidc/auth_requests/${encodeURIComponent(authRequestID)}`,
    {
      method: "POST",
      headers: {
        Authorization: `Bearer ${loginClientPAT}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        session: { sessionId: session.id, sessionToken: session.token },
      }),
    },
  );
  const finalizeJson = await finalizeRes.json().catch(() => ({}));
  if (!finalizeRes.ok || !finalizeJson.callbackUrl) {
    throw new Error(
      `finalizing auth request ${authRequestID} for ${email} failed: ` +
        `HTTP ${finalizeRes.status} ${JSON.stringify(finalizeJson)}`,
    );
  }

  const callbackUrl = new URL(finalizeJson.callbackUrl);
  const code = callbackUrl.searchParams.get("code");
  if (!code) {
    throw new Error(
      `callbackUrl for ${email} carried no authorization code: ${finalizeJson.callbackUrl}`,
    );
  }

  const tokenRes = await fetch(`${issuer}/oauth/v2/token`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "authorization_code",
      code,
      redirect_uri: redirectUri,
      client_id: clientId,
      code_verifier: verifier,
    }),
  });
  const tokenJson = await tokenRes.json();
  if (!tokenRes.ok) {
    throw new Error(
      `token exchange for ${email} failed: HTTP ${tokenRes.status} ${JSON.stringify(tokenJson)}`,
    );
  }
  if (!tokenJson.id_token) {
    throw new Error(
      `token exchange for ${email} returned no id_token: ${JSON.stringify(tokenJson)}`,
    );
  }
  return tokenJson.id_token;
}
