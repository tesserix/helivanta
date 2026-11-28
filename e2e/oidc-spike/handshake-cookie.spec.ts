import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { expect, test } from "@playwright/test";

// #915 spike 2 (docs/superpowers/specs/2026-11-20-server-side-oidc-handshake-design.md).
// "app" stands in for Helivanta, on localhost; "idp" stands in for Zitadel, on
// 127.0.0.1, a different site. The app sets the handshake cookie on its start
// route and sends the browser to the idp; the browser's next top-level
// navigation goes back through the idp's 302 to the app's callback, exactly
// the hop a real sign-in ends on. The Strict control proves the hop really is
// cross-site: if it arrived, this test would be measuring nothing.
//
// Secure is left off because these servers are plain HTTP; the deployed
// cookie is Secure on HTTPS, which changes when it is sent, not SameSite.

function listen(server: Server, host: string): Promise<string> {
  return new Promise((resolve) =>
    server.listen(0, host, () =>
      resolve(`http://${host}:${(server.address() as AddressInfo).port}`),
    ),
  );
}

let app: Server, idp: Server, appOrigin: string, idpOrigin: string;
let received: string | undefined;

test.beforeAll(async () => {
  app = createServer((req, res) => {
    const url = new URL(req.url ?? "/", appOrigin);
    if (url.pathname === "/api/v1/auth/oidc/start") {
      res.writeHead(302, {
        "Set-Cookie": [
          "helivanta_oidc=sealed-state; Path=/api/v1/auth/oidc; HttpOnly; SameSite=Lax; Max-Age=3600",
          "strict_control=1; Path=/api/v1/auth/oidc; HttpOnly; SameSite=Strict; Max-Age=3600",
        ],
        Location: `${idpOrigin}/authorize`,
      });
      res.end();
      return;
    }
    if (url.pathname === "/api/v1/auth/oidc/callback") {
      received = req.headers.cookie ?? "";
      res.writeHead(200, { "Content-Type": "text/plain" });
      res.end("callback");
      return;
    }
    res.writeHead(404).end();
  });
  idp = createServer((req, res) => {
    const url = new URL(req.url ?? "/", idpOrigin);
    if (url.pathname === "/authorize") {
      // The credential card lives on the app; here a link stands in for the
      // browser leaving the idp after sign-in, which is a top-level navigation.
      res.writeHead(200, { "Content-Type": "text/html" });
      res.end(`<a id="finish" href="${idpOrigin}/finish">finish</a>`);
      return;
    }
    if (url.pathname === "/finish") {
      res.writeHead(302, { Location: `${appOrigin}/api/v1/auth/oidc/callback?code=c&state=s` });
      res.end();
      return;
    }
    res.writeHead(404).end();
  });
  appOrigin = await listen(app, "localhost");
  idpOrigin = await listen(idp, "127.0.0.1");
});

test.afterAll(async () => {
  await new Promise((done) => app.close(done));
  await new Promise((done) => idp.close(done));
});

test("the Lax handshake cookie survives the identity provider's cross-site 302", async ({
  page,
}) => {
  received = undefined;
  await page.goto(`${appOrigin}/api/v1/auth/oidc/start`);
  await expect(page).toHaveURL(`${idpOrigin}/authorize`);
  await page.click("#finish");
  await expect(page.locator("body")).toHaveText("callback");
  expect(received, "the callback was reached").toBeDefined();
  expect(received).toContain("helivanta_oidc=sealed-state");
  expect(
    received,
    "a Strict cookie must not cross: otherwise the hop is not cross-site",
  ).not.toContain("strict_control");
});
