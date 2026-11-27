import assert from "node:assert/strict";
import test from "node:test";
import { inventory, parseMembers, parseRegistered } from "./security-key-inventory.mjs";

const users = {
  owner: {
    org: "o1",
    factors: [
      { state: "AUTH_FACTOR_STATE_READY", u2f: { id: "old" } },
      { state: "AUTH_FACTOR_STATE_READY", otp: {} },
    ],
  },
  native: { org: "o2", factors: [{ state: "AUTH_FACTOR_STATE_READY", u2f: { id: "new" } }] },
  pending: { org: "o2", factors: [{ state: "AUTH_FACTOR_STATE_NOT_READY", u2f: { id: "half" } }] },
};
const fake = {
  get: async (path) => {
    const id = decodeURIComponent(path.split("/").pop());
    return users[id] ? { user: { userId: id, details: { resourceOwner: users[id].org } } } : null;
  },
  search: async (path, org) => {
    const id = decodeURIComponent(path.split("/")[4]);
    assert.equal(org, users[id].org);
    return { result: users[id].factors };
  },
};

test("counts only READY u2f keys missing from the registry as legacy", async () => {
  const report = await inventory({
    members: parseMembers("owner\nnative\npending\ngone\nowner\n"),
    registered: parseRegistered("native,new\n"),
    ...fake,
  });
  assert.deepEqual(report, {
    members: 4,
    missing: 1,
    withKeys: 2,
    registeredKeys: 1,
    legacy: [{ subject: "owner", org: "o1", keyID: "old" }],
  });
});

test("a Zitadel failure stops the count instead of reporting zero", async () => {
  await assert.rejects(
    inventory({
      members: ["native"],
      registered: new Set(),
      ...fake,
      search: async () => {
        throw new Error("HTTP 503");
      },
    }),
    /HTTP 503/,
  );
});

test("a user response without an organisation stops the count", async () => {
  await assert.rejects(
    inventory({
      members: ["native"],
      registered: new Set(),
      ...fake,
      get: async () => ({ user: { userId: "native" } }),
    }),
    /no id or organisation/,
  );
});

test("a malformed registry line is refused", () => {
  assert.throws(() => parseRegistered("only-subject\n"), /bad line/);
});
