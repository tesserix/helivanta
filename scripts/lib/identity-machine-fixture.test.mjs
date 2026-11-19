import assert from "node:assert/strict";
import test from "node:test";
import { mkdtempSync, writeFileSync, chmodSync, symlinkSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  ensureLocalIdentityMachine,
  readPrivateFixtureSecret,
} from "./identity-machine-fixture.mjs";

const issuer = "http://auth.tesserix.localhost:20080";
const response = (body, status = 200) => new Response(JSON.stringify(body), { status });
test("creates one real native machine and retains its dedicated credential without printing or rotating it", async () => {
  const calls = [];
  let retained = null;
  const machine = await ensureLocalIdentityMachine({
    issuer,
    pat: "synthetic-seed-token",
    org: "platform-org",
    readSecret: () => null,
    writeSecret: (secret) => {
      retained = secret;
    },
    fetcher: async (url, options) => {
      calls.push([url, options]);
      if (url.endsWith("/_search")) return response({ result: [] });
      if (url.endsWith("/users/machine")) return response({ userId: "native-machine" });
      if (url.endsWith("/users/native-machine"))
        return response({
          user: {
            id: "native-machine",
            state: "USER_STATE_ACTIVE",
            details: { resourceOwner: "platform-org" },
            preferredLoginName: "native-machine",
            machine: {},
          },
        });
      if (url.endsWith("/native-machine/secret")) {
        assert.equal(options.method, "PUT", "pinned native GenerateMachineSecret uses PUT");
        return response({ clientId: "native-machine", clientSecret: "synthetic-client-secret" });
      }
      throw new Error("unbounded native fixture request");
    },
  });
  assert.deepEqual(machine, { userId: "native-machine", clientId: "native-machine" });
  assert.equal(retained, "synthetic-client-secret");
  assert.equal(calls.length, 4);
  assert.equal(JSON.parse(calls[1][1].body).accessTokenType, "ACCESS_TOKEN_TYPE_JWT");
});
test("reuses the existing native machine only with its retained secret and expected owner", async () => {
  let calls = 0;
  const machine = await ensureLocalIdentityMachine({
    issuer,
    pat: "synthetic-seed-token",
    org: "platform-org",
    readSecret: () => "synthetic-client-secret",
    writeSecret: () => assert.fail("must not rotate"),
    fetcher: async (url) => {
      calls++;
      if (url.endsWith("/users/native-machine"))
        return response({
          user: {
            id: "native-machine",
            state: "USER_STATE_ACTIVE",
            details: { resourceOwner: "platform-org" },
            preferredLoginName: "native-machine",
            machine: {},
          },
        });
      return response({
        result: [
          {
            id: "native-machine",
            state: "USER_STATE_ACTIVE",
            details: { resourceOwner: "platform-org" },
            machine: { name: "Local product" },
          },
        ],
      });
    },
  });
  assert.deepEqual(machine, { userId: "native-machine", clientId: "native-machine" });
  assert.equal(calls, 2);
});
test("refuses production identity, missing retained secret, human identity and foreign machine", async () => {
  for (const [origin, user, secret] of [
    ["https://auth.tesserix.app", null, null],
    [
      issuer,
      {
        id: "native-machine",
        state: "USER_STATE_ACTIVE",
        details: { resourceOwner: "platform-org" },
        machine: { name: "Local" },
      },
      null,
    ],
    [
      issuer,
      {
        id: "native-machine",
        state: "USER_STATE_ACTIVE",
        details: { resourceOwner: "platform-org" },
        human: {},
      },
      "secret",
    ],
    [
      issuer,
      {
        id: "native-machine",
        state: "USER_STATE_ACTIVE",
        details: { resourceOwner: "foreign-org" },
        machine: { name: "Local" },
      },
      "secret",
    ],
  ]) {
    await assert.rejects(
      ensureLocalIdentityMachine({
        issuer: origin,
        org: "platform-org",
        pat: "synthetic-seed-token",
        readSecret: () => secret,
        writeSecret: () => assert.fail("no rotation"),
        fetcher: async (url) =>
          url.endsWith("/_search")
            ? response({ result: user ? [user] : [] })
            : response({ user: { preferredLoginName: "native-machine", ...user } }),
      }),
    );
  }
});

test("retained machine credentials require bounded regular private files", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "native-machine-private-"));
  try {
    const file = path.join(dir, "credential");
    assert.equal(readPrivateFixtureSecret(file), null);
    writeFileSync(file, "synthetic-retained-secret\n", { mode: 0o600 });
    assert.equal(readPrivateFixtureSecret(file), "synthetic-retained-secret");
    const link = path.join(dir, "link");
    symlinkSync(file, link);
    assert.throws(() => readPrivateFixtureSecret(link));
    chmodSync(file, 0o644);
    assert.throws(() => readPrivateFixtureSecret(file));
    chmodSync(file, 0o600);
    for (const content of ["", "x".repeat(8193)]) {
      writeFileSync(file, content);
      assert.throws(() => readPrivateFixtureSecret(file));
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("native preferred OAuth login name remains distinct from the verified machine subject", async () => {
  const login = "helivanta-local-identity-product@platform.localhost";
  const calls = [];
  const machine = await ensureLocalIdentityMachine({
    issuer,
    pat: "synthetic-seed-token",
    org: "platform-org",
    readSecret: () => null,
    writeSecret: () => {},
    fetcher: async (url, options) => {
      calls.push([url, options.method]);
      if (url.endsWith("/_search")) return response({ result: [] });
      if (url.endsWith("/users/machine")) return response({ userId: "native-machine" });
      if (url.endsWith("/users/native-machine"))
        return response({
          user: {
            id: "native-machine",
            state: "USER_STATE_ACTIVE",
            details: { resourceOwner: "platform-org" },
            preferredLoginName: login,
            machine: {},
          },
        });
      if (url.endsWith("/secret"))
        return response({ clientId: login, clientSecret: "synthetic-client-secret" });
      throw new Error("unexpected fixture request");
    },
  });
  assert.deepEqual(machine, { userId: "native-machine", clientId: login });
  assert.ok(
    calls.some(([url, method]) => url.endsWith("/users/native-machine") && method === "GET"),
  );
});

test("a credential returned for a different native client is never retained", async () => {
  let saved = false;
  await assert.rejects(
    ensureLocalIdentityMachine({
      issuer,
      pat: "synthetic",
      org: "platform-org",
      readSecret: () => null,
      writeSecret: () => {
        saved = true;
      },
      fetcher: async (url) => {
        if (url.endsWith("/_search")) return response({ result: [] });
        if (url.endsWith("/users/machine")) return response({ userId: "native-machine" });
        if (url.endsWith("/users/native-machine"))
          return response({
            user: {
              id: "native-machine",
              state: "USER_STATE_ACTIVE",
              details: { resourceOwner: "platform-org" },
              preferredLoginName: "native-machine@platform.localhost",
              machine: {},
            },
          });
        if (url.endsWith("/secret"))
          return response({
            clientId: "foreign@platform.localhost",
            clientSecret: "synthetic-client-secret",
          });
        throw new Error("unexpected fixture request");
      },
    }),
  );
  assert.equal(saved, false);
});
