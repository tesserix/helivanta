import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, copyFile, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const output = "docs/qa/staff-operational-access-inventory.json";
const reviewFile = "docs/qa/staff-operational-access-reviews.json";
const registryFile = "docs/api/reference/inventory.json";

async function fixture(t) {
  const dir = await mkdtemp(path.join(tmpdir(), "staff-consumer-inventory-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  for (const subdir of ["scripts", "docs/qa", "docs/api/reference", "backend"])
    await mkdir(path.join(dir, subdir), { recursive: true });
  await copyFile(
    path.join(root, "scripts/staff-operational-access-inventory.mjs"),
    path.join(dir, "scripts/staff-operational-access-inventory.mjs"),
  );
  const registry = {
    format_version: 1,
    modules: [
      {
        name: "care",
        source: "backend/module.go",
        routes: [
          {
            method: "GET",
            path: "/v1/care/:hospital",
            permission: "public",
            authentication_required: true,
            membership_required: true,
            paginated: false,
            counted: false,
          },
        ],
        consumers: [{ name: "care-work", subject: "care.requested.v1", after_commit: true }],
        broadcasts: ["care.revoked.v1"],
      },
    ],
    unauthenticated_routes: { "GET /healthz": "liveness probe" },
  };
  await writeFile(path.join(dir, registryFile), JSON.stringify(registry));
  await writeFile(path.join(dir, reviewFile), JSON.stringify({ format_version: 1, reviews: [] }));
  await writeFile(path.join(dir, "backend/module.go"), "package care\n");
  return { dir, registry };
}
function run(dir, ...args) {
  return spawnSync(
    process.execPath,
    [path.join(dir, "scripts/staff-operational-access-inventory.mjs"), ...args],
    { cwd: dir, encoding: "utf8", timeout: 10000 },
  );
}

test("registry routes, async consumers, broadcasts and pre-login surfaces remain explicitly unclassified", async (t) => {
  const { dir } = await fixture(t);
  const result = run(dir);
  assert.equal(result.status, 0, result.stderr);
  const ledger = JSON.parse(await readFile(path.join(dir, output), "utf8"));
  assert.equal(ledger.entries.length, 4);
  assert.deepEqual(ledger.entries.map((entry) => entry.kind).sort(), [
    "broadcast",
    "consumer",
    "http",
    "unauthenticated_http",
  ]);
  assert.ok(ledger.entries.every((entry) => entry.review === null));
  assert.equal(
    ledger.entries.find((entry) => entry.kind === "http").registration.permission,
    "public",
  );
  assert.equal(
    ledger.summary.unclassified,
    4,
    "public/sessionless/worker registration must not imply an exemption",
  );
  assert.equal(run(dir, "--check").status, 0);
  const gate = run(dir, "--check", "--require-classified");
  assert.notEqual(gate.status, 0);
  assert.match(gate.stderr, /4 unclassified/);
});

async function reviewFixture(dir, overrides = {}, kind = "http") {
  const { createHash } = await import("node:crypto");
  const hash = (value) => createHash("sha256").update(value).digest("hex");
  const result = run(dir);
  assert.equal(result.status, 0, result.stderr);
  const ledger = JSON.parse(await readFile(path.join(dir, output), "utf8"));
  const entry = ledger.entries.find((item) => item.kind === kind);
  const review = {
    id: entry.id,
    registration_sha256: hash(JSON.stringify(entry.registration)),
    authority_kind: "hospital_operational",
    resource_scope: "verified tenant and hospital",
    rationale:
      "The owner reads clinical records under a hospital grant; current employment is not yet checked.",
    authority_sources: [
      {
        path: "backend/module.go",
        sha256: hash(await readFile(path.join(dir, "backend/module.go"))),
        symbol: "package care",
      },
    ],
    remaining: ["Implement and verify effective employment cutoff through signed sessions."],
    ...overrides,
  };
  await writeFile(
    path.join(dir, reviewFile),
    JSON.stringify({ format_version: 1, reviews: [review] }),
  );
  return review;
}

test("source-reviewed classification is explicit, hash-pinned and never reported as runtime acceptance", async (t) => {
  const { dir } = await fixture(t);
  const review = await reviewFixture(dir);
  const result = run(dir);
  assert.equal(result.status, 0, result.stderr);
  const ledger = JSON.parse(await readFile(path.join(dir, output), "utf8"));
  assert.deepEqual(ledger.entries.find((entry) => entry.id === review.id).review, review);
  assert.equal(ledger.summary.unclassified, 3);
  assert.match(result.stdout, /No runtime acceptance/);
});

test("source drift refuses regeneration and preserves the last ledger", async (t) => {
  const { dir } = await fixture(t);
  await reviewFixture(dir);
  assert.equal(run(dir).status, 0);
  const before = await readFile(path.join(dir, output), "utf8");
  await writeFile(path.join(dir, "backend/module.go"), "package changed\n");
  const result = run(dir);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /source changed/);
  assert.equal(await readFile(path.join(dir, output), "utf8"), before);
});

test("route permission changes invalidate the existing review before writes", async (t) => {
  const { dir, registry } = await fixture(t);
  await reviewFixture(dir);
  assert.equal(run(dir).status, 0);
  const before = await readFile(path.join(dir, output), "utf8");
  registry.modules[0].routes[0].permission = "care.new_permission";
  await writeFile(path.join(dir, registryFile), JSON.stringify(registry));
  const result = run(dir);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Registration changed/);
  assert.equal(await readFile(path.join(dir, output), "utf8"), before);
});

