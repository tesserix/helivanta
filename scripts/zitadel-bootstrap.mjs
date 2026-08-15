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
  managementAPI,
  readMachinePAT,
} from "./lib/zitadel.mjs";

const ISSUER = process.env.ZITADEL_ISSUER_URL ?? "http://localhost:20080";
const SECRETS_DIR = fileURLToPath(new URL("../dev/zitadel/secrets/", import.meta.url));
const ENV_OUT_PATH = `${SECRETS_DIR}zitadel.env`;

const ORG_NAME = "HMS";
const PROJECT_NAME = "HMS";
const APP_NAME = "hms-web";

// Placeholder redirect URIs (see lib/zitadel.mjs's DEV_REDIRECT_URI doc
// comment): apps/shell has not been ported off Firebase yet, so nothing
// calls back to these today. They are provisioned now, rather than left
// for the frontend task to discover missing, because the OIDC app itself
// (D1's "what to provision" table) is this task's job — the frontend task
// registers the real callback route and, if it differs, updates these via
// the Console or a follow-up to this script.
const REDIRECT_URIS = [DEV_REDIRECT_URI];
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
  if (app) {
    const detail = await getApp(pat, project.id, app.id);
    clientId = detail.app?.oidcConfig?.clientId;
    console.log(`Reusing existing app "${APP_NAME}" (client_id=${clientId})`);
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
    console.log(`Created app "${APP_NAME}" (client_id=${clientId})`);
  }

  if (!clientId) {
    throw new Error(
      `could not resolve a client_id for app "${APP_NAME}" — the app exists ` +
        `but its OIDC config did not come back as expected; check the Console`,
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
