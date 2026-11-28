import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { expect, test } from "@playwright/test";

import { ZITADEL_ORIGIN } from "./support/hosts";
import { login } from "./support/login";
import { readStoredIdToken } from "./support/stored-session";

// #915 spike 1 (docs/superpowers/specs/2026-11-20-server-side-oidc-handshake-design.md):
// server-side sign-out needs the id_token's `sid` to be the v2 session id,
// and the login-client PAT alone to be allowed to delete that session. The
// existing loginclient.DeleteSession also sends the session's own token,
// which Helivanta never holds for a session the OIDC flow finished, so this
// deletes WITHOUT it. If this fails, the spec's fallback (a server-built
// end-session redirect) applies instead.
function loginClientPAT() {
  return readFileSync(
    resolve(__dirname, "../../dev/zitadel/secrets/login-client.pat"),
    "utf8",
  ).trim();
}

async function zitadel(method: string, path: string) {
  return fetch(`${ZITADEL_ORIGIN}${path}`, {
    method,
    redirect: "error",
    signal: AbortSignal.timeout(10_000),
    headers: { Authorization: `Bearer ${loginClientPAT()}`, "Content-Type": "application/json" },
    ...(method === "DELETE" ? { body: "{}" } : {}),
  });
}

test("the id_token's sid is the v2 session, and the login-client PAT alone can end it", async ({
  page,
}) => {
  await login(page);
  const token = await readStoredIdToken(page);
  expect(token, "the current flow still stores an id_token").toBeTruthy();
  const claims = JSON.parse(Buffer.from(token!.split(".")[1]!, "base64url").toString()) as {
    sub: string;
    sid?: string;
  };
  expect(typeof claims.sid, `id_token claims: ${Object.keys(claims).join(",")}`).toBe("string");

  const read = await zitadel("GET", `/v2/sessions/${claims.sid}`);
  expect(read.status, "the login-client PAT can read the OIDC session by sid").toBe(200);
  const session = (await read.json()) as {
    session: { id: string; factors?: { user?: { id: string } } };
  };
  expect(session.session.id).toBe(claims.sid);
  expect(session.session.factors?.user?.id).toBe(claims.sub);

  const deleted = await zitadel("DELETE", `/v2/sessions/${claims.sid}`);
  expect(deleted.status, `delete without the session token: ${await deleted.clone().text()}`).toBe(
    200,
  );
  expect((await zitadel("GET", `/v2/sessions/${claims.sid}`)).status, "the session is gone").toBe(
    404,
  );
});
