// Provisions Helivanta's Zitadel topology (tenancy topology design doc D1: one
// org, one project, one OIDC app) against the local dev stack, then writes
// dev/zitadel/secrets/zitadel.env so the Makefile can hand
// backend/internal/config/config.go a real ZITADEL_CLIENT_ID.
//
// Run by `make dev-infra`, after docker-compose.dev.yml's zitadel service
// reports healthy. Everything here is driven by the helivanta-seed-bot machine
// PAT that Zitadel itself wrote to dev/zitadel/secrets/helivanta-seed.pat via
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
//
// #848 Task 8 added a SECOND app, helivanta-web-idle-timeout: the idle-timeout
// e2e spec needs its own shell instance on its own port (so it can run
// against a short-IDLE_TIMEOUT API without weakening the 15-minute
// default every other spec and production rely on — see the Makefile's
// "Idle-timeout e2e fixture" comment), and Zitadel's per-app
// `loginVersion.loginV2.baseUri` (see LOGIN_VERSION's doc comment below)
// is a single origin — pointing helivanta-web's own baseUri at a second port
// would make EVERY login, from EVERY app, render its interactive step on
// whichever port won last. A second app, with its own baseUri and its
// own redirect URIs, is the only way to keep the two isolated. Both apps
// share the same org/project — only their OIDC app registration differs.
import { mkdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { managementAPI, readMachinePAT } from "./lib/zitadel.mjs";

const ISSUER = process.env.ZITADEL_ISSUER_URL ?? "http://auth.tesserix.localhost:20080";

// The app's host in dev. NOT "localhost" (#916 Task 4, design spec D6):
// production serves the app at helivanta.app and this IdP at
// auth.tesserix.app — different registrable domains, so every browser
// request from app to IdP is cross-site. The old localhost:4301 /
// localhost:20080 pair shared a site (ports are not part of a site), which
// is exactly why no test could catch #916. Overridable to match the
// Makefile's HELIVANTA_WEB_HOST; every redirect URI below is built from it
// so the registration cannot drift from where the dev server actually
// serves.
const WEB_HOST = process.env.HELIVANTA_WEB_HOST ?? "helivanta.localhost";
const webOrigin = (port) => `http://${WEB_HOST}:${port}`;
const SECRETS_DIR = fileURLToPath(new URL("../dev/zitadel/secrets/", import.meta.url));

const ORG_NAME = "Helivanta";
const PROJECT_NAME = "Helivanta";

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
  const res = await fetch(`${ISSUER}/management/v1/projects/${projectId}/apps/${appId}`, {
    headers: { Authorization: `Bearer ${pat}` },
  });
  const json = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(`fetch app ${appId} failed: HTTP ${res.status} ${JSON.stringify(json)}`);
  }
  return json;
}

// Provisions (or reuses) ONE OIDC app in the given project and writes its
// client_id to envOutPath as ZITADEL_CLIENT_ID=... — the shape every
// consumer (Makefile, scripts/seed-dev.mjs's readClientID) already
// expects, so helivanta-web-idle-timeout's separate secrets file is a drop-in
// the same way helivanta-web's own zitadel.env already is.
async function provisionApp(
  pat,
  project,
  { appName, redirectUris, postLogoutRedirectUris, envOutPath },
) {
  // helivanta-web's login redirect points at Helivanta's own login page instead of
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
  const loginVersion = { loginV2: { baseUri: new URL(redirectUris[0]).origin } };

  let app = await findAppByName(pat, project.id, appName);
  let clientId;
  let appId;
  if (app) {
    appId = app.id;
    const detail = await getApp(pat, project.id, app.id);
    clientId = detail.app?.oidcConfig?.clientId;
    console.log(`Reusing existing app "${appName}" (client_id=${clientId})`);
  } else {
    // Created bare (no oidc_config fields beyond the minimum this endpoint
    // accepts) — the single full PUT below, run unconditionally for BOTH
    // branches, is what actually sets every field including loginVersion.
    // See that PUT's own comment for why this is now the ONLY place any
    // oidc_config field gets set, and why that matters.
    const created = await managementAPI(
      ISSUER,
      pat,
      `/management/v1/projects/${project.id}/apps/oidc`,
      {
        name: appName,
        redirectUris,
        responseTypes: ["OIDC_RESPONSE_TYPE_CODE"],
        grantTypes: ["OIDC_GRANT_TYPE_AUTHORIZATION_CODE"],
        appType: "OIDC_APP_TYPE_WEB",
        // Public client, PKCE only — matches spec D5a: the browser talks to
        // Zitadel directly, Helivanta's own session (not this OIDC client) is
        // what protects /v1 routes, so there is no confidential secret to
        // hold or rotate for helivanta-web.
        authMethodType: "OIDC_AUTH_METHOD_TYPE_NONE",
        postLogoutRedirectUris,
        devMode: true,
      },
    );
    clientId = created.clientId;
    appId = created.appId;
    console.log(`Created app "${appName}" (client_id=${clientId})`);
  }

  if (!clientId) {
    throw new Error(
      `could not resolve a client_id for app "${appName}" — the app exists ` +
        `but its OIDC config did not come back as expected; check the Console`,
    );
  }

  // The ONE call that sets every oidc_config field this app needs,
  // including loginVersion.
  //
  // #854 Task 7, CRITICAL: this used to be set via its own SEPARATE PUT
  // carrying only `{loginVersion: {...}}}`, issued after the main
  // oidc_config PUT/create above. That broke every real login on this stack
  // silently for the lifetime of #854 Tasks 1 through 6: Zitadel's
  // `PUT .../oidc_config` is a FULL REPLACE, not a patch — any field omitted
  // from the body resets to its default, and `authMethodType`'s default
  // requires a client secret. The main PUT/create above explicitly sets
  // authMethodType to OIDC_AUTH_METHOD_TYPE_NONE (the public/PKCE client
  // helivanta-web needs — see its own comment), but a separate loginVersion-only
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
  // The fix is structural, not a fixed ordering: loginVersion is part of
  // the SAME PUT/create call that sets every other field, so there is no
  // second call to omit anything from and nothing left to clobber. Do not
  // reintroduce a separate call for this — if a genuinely separate update is
  // ever unavoidable, it MUST read the full current config first (remembering
  // GET can itself elide authMethodType) and resend every field, not just
  // the one being changed.
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
        redirectUris,
        responseTypes: ["OIDC_RESPONSE_TYPE_CODE"],
        grantTypes: ["OIDC_GRANT_TYPE_AUTHORIZATION_CODE"],
        appType: "OIDC_APP_TYPE_WEB",
        authMethodType: "OIDC_AUTH_METHOD_TYPE_NONE",
        postLogoutRedirectUris,
        devMode: true,
        loginVersion,
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
      console.log(`oidc_config for "${appName}" already matches (no changes)`);
    } else {
      throw new Error(
        `updating oidc_config for app "${appName}" failed: HTTP ${updateRes.status} ${JSON.stringify(body)}`,
      );
    }
  } else {
    console.log(
      `Set oidc_config (authMethodType=NONE, loginVersion.loginV2.baseUri=${loginVersion.loginV2.baseUri}) for "${appName}"`,
    );
  }

  writeFileSync(envOutPath, `ZITADEL_CLIENT_ID=${clientId}\n`);
  console.log(`Wrote ${envOutPath}`);
  return clientId;
}

