import assert from "node:assert/strict";
import test from "node:test";
import { validateReleaseManifest } from "./lib/release-capabilities.mjs";

const manifest = () => ({
  version: 1,
  policy_version: "test-policy",
  capabilities: [
    { key: "team_accounts", dependencies: [], eligible: true, owner: "platform" },
    { key: "staff_core", dependencies: ["team_accounts"], eligible: true, owner: "workforce" },
  ],
  routes: [{ method: "GET", path: "/v1/iam/members", capability: "team_accounts" }],
  pages: [{ path: "/workforce", capability: "staff_core" }],
  panels: [{ key: "staff.postings", capability: "staff_core" }],
});

test("a released feature never makes an unknown API route available", () => {
  const value = manifest();
  assert.throws(
    () => validateReleaseManifest(value, [{ method: "POST", path: "/v1/new-command" }]),
    /unclassified.*POST \/v1\/new-command/,
  );
});

test("known API, page and panel bindings share one capability vocabulary", () => {
  const value = manifest();
  assert.doesNotThrow(() => validateReleaseManifest(value, value.routes));
  value.panels[0].capability = "unknown";
  assert.throws(() => validateReleaseManifest(value, value.routes), /unknown capability/);
});

test("duplicate API declarations cannot silently overwrite a restriction", () => {
  const value = manifest();
  value.routes.push({ ...value.routes[0], capability: "baseline" });
  assert.throws(() => validateReleaseManifest(value, value.routes), /duplicate route/);
});

test("release dependencies must exist, be acyclic and be eligible before dependants", () => {
  const value = manifest();
  value.capabilities[0].dependencies = ["staff_core"];
  assert.throws(() => validateReleaseManifest(value, value.routes), /dependency cycle/);
  value.capabilities[0].dependencies = ["missing"];
  assert.throws(() => validateReleaseManifest(value, value.routes), /unknown dependency/);
  value.capabilities[0].dependencies = [];
  value.capabilities[0].eligible = false;
  assert.throws(() => validateReleaseManifest(value, value.routes), /ineligible dependency/);
});

test("baseline security is not a remotely configurable capability", () => {
  const value = manifest();
  value.capabilities.push({ key: "baseline", dependencies: [], eligible: true, owner: "platform" });
  assert.throws(() => validateReleaseManifest(value, value.routes), /reserved capability/);
});

test("unknown declaration fields and ambiguous routes are refused", () => {
  const value = manifest();
  value.routes[0].allowWhenMissing = true;
  assert.throws(() => validateReleaseManifest(value, value.routes), /unknown route field/);
  delete value.routes[0].allowWhenMissing;
  value.routes[0].path = "/v1/iam/members?enabled=true";
  assert.throws(() => validateReleaseManifest(value, value.routes), /invalid route/);
});
