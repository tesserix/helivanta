import http from "node:http";
import type { AddressInfo } from "node:net";

import { expect, test } from "@playwright/test";

import { ZITADEL_ORIGIN } from "./support/hosts";

// #916 Task 4, design spec D6 — the guard on the harness itself.
//
// #916 was a production defect no test could have caught, and THAT is the
// finding worth more than the fix. In production the app is helivanta.app
// and the IdP is auth.tesserix.app: different registrable domains, so every
// browser request from the app to the IdP is CROSS-SITE, so Zitadel's
// SameSite=Lax session cookie was never sent and the hidden-iframe
// `prompt=none` renewal always failed. Clinicians were evicted minutes
// after signing in.
//
// The old dev/CI harness served the app at localhost:4301 and the IdP at
// localhost:20080. A "site" is scheme + registrable domain and PORTS ARE
// NOT PART OF IT, so that pair was SAME-SITE: the cookie flowed, renewal
// succeeded, and production's reality was never exercised. The harness was
// more permissive than production, which is the same shape as #894.
//
// The fix moved the two onto helivanta.localhost and auth.tesserix.localhost.
// That rests on a claim about the browser — that `*.localhost` resolves to
// loopback without an /etc/hosts edit AND that helivanta.localhost and
// tesserix.localhost are distinct registrable domains — and a claim about
// the browser is exactly the kind of thing that must be proven rather than
// assumed. This spec proves it, both directions, so the property the whole
// topology exists for cannot silently regress.
//
// CRITICALLY, it derives the two hosts it probes from the RUNNING
// CONFIGURATION — the project's own `baseURL` for the app, and
// tests/support/hosts.ts's ZITADEL_ORIGIN (HELIVANTA_ZITADEL_HOST) for the
// IdP — never from string literals. An earlier version hardcoded both
// names, which made it unable to observe the very thing it guards: setting
// HELIVANTA_WEB_HOST and HELIVANTA_ZITADEL_HOST back to `localhost` and
// re-running `make dev-infra` re-registers the redirect URIs, so login
// keeps working, the whole suite silently returns to same-site, and a spec
// named "…are cross-site" goes on reporting green. Reading the live values
// is what turns this from a restatement of an assumption into a control.
//
// Deliberately self-contained: two throwaway HTTP servers rather than the
// real stack, because the claim under test is about the BROWSER's
// same-site computation, not about Helivanta. Driving it through a real
// Zitadel login would test far more machinery for a far weaker signal, and
// could not express the CONTROL case (the old same-site pair) at all.

const IDP_COOKIE = "harness_idp_session";

// A stand-in IdP: /set installs a SameSite=Lax cookie the way a real
// top-level sign-in does; /echo reports whether that cookie arrived on the
// request that fetched it.
function startIdp(): Promise<http.Server> {
  const server = http.createServer((req, res) => {
    if ((req.url ?? "").startsWith("/set")) {
      res.writeHead(200, {
        "set-cookie": `${IDP_COOKIE}=yes; Path=/; SameSite=Lax`,
        "content-type": "text/html",
      });
      res.end("<body>idp cookie installed</body>");
      return;
    }
    const sent = (req.headers.cookie ?? "").includes(`${IDP_COOKIE}=yes`);
    res.writeHead(200, { "content-type": "text/html" });
    res.end(`<body data-cookie-sent="${sent}">cookie sent: ${sent}</body>`);
  });
  return new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve(server)));
}

// A stand-in app whose page embeds an iframe pointing at the IdP — the
// exact shape of the `prompt=none` silent renewal #916 removed.
function startApp(idpOrigin: string): Promise<http.Server> {
  const server = http.createServer((_req, res) => {
    res.writeHead(200, { "content-type": "text/html" });
    res.end(`<body><iframe id="probe" src="${idpOrigin}/echo"></iframe></body>`);
  });
  return new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve(server)));
}

function portOf(server: http.Server): number {
  return (server.address() as AddressInfo).port;
}

// Runs one probe: seed the IdP cookie by a TOP-LEVEL visit (which a Lax
// cookie is always set on), then load the app page and read what the
// iframe's own request carried. Returns true when the Lax cookie travelled,
// i.e. when the two hosts are same-site.
async function laxCookieTravels(
  browserContextPage: import("@playwright/test").Page,
  appHost: string,
  appPort: number,
  idpHost: string,
  idpPort: number,
): Promise<boolean> {
  await browserContextPage.goto(`http://${idpHost}:${idpPort}/set`);
  await browserContextPage.goto(`http://${appHost}:${appPort}/`);
  const sent = await browserContextPage
    .frameLocator("#probe")
    .locator("body")
    .getAttribute("data-cookie-sent");
  expect(sent, "the probe iframe did not report a result at all").not.toBeNull();
  return sent === "true";
}

test.describe("the e2e harness reproduces production's cross-site app/IdP relationship", () => {
  // The claim that MATTERS: on the hostnames THIS RUN is configured with,
  // a SameSite=Lax IdP cookie is withheld from an app-origin iframe
  // request — so the harness can see #916's class of defect.
  test("the configured app and IdP hosts are cross-site", async ({ page, baseURL }) => {
    // Read from the running configuration, never hardcoded — see this
    // file's header for why that distinction is the whole point.
    expect(baseURL, "the project must define a baseURL for this guard to read").toBeTruthy();
    const appHost = new URL(baseURL!).hostname;
    const idpHost = new URL(ZITADEL_ORIGIN).hostname;

    // Fails loudly rather than probing two identical hosts and reporting
    // a confusing cookie result: identical hosts are trivially same-site,
    // and the message should name the misconfiguration, not its symptom.
    expect(
      appHost,
      `the app and the IdP are configured on the SAME host (${appHost}). They must be on ` +
        "different registrable domains or this suite cannot see #916's class of defect — see " +
        "HELIVANTA_WEB_HOST / HELIVANTA_ZITADEL_HOST in the Makefile.",
    ).not.toBe(idpHost);

    const idp = await startIdp();
    const idpPort = portOf(idp);
    const app = await startApp(`http://${idpHost}:${idpPort}`);
    try {
      const travelled = await laxCookieTravels(page, appHost, portOf(app), idpHost, idpPort);
      expect(
        travelled,
        `${appHost} (the app) and ${idpHost} (the IdP) must be CROSS-SITE — a SameSite=Lax ` +
          "cookie set on the IdP must NOT be sent on an iframe request from the app origin. " +
          "If this fails the harness is same-site again and #916's class of defect is invisible " +
          "to every test in this suite. Ports do not make two hosts different sites; only a " +
          "different registrable domain does.",
      ).toBe(false);
    } finally {
      idp.close();
      app.close();
    }
  });

  // The control, and the reason this file exists rather than a comment:
  // it demonstrates that the harness this replaced genuinely could NOT
  // have caught #916. Without it, the assertion above is a claim about one
  // pair of hostnames with nothing to contrast it against, and a future
  // reader has no evidence that ports really are irrelevant to a "site".
  test("the previous localhost-only harness was same-site, which is why it was blind", async ({
    page,
  }) => {
    const idp = await startIdp();
    const idpPort = portOf(idp);
    const app = await startApp(`http://localhost:${idpPort}`);
    try {
      const travelled = await laxCookieTravels(page, "localhost", portOf(app), "localhost", idpPort);
      expect(
        travelled,
        "two different PORTS on localhost are one site, so a SameSite=Lax cookie is sent — " +
          "this is exactly why the old app-on-4301 / IdP-on-20080 harness could not have " +
          "caught #916",
      ).toBe(true);
    } finally {
      idp.close();
      app.close();
    }
  });
});
