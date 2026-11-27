import { test } from "node:test";
import assert from "node:assert/strict";
import { copyFile, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const artifacts = [
  "backend/internal/modules/pharmacy/verification_inputs_generated.go",
  "packages/api/src/pharmacy-verification-contract.ts",
  "backend/internal/modules/pharmacy/verification_policy_contract_generated.sql",
];

async function fixture(t) {
  const dir = await mkdtemp(path.join(tmpdir(), "pharmacy-verification-contract-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  for (const file of [
    "package.json",
    "packages/api/package.json",
    "contracts/pharmacy-verification.json",
    "scripts/generate-pharmacy-verification.mjs",
    "scripts/lib/contract-codegen.mjs",
    ...artifacts,
  ]) {
    await mkdir(path.dirname(path.join(dir, file)), { recursive: true });
    await copyFile(path.join(root, file), path.join(dir, file));
  }
  await symlink(path.join(root, "node_modules"), path.join(dir, "node_modules"), "dir");
  return dir;
}

function generate(dir, ...args) {
  return spawnSync(
    process.execPath,
    [path.join(dir, "scripts/generate-pharmacy-verification.mjs"), ...args],
    {
      cwd: dir,
      encoding: "utf8",
      timeout: 20000,
    },
  );
}

test("verification generation is repeatable and detects either stale artifact", async (t) => {
  const dir = await fixture(t);
  for (const file of artifacts) {
    const before = await readFile(path.join(dir, file), "utf8");
    assert.equal(generate(dir).status, 0);
    assert.equal(await readFile(path.join(dir, file), "utf8"), before);
    await writeFile(path.join(dir, file), before + "\n// stale\n");
    const check = generate(dir, "--check");
    assert.notEqual(check.status, 0);
    assert.match(check.stderr, /stale/);
    assert.equal(generate(dir).status, 0);
  }
  assert.equal(generate(dir, "--check").status, 0);
});

test("canonical clinical-note bounds reach both server and browser", async (t) => {
  const dir = await fixture(t);
  const sourceFile = path.join(dir, "contracts/pharmacy-verification.json");
  const source = JSON.parse(await readFile(sourceFile, "utf8"));
  source.models.PrescriptionVerificationDecisionInput.find(
    (field) => field.name === "clinical_note",
  ).max = 1000;
  await writeFile(sourceFile, JSON.stringify(source));
  const result = generate(dir);
  assert.equal(result.status, 0, result.stderr);
  assert.match(
    await readFile(path.join(dir, artifacts[0]), "utf8"),
    /verificationNoteMaxUTF16\s*=\s*1000/,
  );
  assert.match(
    await readFile(path.join(dir, artifacts[1]), "utf8"),
    /\.max\(1000, "Use at most 1000 characters\."\)/,
  );
});

test("an unspecified decision rule cannot silently produce permissive outputs", async (t) => {
  const dir = await fixture(t);
  const before = await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8")));
  const sourceFile = path.join(dir, "contracts/pharmacy-verification.json");
  const source = JSON.parse(await readFile(sourceFile, "utf8"));
  delete source.decisions.rejected;
  await writeFile(sourceFile, JSON.stringify(source));
  const result = generate(dir);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /explicit intent requirements/);
  assert.deepEqual(
    await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8"))),
    before,
  );
});

test("policy interval constraints remain independent across Go, Zod and SQL", async (t) => {
  const dir = await fixture(t);
  const sourceFile = path.join(dir, "contracts/pharmacy-verification.json");
  const source = JSON.parse(await readFile(sourceFile, "utf8"));
  source.models.VerificationPolicyDraftInput.find(
    (field) => field.name === "query_response_target_seconds",
  ).max = 1000;
  await writeFile(sourceFile, JSON.stringify(source));
  const result = generate(dir);
  assert.equal(result.status, 0, result.stderr);
  assert.match(
    await readFile(path.join(dir, artifacts[0]), "utf8"),
    /verificationPolicyResponseDurationMax\s*=\s*1000/,
  );
  assert.match(
    await readFile(path.join(dir, artifacts[1]), "utf8"),
    /query_response_target_seconds: z\.number\(\)\.int\(\)\.min\(1\)\.max\(1000\)/,
  );
  assert.match(
    await readFile(path.join(dir, artifacts[2]), "utf8"),
    /response_seconds BETWEEN 1 AND 1000/,
  );
});
