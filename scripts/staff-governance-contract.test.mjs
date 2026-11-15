import { test } from "node:test";
import assert from "node:assert/strict";
import { copyFile, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const artifacts = [
  "backend/internal/modules/iam/governance_types_generated.go",
  "packages/api/src/staff-governance-contract.ts",
];
async function fixture(t) {
  const dir = await mkdtemp(path.join(tmpdir(), "staff-governance-contract-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  for (const file of [
    "package.json",
    "packages/api/package.json",
    "scripts/generate-staff-governance.mjs",
    "scripts/lib/contract-codegen.mjs",
    "contracts/staff-governance.json",
    ...artifacts,
  ]) {
    await mkdir(path.dirname(path.join(dir, file)), { recursive: true });
    await copyFile(path.join(root, file), path.join(dir, file));
  }
  await symlink(path.join(root, "node_modules"), path.join(dir, "node_modules"), "dir");
  return {
    dir,
    source: JSON.parse(await readFile(path.join(dir, "contracts/staff-governance.json"), "utf8")),
  };
}
function generate(dir, ...args) {
  return spawnSync(
    process.execPath,
    [path.join(dir, "scripts/generate-staff-governance.mjs"), ...args],
    {
      cwd: dir,
      encoding: "utf8",
      timeout: 20000,
    },
  );
}

test("governance generation is repeatable and checks both canonical artifacts", async (t) => {
  const { dir } = await fixture(t);
  const before = await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8")));
  for (const args of [[], ["--check"]]) {
    const result = generate(dir, ...args);
    assert.equal(result.status, 0, result.stderr);
  }
  assert.deepEqual(
    await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8"))),
    before,
  );
  for (const file of artifacts) {
    await writeFile(path.join(dir, file), "stale\n");
    const result = generate(dir, "--check");
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /stale/i);
    const restored = generate(dir);
    assert.equal(restored.status, 0, restored.stderr);
  }
});

test("changing the canonical text bound changes Go and browser validation together", async (t) => {
  const { dir, source } = await fixture(t);
  source.text_limits.reason = 320;
  await writeFile(path.join(dir, "contracts/staff-governance.json"), JSON.stringify(source));
  const result = generate(dir);
  assert.equal(result.status, 0, result.stderr);
  const [go, ts] = await Promise.all(
    artifacts.map((file) => readFile(path.join(dir, file), "utf8")),
  );
  assert.match(go, /governanceReasonMaxUTF16\s*=\s*320/);
  assert.equal((ts.match(/value.length <= 320/g) ?? []).length, 2);
  assert.doesNotMatch(ts, /value.length <= 500/);
});

test("an ambiguous fixed-template catalog fails before changing any artifact", async (t) => {
  const { dir, source } = await fixture(t);
  source.templates.push({ ...source.templates[0] });
  const before = await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8")));
  await writeFile(path.join(dir, "contracts/staff-governance.json"), JSON.stringify(source));
  const result = generate(dir);
  assert.notEqual(result.status, 0, "duplicate keys cannot select two different authority bundles");
  assert.match(result.stderr, /duplicate|template/i);
  assert.deepEqual(
    await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8"))),
    before,
  );
});

test("invalid fixed-template metadata fails before generating a false catalog", async (t) => {
  for (const [name, change] of Object.entries({
    "missing name": (template) => delete template.name,
    "unknown field": (template) => (template.label = "Unreviewed alias"),
    "duplicate scopes": (template) => (template.scope_kinds = ["hospital", "hospital"]),
    "unknown permission syntax": (template) => (template.permissions = ["workforce.*"]),
  })) {
    await t.test(name, async (t) => {
      const { dir, source } = await fixture(t);
      change(source.templates[0]);
      const before = await Promise.all(
        artifacts.map((file) => readFile(path.join(dir, file), "utf8")),
      );
      await writeFile(path.join(dir, "contracts/staff-governance.json"), JSON.stringify(source));
      const result = generate(dir);
      assert.notEqual(
        result.status,
        0,
        "a malformed template must not become generated Go/browser authority",
      );
      assert.match(result.stderr, /template/i);
      assert.deepEqual(
        await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8"))),
        before,
      );
    });
  }
});
