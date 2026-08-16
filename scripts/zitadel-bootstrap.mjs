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
  setLoginV2BaseUri,
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
    // #838 Task 7 added DEV_SILENT_RENEW_REDIRECT_URI after this app may
    // already have been provisioned by an earlier `make dev-infra` run
    // (Task 6's stock still only registered DEV_REDIRECT_URI). Updating
    // unconditionally, every run, rather than only when a new app is
    // created, is what makes a stack provisioned before this change pick
    // up the silent-renew URI without a manual reset — the alternative
    // (leaving an older dev stack permanently missing it) fails
    // "redirect_uri not allowed" only the first time signinSilent() is
    // ever called, which is exactly the kind of error that looks like a
    // frontend bug rather than a stale provisioning run.
    //
    // PUT, not POST: unlike every other call in this file (create/_search,
    // all genuinely POST per lib/zitadel.mjs's managementAPI doc comment),
    // Zitadel's oidc_config update is a PUT. Raw fetch here rather than
    // bending managementAPI's hardcoded POST to fit.
    const updateRes = await fetch(
      `${ISSUER}/management/v1/projects/${project.id}/apps/${app.id}/oidc_config`,
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
        }),
      },
    );
    if (!updateRes.ok) {
      const body = await updateRes.json().catch(() => ({}));
      throw new Error(
        `updating oidc_config for app "${APP_NAME}" failed: HTTP ${updateRes.status} ${JSON.stringify(body)}`,
      );
    }
  } else {
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

  // Point hms-web's login redirect at HMS's own login page instead of
  // Zitadel's stock hosted UI (#854 Task 1) — scoped to this ONE app via
  // Zitadel's per-app `loginVersion.loginV2.baseUri`, deliberately not the
  // instance-wide setting, because this Zitadel instance is shared with
  // every other Tesserix product and the instance-wide setting would move
  // login for all of them. Runs unconditionally, every `make up`, same as
  // the oidc_config update above — see setLoginV2BaseUri's doc comment in
  // lib/zitadel.mjs for why this needs
  // ZITADEL_DEFAULTINSTANCE_FEATURES_LOGINV2_REQUIRED=false (above, in
  // docker-compose.dev.yml) to take effect at all, and why the origin
  // passed here must have no path.
  await setLoginV2BaseUri(ISSUER, pat, project.id, appId, new URL(DEV_REDIRECT_URI).origin);
  console.log(`Set loginVersion.loginV2.baseUri=${new URL(DEV_REDIRECT_URI).origin} for "${APP_NAME}"`);

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
