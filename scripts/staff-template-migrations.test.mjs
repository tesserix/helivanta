import { test } from "node:test";
import { createHash } from "node:crypto";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, writeFile, copyFile, symlink, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const artifacts = [
  "backend/internal/modules/iam/staff_access_generated.go",
  "packages/api/src/staff-access-contract.ts",
  "backend/internal/modules/iam/contract/assignment_scope_generated.go",
  "backend/internal/modules/iam/contract/assignment_projection_generated.go",
  "backend/internal/modules/workforce/contract/assignment_scope_generated.go",
  "backend/internal/modules/iam/staff_access_templates_generated.go",
  "packages/api/src/staff-access-templates.ts",
  "backend/internal/modules/iam/assignment_templates_generated.sql",
];
async function fixture(t) {
  const dir = await mkdtemp(path.join(tmpdir(), "staff-template-history-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  for (const file of [
    "package.json",
    "packages/api/package.json",
    "scripts/generate-staff-access.mjs",
    "scripts/lib/contract-codegen.mjs",
    "contracts/staff-access.json",
    "backend/internal/modules/iam/assignment_activation.sql",
    ...artifacts,
  ]) {
    await mkdir(path.dirname(path.join(dir, file)), { recursive: true });
    await copyFile(path.join(root, file), path.join(dir, file));
  }
  await symlink(path.join(root, "node_modules"), path.join(dir, "node_modules"), "dir");
  await symlink(
    path.join(root, "packages/api/node_modules"),
    path.join(dir, "packages/api/node_modules"),
    "dir",
  );
  const contractPath = path.join(dir, "contracts/staff-access.json");
  const contract = JSON.parse(await readFile(contractPath, "utf8"));
  return { dir, contract, contractPath };
}
function generate(dir, ...args) {
  return spawnSync(
    process.execPath,
    [path.join(dir, "scripts/generate-staff-access.mjs"), ...args],
    {
      cwd: dir,
      encoding: "utf8",
      timeout: 20000,
    },
  );
}

test("template regeneration refuses to rewrite historical authority before any artifact changes", async (t) => {
  const { dir, contract, contractPath } = await fixture(t);
  const before = await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8")));
  const templates =
    contract.templates ?? contract.template_migrations.flatMap((entry) => entry.templates);
  templates[0].permissions = ["workforce.payroll.approve"];
  await writeFile(contractPath, JSON.stringify(contract));
  const result = generate(dir);
  assert.notEqual(
    result.status,
    0,
    "historical permission changes must not regenerate migration 0014",
  );
  assert.match(result.stderr, /immutable|historical|digest/i);
  const after = await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8")));
  assert.deepEqual(after, before, "a rejected catalog must not partially update models or SQL");
});

test("unchanged catalog regenerates and checks without changing public artifacts", async (t) => {
  const { dir } = await fixture(t);
  const before = await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8")));
  for (const args of [[], ["--check"]]) {
    const result = generate(dir, ...args);
    assert.equal(result.status, 0, result.stderr);
  }
  const after = await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8")));
  assert.deepEqual(after, before);
});

async function extension(contract) {
  const sql = await readFile(
    path.join(root, "backend/internal/modules/iam/testdata/staff_template_append.sql"),
    "utf8",
  );
  const templates = [
    { ...contract.template_migrations[0].templates[0], key: "workforce.leave_reviewer.v2" },
  ];
  const fields = contract.models.TemplateDefinition.map((field) => field.name);
  const normalized = templates.map((template) =>
    Object.fromEntries(fields.map((field) => [field, template[field]])),
  );
  return {
    definitions_sha256: createHash("sha256").update(JSON.stringify(normalized)).digest("hex"),
    id: "0020_iam_test_templates",
    file: "assignment_test_templates_generated.sql",
    sql_sha256: createHash("sha256").update(sql).digest("hex"),
    templates,
  };
}

test("a new template version generates additive SQL and leaves every historical mapping unchanged", async (t) => {
  const { dir, contract, contractPath } = await fixture(t);
  const baseline = await readFile(path.join(dir, artifacts.at(-1)), "utf8");
  const entry = await extension(contract);
  contract.template_migrations.push(entry);
  await writeFile(contractPath, JSON.stringify(contract));
  const result = generate(dir);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(await readFile(path.join(dir, artifacts.at(-1)), "utf8"), baseline);
  const sql = await readFile(path.join(dir, "backend/internal/modules/iam", entry.file), "utf8");
  assert.equal(
    sql,
    await readFile(
      path.join(root, "backend/internal/modules/iam/testdata/staff_template_append.sql"),
      "utf8",
    ),
  );
  for (const file of [artifacts[1], artifacts[5], artifacts[6]])
    assert.match(await readFile(path.join(dir, file), "utf8"), /workforce\.leave_reviewer\.v2/);
  const publicDefinitions = await readFile(path.join(dir, artifacts[6]), "utf8");
  assert.doesNotMatch(publicDefinitions, /sql_sha256|0014_iam|0020_iam/);
  const check = generate(dir, "--check");
  assert.equal(check.status, 0, check.stderr);
});

