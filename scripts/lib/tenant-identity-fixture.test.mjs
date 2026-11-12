import assert from "node:assert/strict";
import test from "node:test";
import { ensureTenantIdentityFixtures } from "./tenant-identity-fixture.mjs";

test("identity fixture refuses a production issuer before requesting credentials", async () => {
  let calls = 0;
  await assert.rejects(
    ensureTenantIdentityFixtures({
      issuer: "https://auth.tesserix.app",
      pat: "synthetic",
      fetcher: async () => {
        calls++;
      },
    }),
    /local native issuer/,
  );
  assert.equal(calls, 0);
});

test("identity fixture provisions two native scopes without adopting the platform org", async () => {
  const requests = [];
  const manifest = await ensureTenantIdentityFixtures({
    issuer: "http://auth.tesserix.localhost:20080",
    pat: "synthetic",
    fetcher: async (url, options) => {
      const path = new URL(url).pathname;
      const body = options.body ? JSON.parse(options.body) : null;
      requests.push({ path, body, scope: options.headers["x-zitadel-orgid"] });
      if (path === "/management/v1/orgs/me") return Response.json({ org: { id: "platform-org" } });
      if (path === "/management/v1/projects/_search")
        return Response.json({ result: [{ id: "native-project", name: "Helivanta" }] });
      if (path.startsWith("/admin/v1/orgs/")) return new Response("{}", { status: 404 });
      if (path === "/v2/organizations")
        return Response.json({ organizationId: body.organizationId });
      if (path === "/management/v1/projectgrants/_search")
        return Response.json({ details: {}, result: [] });
      return Response.json({ grantId: "native-grant" });
    },
  });
  assert.deepEqual(manifest, { projectId: "native-project", adminOrgId: "platform-org" });
  const creates = requests.filter((x) => x.path === "/v2/organizations");
  assert.equal(creates.length, 2);
  assert.notEqual(creates[0].body.organizationId, creates[1].body.organizationId);
  assert.ok(creates.every((x) => x.body.organizationId.startsWith("helivanta-")));
  const metadata = requests.filter((x) => x.path === "/management/v1/metadata/tenant_id");
  assert.equal(metadata.length, 2);
  for (const entry of metadata)
    assert.equal(entry.scope, "helivanta-" + Buffer.from(entry.body.value, "base64").toString());
  const grants = requests.filter((x) => x.path === "/management/v1/projects/native-project/grants");
  assert.equal(grants.length, 2);
  assert.ok(grants.every((x) => x.scope === "platform-org"));
});

await test("identity owner setup refuses nonlocal configuration before invoking tools", async () => {
  const { spawnSync } = await import("node:child_process");
  const script = new URL("../ci/identity-owner-fixture.sh", import.meta.url);
  for (const overrides of [
    { HELIVANTA_ENV: "production", ZITADEL_ISSUER_URL: "http://auth.tesserix.localhost:20080" },
    { HELIVANTA_ENV: "dev", ZITADEL_ISSUER_URL: "https://auth.tesserix.app" },
  ]) {
    const result = spawnSync("/bin/bash", [script.pathname, "setup"], {
      env: { PATH: "", HELIVANTA_IDENTITY_FIXTURE: "true", ...overrides },
      encoding: "utf8",
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /requires explicit local development/);
    assert.doesNotMatch(result.stderr, /command not found/);
  }
});