async function main() {
  mkdirSync(SECRETS_DIR, { recursive: true });
  const pat = readMachinePAT();

  let project = await findProjectByName(pat, PROJECT_NAME);
  if (project) {
    console.log(`Reusing existing project "${PROJECT_NAME}" (${project.id})`);
  } else {
    const created = await managementAPI(ISSUER, pat, "/management/v1/projects", {
      name: PROJECT_NAME,
    });
    project = { id: created.id };
    console.log(`Created project "${PROJECT_NAME}" (${project.id})`);
  }

  // helivanta-web: the real app every human developer and every e2e spec other
  // than idle-timeout.spec.ts signs in through.
  //
  // The real redirect URI #838 Task 7 (apps/shell) serves:
  // app/api/auth/callback/page.tsx for the full redirect flow. There
  // used to be a second one, app/api/auth/silent-renew/page.tsx, for a
  // hidden-iframe renewal flow (design spec D4a) — deleted by #916
  // (design spec D1): that flow depended on Zitadel's SameSite=Lax
  // session cookie travelling on a cross-site iframe request, which it
  // never did. Renewal is now a same-origin POST /v1/auth/renew that
  // never touches Zitadel through the browser at all (lib/renew.ts), so
  // it needs no redirect_uri here.
  const clientId = await provisionApp(pat, project, {
    appName: "helivanta-web",
    redirectUris: [`${webOrigin(4301)}/api/auth/callback`],
    postLogoutRedirectUris: [`${webOrigin(4301)}/login`],
    envOutPath: `${SECRETS_DIR}zitadel.env`,
  });

  // helivanta-web-idle-timeout: idle-timeout.spec.ts's OWN shell instance
  // (Makefile's dev-web-idle-timeout, port HELIVANTA_IDLE_WEB_PORT). See this
  // file's header comment for why a short-IDLE_TIMEOUT spec needs a
  // wholly separate OIDC app rather than reusing helivanta-web's.
  const idleTimeoutClientId = await provisionApp(pat, project, {
    appName: "helivanta-web-idle-timeout",
    redirectUris: [`${webOrigin(4399)}/api/auth/callback`],
    postLogoutRedirectUris: [`${webOrigin(4399)}/login`],
    envOutPath: `${SECRETS_DIR}zitadel-idle-timeout.env`,
  });

  // helivanta-web-renewal: session-renewal.spec.ts's OWN shell instance
  // (Makefile's dev-web-renewal, port HELIVANTA_RENEWAL_WEB_PORT), against an
  // API booted with a short SESSION_TTL. Same reasoning as
  // helivanta-web-idle-timeout above — a per-app loginV2 baseUri is a single
  // origin, so a third fixture on a third port needs a third app rather than
  // borrowing either of the other two's registration.
  const renewalClientId = await provisionApp(pat, project, {
    appName: "helivanta-web-renewal",
    redirectUris: [`${webOrigin(4398)}/api/auth/callback`],
    postLogoutRedirectUris: [`${webOrigin(4398)}/login`],
    envOutPath: `${SECRETS_DIR}zitadel-renewal.env`,
  });

  console.log(`
Zitadel provisioned:
  Issuer:        ${ISSUER}
  Org:           ${ORG_NAME}
  Project:       ${PROJECT_NAME}
  App:           helivanta-web (client_id=${clientId})
  App:           helivanta-web-idle-timeout (client_id=${idleTimeoutClientId})
  App:           helivanta-web-renewal (client_id=${renewalClientId})
  Console admin: admin@helivanta.localhost / HmsDevAdminPassw0rd! (dev only — see
                 docker-compose.dev.yml's zitadel service)
`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