test("invalid or conflicting catalog history fails before any artifact writes", async (t) => {
  const cases = [
    [
      "missing history",
      (c) => {
        delete c.template_migrations;
      },
    ],
    [
      "empty history",
      (c) => {
        c.template_migrations = [];
      },
    ],
    [
      "parallel legacy definitions",
      (c) => {
        c.templates = c.template_migrations[0].templates;
      },
    ],
    [
      "changed historical ID",
      (c) => {
        c.template_migrations[0].id = "0013_iam_staff_templates";
      },
    ],
    [
      "changed historical file",
      (c) => {
        c.template_migrations[0].file = "assignment_changed_generated.sql";
      },
    ],
    [
      "invalid definitions digest",
      (c) => {
        c.template_migrations[1].definitions_sha256 = "invalid";
      },
    ],
    [
      "wrong definitions digest",
      (c) => {
        c.template_migrations[1].definitions_sha256 = "0".repeat(64);
      },
    ],
    [
      "unknown definition fields",
      (c) => {
        c.template_migrations[1].templates[0].unreviewed = true;
      },
    ],
    [
      "invalid digest",
      (c) => {
        c.template_migrations[1].sql_sha256 = "invalid";
      },
    ],
    [
      "wrong reviewed digest",
      (c) => {
        c.template_migrations[1].sql_sha256 = "0".repeat(64);
      },
    ],
    [
      "reused migration ID",
      (c) => {
        c.template_migrations[1].id = c.template_migrations[0].id;
      },
    ],
    [
      "reused numeric migration phase",
      (c) => {
        c.template_migrations[1].id = "0014_iam_zeta";
      },
    ],
    [
      "reordered migrations",
      (c) => {
        c.template_migrations.reverse();
      },
    ],
    [
      "reused artifact path",
      (c) => {
        c.template_migrations[1].file = c.template_migrations[0].file;
      },
    ],
    [
      "path traversal",
      (c) => {
        c.template_migrations[1].file = "../../outside.sql";
      },
    ],
    [
      "reused template key",
      (c) => {
        c.template_migrations[1].templates[0].key = c.template_migrations[0].templates[0].key;
      },
    ],
    [
      "empty introduction",
      (c) => {
        c.template_migrations[1].templates = [];
      },
    ],
    [
      "unknown family",
      (c) => {
        c.template_migrations[1].templates[0].family = "unreviewed";
      },
    ],
    [
      "unsupported compound template",
      (c) => {
        c.template_migrations[1].templates[0].permissions.push("workforce.documents.manage");
      },
    ],
  ];
  for (const [name, mutate] of cases)
    await t.test(name, async (t) => {
      const { dir, contract, contractPath } = await fixture(t);
      contract.template_migrations.push(await extension(contract));
      mutate(contract);
      const before = await Promise.all(
        artifacts.map((file) => readFile(path.join(dir, file), "utf8")),
      );
      await writeFile(contractPath, JSON.stringify(contract));
      for (const args of [[], ["--check"]]) {
        const result = generate(dir, ...args);
        assert.notEqual(result.status, 0, `${name} must be refused in ${args[0] ?? "normal"} mode`);
      }
      const after = await Promise.all(
        artifacts.map((file) => readFile(path.join(dir, file), "utf8")),
      );
      assert.deepEqual(after, before);
    });
});

test("historical template family cannot change while retaining the same permission mapping", async (t) => {
  const { dir, contract, contractPath } = await fixture(t);
  const baseline = await readFile(path.join(dir, artifacts.at(-1)), "utf8");
  contract.template_migrations[0].templates[0].family = "identity";
  await writeFile(contractPath, JSON.stringify(contract));
  const result = generate(dir);
  assert.notEqual(result.status, 0, "historical family changes require a new template version");
  assert.match(result.stderr, /immutable|historical|digest/i);
  assert.equal(await readFile(path.join(dir, artifacts.at(-1)), "utf8"), baseline);
});

test("historical snapshot SQL changes fail before model or migration output", async (t) => {
  const { dir } = await fixture(t);
  const before = await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8")));
  const file = path.join(dir, "backend/internal/modules/iam/assignment_activation.sql");
  const original = await readFile(file, "utf8");
  const marker =
    "CREATE OR REPLACE FUNCTION iam_assignment_snapshot_record() RETURNS trigger LANGUAGE plpgsql AS $$\nBEGIN";
  assert.ok(original.includes(marker));
  await writeFile(
    file,
    original.replace(marker, marker + "\n -- synthetic changed snapshot source"),
  );
  const result = generate(dir);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /immutable.*digest/i);
  const after = await Promise.all(
    artifacts.map((artifact) => readFile(path.join(dir, artifact), "utf8")),
  );
  assert.deepEqual(after, before);
});
