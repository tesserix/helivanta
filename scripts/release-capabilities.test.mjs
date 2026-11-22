import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
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

test("every governance template and scope has an explicit release binding", () => {
  const release = JSON.parse(
    readFileSync(new URL("../contracts/release-capabilities.json", import.meta.url), "utf8"),
  );
  const governance = JSON.parse(
    readFileSync(new URL("../contracts/staff-governance.json", import.meta.url), "utf8"),
  );
  const panels = new Map(release.panels.map((panel) => [panel.key, panel.capability]));
  const capabilities = new Set(release.capabilities.map((definition) => definition.key));
  for (const template of governance.templates) {
    assert.ok(
      capabilities.has(panels.get(`staff.governance.template.${template.key}`)),
      template.key,
    );
    for (const kind of template.scope_kinds)
      assert.ok(capabilities.has(panels.get(`staff.governance.scope.${kind}`)), kind);
  }
  assert.equal(
    panels.get("staff.governance.template.iam.operating_access_reviewer.v1"),
    "supplier_collaboration",
  );
  assert.equal(panels.get("staff.governance.scope.pharmacy"), "pharmacy_staff");
});

test("a released feature never makes an unknown API route available", () => {
  const value = manifest();
  assert.throws(
    () => validateReleaseManifest(value, [{ method: "POST", path: "/v1/new-command" }]),
    /unclassified.*POST \/v1\/new-command/,
  );
});

test("disabling new Pharmacy payables preserves retained bill credit evidence", () => {
  const value = JSON.parse(
    readFileSync(new URL("../contracts/release-capabilities.json", import.meta.url), "utf8"),
  );
  const base = "/v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet/accounts/payables";
  const routes = value.routes.filter(
    (route) => route.path === base || route.path.startsWith(`${base}/`),
  );
  assert.ok(
    routes.some(
      (route) => route.method === "GET" && route.path === `${base}/:id/supplier-credit-allocations`,
    ),
    "selected-bill credit history must have an explicit release binding",
  );
  for (const route of routes) {
    const retained = route.method === "GET" && route.path === `${base}/:id/supplier-credit-allocations`;
    assert.equal(
      route.capability,
      retained ? "baseline" : "pharmacy_payables",
      `${route.method} ${route.path}`,
    );
  }
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

test("conditional owner callbacks are explicit baseline routes, never public capabilities", () => {
  const value = manifest();
  value.internal_routes = [
    { method: "POST", path: "/v1/supply/internal/trading-source/check", capability: "baseline" },
  ];
  assert.doesNotThrow(() => validateReleaseManifest(value, value.routes));
  value.internal_routes[0].capability = "staff_core";
  assert.throws(() => validateReleaseManifest(value, value.routes), /invalid internal owner route/);
  value.internal_routes[0].capability = "baseline";
  value.internal_routes[0].path = "/v1/public-command";
  assert.throws(() => validateReleaseManifest(value, value.routes), /invalid internal owner route/);
  value.internal_routes[0].path = "/internal/:any";
  assert.throws(() => validateReleaseManifest(value, value.routes), /invalid internal owner route/);
});

test("snapshot wire keys and bounds follow the canonical capability catalogue", async () => {
  const { releaseWireModels } = await import("./lib/release-capabilities.mjs");
  const value = manifest();
  value.models = {
    ReleaseCapability: [{ name: "key", type: "string", enum_from: "capabilities.key" }],
    ReleaseSnapshot: [
      { name: "capabilities", type: "[]ReleaseCapability", count_from: "capabilities" },
    ],
  };
  const models = releaseWireModels(value);
  assert.deepEqual(models.ReleaseCapability[0].enum, ["team_accounts", "staff_core"]);
  assert.equal(models.ReleaseSnapshot[0].min, 2);
  assert.equal(models.ReleaseSnapshot[0].max, 2);
  value.capabilities.push({
    key: "staff_mfa_recovery",
    dependencies: ["staff_core"],
    eligible: false,
    owner: "workforce",
  });
  const added = releaseWireModels(value);
  assert.deepEqual(added.ReleaseCapability[0].enum, [
    "team_accounts",
    "staff_core",
    "staff_mfa_recovery",
  ]);
  assert.equal(added.ReleaseSnapshot[0].min, 3);
  assert.equal(added.ReleaseSnapshot[0].max, 3);
  assert.equal(
    value.models.ReleaseSnapshot[0].min,
    undefined,
    "generation must preserve canonical directives",
  );
});

test("manually copied or unknown snapshot bindings cannot bypass catalogue derivation", async () => {
  const { releaseWireModels } = await import("./lib/release-capabilities.mjs");
  const value = manifest();
  for (const [key, count] of [
    [
      { name: "key", type: "string", enum: ["old_key"] },
      { name: "capabilities", type: "[]ReleaseCapability", min: 1, max: 1 },
    ],
    [
      { name: "key", type: "string", enum_from: "unknown" },
      { name: "capabilities", type: "[]ReleaseCapability", count_from: "capabilities" },
    ],
    [
      { name: "key", type: "string", enum_from: "capabilities.key", enum: ["old_key"] },
      { name: "capabilities", type: "[]ReleaseCapability", count_from: "capabilities" },
    ],
  ]) {
    value.models = { ReleaseCapability: [key], ReleaseSnapshot: [count] };
    assert.throws(() => releaseWireModels(value), /catalogue/);
  }
});

test("disabling new Staff MFA resets preserves authenticated status and cancellation", () => {
  const value = JSON.parse(
    readFileSync(new URL("../contracts/release-capabilities.json", import.meta.url), "utf8"),
  );
  const routes = new Map(
    value.routes.map((route) => [`${route.method} ${route.path}`, route.capability]),
  );
  for (const [family, kind] of [
    ["hospitals", "hospital"],
    ["pharmacies", "pharmacy"],
    ["suppliers", "supplier"],
  ]) {
    const path = `/v1/workforce/${family}/:${kind}/staff/:id/mfa-recoveries/:command`;
    assert.equal(routes.get(`PUT ${path}`), "staff_mfa_recovery");
    assert.equal(routes.get(`GET ${path}`), "baseline");
    assert.equal(routes.get(`POST ${path}/cancel`), "baseline");
  }
});