test("new routes and consumers cannot disappear behind a previously generated ledger", async (t) => {
  const { dir, registry } = await fixture(t);
  assert.equal(run(dir).status, 0);
  registry.modules[0].routes.push({ ...registry.modules[0].routes[0], path: "/v1/new-route" });
  registry.modules[0].consumers.push({
    name: "new-worker",
    subject: "new.requested.v1",
    after_commit: false,
  });
  await writeFile(path.join(dir, registryFile), JSON.stringify(registry));
  const stale = run(dir, "--check");
  assert.notEqual(stale.status, 0);
  assert.match(stale.stderr, /stale/);
  assert.equal(run(dir).status, 0);
  const ledger = JSON.parse(await readFile(path.join(dir, output), "utf8"));
  assert.equal(ledger.summary.unclassified, 6);
});

test("removed routes and duplicate reviews are rejected instead of retaining stale classifications", async (t) => {
  const { dir, registry } = await fixture(t);
  const review = await reviewFixture(dir);
  await writeFile(
    path.join(dir, reviewFile),
    JSON.stringify({ format_version: 1, reviews: [review, review] }),
  );
  const duplicate = run(dir);
  assert.notEqual(duplicate.status, 0);
  assert.match(duplicate.stderr, /Duplicate review/);
  await writeFile(
    path.join(dir, reviewFile),
    JSON.stringify({ format_version: 1, reviews: [review] }),
  );
  registry.modules[0].routes = [];
  await writeFile(path.join(dir, registryFile), JSON.stringify(registry));
  const removed = run(dir);
  assert.notEqual(removed.status, 0);
  assert.match(removed.stderr, /no registered surface/);
});

for (const [name, overrides, expected] of [
  ["unrecognized authority", { authority_kind: "tenant_admin_exempt" }, /Explicit authority/],
  ["missing remaining proof", { remaining: [] }, /Remaining runtime acceptance/],
  ["missing sources", { authority_sources: [] }, /Pinned authority source/],
  [
    "path traversal",
    { authority_sources: [{ path: "backend/../secret", symbol: "x", sha256: "0".repeat(64) }] },
    /Invalid authority source/,
  ],
  ["unverified acceptance marker", { accepted: true }, /Unexpected review field/],
]) {
  test(`rejects ${name} without changing the ledger`, async (t) => {
    const { dir } = await fixture(t);
    await reviewFixture(dir, overrides);
    const before = await readFile(path.join(dir, output), "utf8");
    const result = run(dir);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, expected);
    assert.equal(await readFile(path.join(dir, output), "utf8"), before);
  });
}

test("duplicate HTTP registration across owners cannot be hidden by module-specific keys", async (t) => {
  const { dir, registry } = await fixture(t);
  registry.modules.push({ ...registry.modules[0], name: "other" });
  await writeFile(path.join(dir, registryFile), JSON.stringify(registry));
  const result = run(dir);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Duplicate HTTP/);
});

test("unknown registry transport fields fail closed rather than claiming inventory coverage", async (t) => {
  const { dir, registry } = await fixture(t);
  registry.modules[0].timers = [{ name: "untracked" }];
  await writeFile(path.join(dir, registryFile), JSON.stringify(registry));
  const result = run(dir);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Unexpected module field/);
});

test("consumer transaction phase changes require a fresh source review", async (t) => {
  const { dir, registry } = await fixture(t);
  await reviewFixture(dir, { authority_kind: "system_continuation" }, "consumer");
  assert.equal(run(dir).status, 0);
  const before = await readFile(path.join(dir, output), "utf8");
  registry.modules[0].consumers[0].after_commit = false;
  await writeFile(path.join(dir, registryFile), JSON.stringify(registry));
  const result = run(dir);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Registration changed/);
  assert.equal(await readFile(path.join(dir, output), "utf8"), before);
});

test("fully classified fixture passes the classification gate without asserting cutoff acceptance", async (t) => {
  const { dir } = await fixture(t);
  const reviews = [];
  for (const [kind, authority] of [
    ["http", "hospital_operational"],
    ["consumer", "system_continuation"],
    ["broadcast", "system_continuation"],
    ["unauthenticated_http", "public_entry"],
  ]) {
    reviews.push(await reviewFixture(dir, { authority_kind: authority }, kind));
  }
  await writeFile(path.join(dir, reviewFile), JSON.stringify({ format_version: 1, reviews }));
  assert.equal(run(dir).status, 0);
  const result = run(dir, "--check", "--require-classified");
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /0 unclassified/);
  assert.match(result.stdout, /No runtime acceptance/);
});
