import assert from "node:assert/strict";
import test from "node:test";
import {
  ensureTenantIdentityFixtures,
  verifyLocalIdentityUser,
} from "./tenant-identity-fixture.mjs";

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

test("native user fixtures verify the actual home organization before granting application membership", async () => {
  const subject = "native-staff";
  const tenantId = "22222222-2222-2222-2222-222222222222";
  let calls = 0;
  await verifyLocalIdentityUser({
    issuer: "http://auth.tesserix.localhost:20080",
    pat: "synthetic",
    subject,
    tenantId,
    fetcher: async (url, options) => {
      calls++;
      assert.equal(url, "http://auth.tesserix.localhost:20080/v2/users/native-staff");
      assert.equal(options.method, "GET");
      return Response.json({
        user: {
          userId: subject,
          state: "USER_STATE_ACTIVE",
          details: { resourceOwner: `helivanta-${tenantId}` },
          human: {},
        },
      });
    },
  });
  assert.equal(calls, 1);
  for (const user of [
    {
      userId: subject,
      state: "USER_STATE_ACTIVE",
      details: { resourceOwner: "helivanta-11111111-1111-1111-1111-111111111111" },
      human: {},
    },
    {
      userId: subject,
      state: "USER_STATE_INACTIVE",
      details: { resourceOwner: `helivanta-${tenantId}` },
      human: {},
    },
    {
      userId: "other",
      state: "USER_STATE_ACTIVE",
      details: { resourceOwner: `helivanta-${tenantId}` },
      human: {},
    },
    {
      userId: subject,
      state: "USER_STATE_ACTIVE",
      details: { resourceOwner: `helivanta-${tenantId}` },
      machine: {},
    },
  ])
    await assert.rejects(
      verifyLocalIdentityUser({
        issuer: "http://auth.tesserix.localhost:20080",
        pat: "synthetic",
        subject,
        tenantId,
        fetcher: async () => Response.json({ user }),
      }),
    );
  await assert.rejects(
    verifyLocalIdentityUser({
      issuer: "https://auth.tesserix.app",
      pat: "synthetic",
      subject,
      tenantId,
      fetcher: async () => assert.fail("production must not be queried"),
    }),
  );
});

test("native fixture account parsing does not expose malformed provider payloads", async () => {
  await assert.rejects(
    verifyLocalIdentityUser({
      issuer: "http://auth.tesserix.localhost:20080",
      pat: "synthetic",
      subject: "native-staff",
      tenantId: "22222222-2222-2222-2222-222222222222",
      fetcher: async () => new Response("sensitive-invalid-provider-material"),
    }),
    (error) => {
      assert.equal(error.message, "native Staff fixture account proof malformed");
      assert.ok(!String(error).includes("sensitive-invalid-provider-material"));
      return true;
    },
  );
});
