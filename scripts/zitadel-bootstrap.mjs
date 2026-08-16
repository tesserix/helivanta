// Provisions HMS's Zitadel topology (tenancy topology design doc D1: one
// org, one project, one OIDC app) against the local dev stack, then writes
// dev/zitadel/secrets/zitadel.env so the Makefile can hand
// backend/internal/config/config.go a real ZITADEL_CLIENT_ID.
//
// Run by `make dev-infra`, after docker-compose.dev.yml's zitadel service
// reports healthy. Everything here is driven by the hms-seed-bot machine
// PAT that Zitadel itself wrote to dev/zitadel/secrets/hms-seed.pat via
// FirstInstance.Org.Machine + PatPath at first boot — NO interactive
// login, scripted or otherwise, is needed to reach this point. That closes
// the one gap docs/superpowers/spikes/2026-08-15-zitadel-spike.md left
// "still NOT VERIFIED": a fully non-interactive path to the first machine
// PAT. Verified for #838 Task 6 by actually driving this declarative
// bootstrap end to end against a real v4.15.3 instance — see the PR body.
//
// Idempotent: re-running (e.g. every `make up`) finds the existing
// project/app by name and reuses them rather than erroring or duplicating,
// the same convention scripts/seed-dev.mjs already uses for re-seeding.
import { mkdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import {
  DEV_POST_LOGOUT_REDIRECT_URI,
  DEV_REDIRECT_URI,
  DEV_SILENT_RENEW_REDIRECT_URI,
  managementAPI,
  readMachinePAT,
} from "./lib/zitadel.mjs";

const ISSUER = process.env.ZITADEL_ISSUER_URL ?? "http://localhost:20080";
const SECRETS_DIR = fileURLToPath(new URL("../dev/zitadel/secrets/", import.meta.url));
const ENV_OUT_PATH = `${SECRETS_DIR}zitadel.env`;

const ORG_NAME = "HMS";
const PROJECT_NAME = "HMS";
const APP_NAME = "hms-web";

// The real redirect URIs #838 Task 7 (apps/shell) serves:
// app/api/auth/callback/page.tsx for the full redirect flow,
// app/api/auth/silent-renew/page.tsx for the hidden-iframe renewal flow
// (design spec D4a).
const REDIRECT_URIS = [DEV_REDIRECT_URI, DEV_SILENT_RENEW_REDIRECT_URI];
const POST_LOGOUT_REDIRECT_URIS = [DEV_POST_LOGOUT_REDIRECT_URI];

// hms-web's login redirect points at HMS's own login page instead of
// Zitadel's stock hosted UI (#854 Task 1) — scoped to this ONE app via
// Zitadel's per-app `loginVersion.loginV2.baseUri`, deliberately not the
// instance-wide setting, because this Zitadel instance is shared with
// every other Tesserix product and the instance-wide setting would move
// login for all of them (see docker-compose.dev.yml's
// ZITADEL_DEFAULTINSTANCE_FEATURES_LOGINV2_REQUIRED comment for why that
// instance-wide flag must stay "false" for this per-app setting to take
// effect at all). baseUri must be an ORIGIN WITH NO PATH — Zitadel appends
// "/login" itself when building the redirect (confirmed live: baseUri
// "http://localhost:4301/login" produced a redirect to
// ".../login/login?authRequest=…", a double path segment).
//
// #854 Task 7, CRITICAL: this used to be set via its own SEPARATE PUT
// carrying only `{loginVersion: {...}}}`, issued after the main
// oidc_config PUT/create below. That broke every real login on this stack
// silently for the lifetime of #854 Tasks 1 through 6: Zitadel's
// `PUT .../oidc_config` is a FULL REPLACE, not a patch — any field omitted
// from the body resets to its default, and `authMethodType`'s default
// requires a client secret. The main PUT/create below explicitly sets
// authMethodType to OIDC_AUTH_METHOD_TYPE_NONE (the public/PKCE client
// hms-web needs — see its own comment), but the separate loginVersion-only
// PUT that ran AFTER it silently reset authMethodType back away from NONE
// every single `make up`, breaking the token exchange
// (`/oauth/v2/token` → `invalid_client: empty client secret`) for every
// login, seeded or human. `GET`ting the config back afterward looked fine
// — protojson elides `authMethodType` from the response whenever it holds
// certain values, the same elision trap
// backend/internal/modules/iam/loginclient/client.go's LoginPolicy doc
// comment documents for `forceMfa` — so nothing about reading the config
// back revealed the break; only actually driving a token exchange did
// (scripts/zitadel-verify-login.mjs, which is exactly what surfaced this).
//
// The fix is structural, not a fixed ordering: loginVersion is now part of
// the SAME PUT/create call that sets every other field, so there is no
// second call to omit anything from and nothing left to clobber. Do not
// reintroduce a separate call for this — if a genuinely separate update is
// ever unavoidable, it MUST read the full current config first (remembering
// GET can itself elide authMethodType) and resend every field, not just
// the one being changed.
const LOGIN_VERSION = { loginV2: { baseUri: new URL(DEV_REDIRECT_URI).origin } };

async function findProjectByName(pat, name) {
  const { result } = await managementAPI(ISSUER, pat, "/management/v1/projects/_search", {});
  return (result ?? []).find((p) => p.name === name) ?? null;
}

async function findAppByName(pat, projectId, name) {
  const { result } = await managementAPI(
    ISSUER,
    pat,
    `/management/v1/projects/${projectId}/apps/_search`,
    {},
  );
  return (result ?? []).find((a) => a.name === name) ?? null;
}

// _search does not return oidcConfig (just id/name/state), so reusing an
// existing app needs a second GET for its clientId.
async function getApp(pat, projectId, appId) {
  const res = await fetch(
    `${ISSUER}/management/v1/projects/${projectId}/apps/${appId}`,
    { headers: { Authorization: `Bearer ${pat}` } },
  );
  const json = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(
      `fetch app ${appId} failed: HTTP ${res.status} ${JSON.stringify(json)}`,
    );
  }
  return json;
}

