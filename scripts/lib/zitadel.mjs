// Shared Zitadel helpers for scripts/seed-dev.mjs, scripts/zitadel-bootstrap.mjs
// and scripts/zitadel-verify-login.mjs — one place holding the machine-API
// call convention and the real hosted-UI login drive, so the three scripts
// cannot drift on how either is done.
import crypto from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const SECRETS_DIR = fileURLToPath(new URL("../../dev/zitadel/secrets/", import.meta.url));

// The redirect_uris registered on the hms-web app (scripts/zitadel-
// bootstrap.mjs). apps/shell/app/api/auth/callback/page.tsx and
// apps/shell/app/api/auth/silent-renew/page.tsx (#838 Task 7) are the
// real handlers for the first two; hostedUILogin below still captures the
// authorization code from the request itself rather than waiting for a
// navigation, since that is more robust for a scripted login than relying
// on the shell app being reachable at all during dev-infra/seeding.
export const DEV_REDIRECT_URI = "http://localhost:4301/api/auth/callback";
export const DEV_SILENT_RENEW_REDIRECT_URI = "http://localhost:4301/api/auth/silent-renew";
export const DEV_POST_LOGOUT_REDIRECT_URI = "http://localhost:4301/login";

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
          `so scripts/zitadel-bootstrap.mjs can provision the HMS app`,
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

// readMachinePAT reads the hms-seed-bot IAM_OWNER PAT Zitadel itself wrote
// at first boot (docker-compose.dev.yml's zitadel service,
// FirstInstance.Org.Machine.Pat) — the credential scripts/seed-dev.mjs and
// scripts/zitadel-bootstrap.mjs use for every management-API call.
export function readMachinePAT() {
  try {
    return readFileSync(`${SECRETS_DIR}hms-seed.pat`, "utf8").trim();
  } catch (err) {
    if (err.code === "ENOENT") {
      throw new Error(
        `${SECRETS_DIR}hms-seed.pat does not exist — Zitadel writes it at first ` +
          `boot; check 'docker compose -f docker-compose.dev.yml logs zitadel' ` +
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

function b64url(buf) {
  return buf.toString("base64").replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// fillAndSubmit fills labelPattern with value and clicks buttonPattern,
// re-filling and retrying the click a bounded number of times if the
// button stays disabled. Observed live (#838 Task 6, seeding 14 accounts
// in one run): the first hosted-UI login against a freshly launched
// browser succeeds first try, but a second login moments later in the
// SAME browser can hit the submit button still disabled 30s later — the
// login UI's own JS had not finished attaching its input handler to a
// freshly-navigated page yet, so Playwright's fill() set the value before
// anything was listening for it. Re-filling once hydration has caught up
// resolves it; this is a UI-timing race, not a wrong selector or a wrong
// credential (both of which fail every attempt, not just the retries).
async function fillAndSubmit(page, labelPattern, value, buttonPattern) {
  const field = page.getByLabel(labelPattern).first();
  const button = page.getByRole("button", { name: buttonPattern }).first();
  await field.waitFor({ state: "visible", timeout: 15_000 });

  const attempts = 3;
  for (let attempt = 1; attempt <= attempts; attempt++) {
    await field.fill(value);
    try {
      await button.click({ timeout: 10_000 });
      return;
    } catch (err) {
      if (attempt === attempts) throw err;
    }
  }
}

// hostedUILogin drives the REAL Zitadel-hosted login UI with a headless
// browser and returns the resulting ID token — a completed authorization-
// code-plus-PKCE exchange, not a management-API shortcut. This is
// deliberate and load-bearing (#838 Task 6, and the plan's explicit
// instruction): POST /management/v1/users/human/_import and
// /v2/users/human both return HTTP 200 while silently discarding fields
// they do not recognise (docs/superpowers/spikes/2026-08-15-zitadel-spike.md
// "Task 0"), so a 200 status code is not evidence a seeded account can
// actually authenticate. Only a completed login proves that.
//
// clientId must belong to a PUBLIC client (authMethodType NONE) — the one
// scripts/zitadel-bootstrap.mjs provisions — since this performs PKCE with
// no client secret, exactly as a browser would.
//
// browser is optional: a caller verifying many accounts in one run (see
// scripts/seed-dev.mjs) can launch one Chromium instance and pass it to
// every call, rather than paying process-launch cost per account — with
// 14 accounts as of this writing (2 human + 2 per e2e spec file), that
// launch cost is the difference between a few seconds and the better part
// of a minute. Left unset, hostedUILogin launches and closes its own (the
// single-call case: scripts/zitadel-verify-login.mjs).
//
// Retries once on any failure. Observed live (#838 Task 6): the same
// account, same script, back-to-back runs, one succeeds and the very next
// attempt times out waiting for the code redirect under a loaded dev
// machine (four `next dev` servers, five-plus Docker containers, other Go
// tests running) — a slow page load or slow submit, not a broken account
// (a genuinely broken account fails EVERY attempt, retried or not). This
// mirrors e2e/tests/support/login.ts's own retry, which exists for a
// different specific race (the revocation watermark) but is the same
// judgment call: retrying a real login is legitimate here because success
// is still a complete, unmocked authorization-code-plus-PKCE exchange —
// nothing about what is being proven weakens on a retry.
export async function hostedUILogin(opts) {
  const maxAttempts = 2;
  let lastErr;
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      return await hostedUILoginOnce(opts);
    } catch (err) {
      lastErr = err;
    }
  }
  throw lastErr;
}

async function hostedUILoginOnce({ issuer, clientId, redirectUri, email, password, browser }) {
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

  const ownBrowser = browser ?? (await chromium.launch({ headless: true }));
  let code = null;
  try {
    const page = await ownBrowser.newPage();
    // The redirect to redirectUri is the proof, captured here rather than
    // waited for as a navigation: the callback route does not exist yet
    // (apps/shell is still Firebase-based, #838 Task 6 is dev stack/
    // seeding only), so letting Chromium actually navigate there would
    // hit a connection error. The request event fires before that happens.
    const gotCode = new Promise((resolve) => {
      page.on("request", (req) => {
        if (req.url().startsWith(redirectUri)) {
          resolve(new URL(req.url()).searchParams.get("code"));
        }
      });
    });

    try {
      await page.goto(authUrl.toString(), { waitUntil: "domcontentloaded", timeout: 25_000 });
      await fillAndSubmit(page, /Loginname|Email|Login Name/i, email, /next|continue/i);
      await page.waitForURL(/\/password\?/, { timeout: 20_000 });
      await fillAndSubmit(page, /Password/i, password, /continue/i);
      code = await Promise.race([
        gotCode,
        new Promise((resolve) => setTimeout(() => resolve(null), 20_000)),
      ]);
    } finally {
      await page.close();
    }
  } finally {
    if (!browser) await ownBrowser.close();
  }

  if (!code) {
    throw new Error(
      `hosted-UI login for ${email} never reached ${redirectUri} with an authorization code — ` +
        `the account may not exist, the password may be wrong, or the login UI's selectors drifted`,
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
    throw new Error(`token exchange for ${email} failed: HTTP ${tokenRes.status} ${JSON.stringify(tokenJson)}`);
  }
  if (!tokenJson.id_token) {
    throw new Error(`token exchange for ${email} returned no id_token: ${JSON.stringify(tokenJson)}`);
  }
  return tokenJson.id_token;
}