async function main() {
  mkdirSync(SECRETS_DIR, { recursive: true });
  const pat = readMachinePAT();

  let project = await findProjectByName(pat, PROJECT_NAME);
  if (project) {
    console.log(`Reusing existing project "${PROJECT_NAME}" (${project.id})`);
  } else {
    const created = await managementAPI(ISSUER, pat, "/management/v1/projects", { name: PROJECT_NAME });
    project = { id: created.id };
    console.log(`Created project "${PROJECT_NAME}" (${project.id})`);
  }

  let app = await findAppByName(pat, project.id, APP_NAME);
  let clientId;
  let appId;
  if (app) {
    appId = app.id;
    const detail = await getApp(pat, project.id, app.id);
    clientId = detail.app?.oidcConfig?.clientId;
    console.log(`Reusing existing app "${APP_NAME}" (client_id=${clientId})`);
  } else {
    // Created bare (no oidc_config fields beyond the minimum this endpoint
    // accepts) — the single full PUT below, run unconditionally for BOTH
    // branches, is what actually sets every field including loginVersion.
    // See that PUT's own comment for why this is now the ONLY place any
    // oidc_config field gets set, and why that matters.
    const created = await managementAPI(ISSUER, pat, `/management/v1/projects/${project.id}/apps/oidc`, {
      name: APP_NAME,
      redirectUris: REDIRECT_URIS,
      responseTypes: ["OIDC_RESPONSE_TYPE_CODE"],
      grantTypes: ["OIDC_GRANT_TYPE_AUTHORIZATION_CODE"],
      appType: "OIDC_APP_TYPE_WEB",
      // Public client, PKCE only — matches spec D5a: the browser talks to
      // Zitadel directly, HMS's own session (not this OIDC client) is
      // what protects /v1 routes, so there is no confidential secret to
      // hold or rotate for hms-web.
      authMethodType: "OIDC_AUTH_METHOD_TYPE_NONE",
      postLogoutRedirectUris: POST_LOGOUT_REDIRECT_URIS,
      devMode: true,
    });
    clientId = created.clientId;
    appId = created.appId;
    console.log(`Created app "${APP_NAME}" (client_id=${clientId})`);
  }

  if (!clientId) {
    throw new Error(
      `could not resolve a client_id for app "${APP_NAME}" — the app exists ` +
        `but its OIDC config did not come back as expected; check the Console`,
    );
  }

  // The ONE call that sets every oidc_config field this app needs,
  // including loginVersion — see LOGIN_VERSION's doc comment above for why
  // this must never again be split into a separate call. Runs
  // unconditionally, every `make up`, for both a freshly created app (the
  // create call above accepts no loginVersion field at all — Login V2's
  // per-app baseUri is only settable through oidc_config) and a reused
  // one (#838 Task 7 added DEV_SILENT_RENEW_REDIRECT_URI after some stacks
  // may already have been provisioned without it; updating unconditionally
  // is what makes an older stack pick it up without a manual reset).
  //
  // PUT, not POST: unlike every other call in this file (create/_search,
  // all genuinely POST per lib/zitadel.mjs's managementAPI doc comment),
  // Zitadel's oidc_config update is a PUT. Raw fetch here rather than
  // bending managementAPI's hardcoded POST to fit.
  const updateRes = await fetch(
    `${ISSUER}/management/v1/projects/${project.id}/apps/${appId}/oidc_config`,
    {
      method: "PUT",
      headers: { Authorization: `Bearer ${pat}`, "Content-Type": "application/json" },
      body: JSON.stringify({
        redirectUris: REDIRECT_URIS,
        responseTypes: ["OIDC_RESPONSE_TYPE_CODE"],
        grantTypes: ["OIDC_GRANT_TYPE_AUTHORIZATION_CODE"],
        appType: "OIDC_APP_TYPE_WEB",
        authMethodType: "OIDC_AUTH_METHOD_TYPE_NONE",
        postLogoutRedirectUris: POST_LOGOUT_REDIRECT_URIS,
        devMode: true,
        loginVersion: LOGIN_VERSION,
      }),
    },
  );
  if (!updateRes.ok) {
    const body = await updateRes.json().catch(() => ({}));
    // Zitadel 400s this PUT with COMMAND-1m88i ("No changes") when every
    // field in the body already matches what is persisted — reachable on
    // every `make up` after the first, once this call has already put the
    // config exactly where it belongs. That is success, not a failure: the
    // whole point of this PUT is the config ending up in this state, and
    // it already has. Any OTHER 400 (or non-400) is a real failure and
    // still throws.
    const errID = body?.details?.[0]?.id;
    if (updateRes.status === 400 && errID === "COMMAND-1m88i") {
      console.log(`oidc_config for "${APP_NAME}" already matches (no changes)`);
    } else {
      throw new Error(
        `updating oidc_config for app "${APP_NAME}" failed: HTTP ${updateRes.status} ${JSON.stringify(body)}`,
      );
    }
  } else {
    console.log(
      `Set oidc_config (authMethodType=NONE, loginVersion.loginV2.baseUri=${LOGIN_VERSION.loginV2.baseUri}) for "${APP_NAME}"`,
    );
  }

  writeFileSync(ENV_OUT_PATH, `ZITADEL_CLIENT_ID=${clientId}\n`);
  console.log(`Wrote ${ENV_OUT_PATH}`);
  console.log(`
Zitadel provisioned:
  Issuer:        ${ISSUER}
  Org:           ${ORG_NAME}
  Project:       ${PROJECT_NAME}
  App:           ${APP_NAME} (client_id=${clientId})
  Console admin: admin@hms.localhost / HmsDevAdminPassw0rd! (dev only — see
                 docker-compose.dev.yml's zitadel service)
`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
