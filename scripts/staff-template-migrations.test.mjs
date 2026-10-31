import { test } from "node:test";
import { createHash } from "node:crypto";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, writeFile, copyFile, symlink, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const catalog = JSON.parse(await readFile(path.join(root, "contracts/staff-access.json"), "utf8"));
const artifacts = [
  "backend/internal/modules/iam/staff_access_generated.go",
  "packages/api/src/staff-access-contract.ts",
  "backend/internal/modules/iam/contract/assignment_scope_generated.go",
  "backend/internal/modules/iam/contract/assignment_projection_generated.go",
  "backend/internal/modules/workforce/contract/assignment_scope_generated.go",
  "backend/internal/modules/iam/staff_access_templates_generated.go",
  "packages/api/src/staff-access-templates.ts",
  ...catalog.template_migrations
    .filter((entry) => entry.file !== "assignment_templates_generated.sql")
    .map((entry) => "backend/internal/modules/iam/" + entry.file),
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
    id:
      String(Number.parseInt(contract.template_migrations.at(-1).id.slice(0, 4), 10) + 1).padStart(
        4,
        "0",
      ) + "_iam_test_templates",
    file: "assignment_test_templates_generated.sql",
    sql_sha256: createHash("sha256").update(sql).digest("hex"),
    templates,
  };
}

async function intakeExtension(contract) {
  const entry = await extension(contract);
  entry.id = entry.id.replace("_test_templates", "_hr_intake_templates");
  entry.file = "assignment_hr_intake_next_templates_generated.sql";
  entry.templates = [
    {
      key: "workforce.hr_intake.v2",
      name: "HR intake administrator",
      description:
        "Prepare hospital Staff profiles, employment drafts and protected evidence; cannot approve employment or grant access.",
      family: "intake",
      permissions: ["workforce.enrollment.manage", "workforce.documents.manage"],
    },
  ];
  const fields = contract.models.TemplateDefinition.map((field) => field.name);
  entry.definitions_sha256 = createHash("sha256")
    .update(
      JSON.stringify(
        entry.templates.map((template) =>
          Object.fromEntries(fields.map((field) => [field, template[field]])),
        ),
      ),
    )
    .digest("hex");
  const sql = (
    await readFile(
      path.join(root, "backend/internal/modules/iam/testdata/staff_template_append.sql"),
      "utf8",
    )
  ).replace(
    "WHEN 'workforce.leave_reviewer.v2' THEN '[\"workforce.leave.approve\"]'::jsonb",
    "WHEN 'workforce.hr_intake.v2' THEN '[\"workforce.enrollment.manage\",\"workforce.documents.manage\"]'::jsonb",
  );
  entry.sql_sha256 = createHash("sha256").update(sql).digest("hex");
  return { entry, sql };
}

test("HR intake introduces its exact compound authority without rewriting history", async (t) => {
  const { dir, contract, contractPath } = await fixture(t);
  const historical = await readFile(path.join(dir, artifacts.at(-1)), "utf8");
  const { entry, sql } = await intakeExtension(contract);
  contract.template_migrations.push(entry);
  await writeFile(contractPath, JSON.stringify(contract));
  const result = generate(dir);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(await readFile(path.join(dir, artifacts.at(-1)), "utf8"), historical);
  assert.equal(
    await readFile(path.join(dir, "backend/internal/modules/iam", entry.file), "utf8"),
    sql,
  );
  const publicDefinitions = await readFile(path.join(dir, artifacts[6]), "utf8");
  assert.match(publicDefinitions, /family: "intake"/);
  assert.match(
    publicDefinitions,
    /permissions: \["workforce.enrollment.manage", "workforce.documents.manage"\]/,
  );
  assert.doesNotMatch(publicDefinitions, /workforce\.(payroll|benefits|credentials|hospital)\./);
  assert.equal(generate(dir, "--check").status, 0);
});

test("HR intake rejects incomplete, duplicated or broader permission bundles before writes", async (t) => {
  for (const permissions of [
    ["workforce.enrollment.manage"],
    ["workforce.documents.manage"],
    ["workforce.enrollment.manage", "workforce.enrollment.manage"],
    ["workforce.enrollment.manage", "workforce.payroll.approve"],
    ["workforce.enrollment.manage", "workforce.documents.manage", "workforce.hospital.manage"],
  ]) {
    await t.test(permissions.join(","), async (t) => {
      const { dir, contract, contractPath } = await fixture(t);
      const before = await Promise.all(
        artifacts.map((file) => readFile(path.join(dir, file), "utf8")),
      );
      const { entry } = await intakeExtension(contract);
      entry.templates[0].permissions = permissions;
      const fields = contract.models.TemplateDefinition.map((field) => field.name);
      entry.definitions_sha256 = createHash("sha256")
        .update(
          JSON.stringify(
            entry.templates.map((template) =>
              Object.fromEntries(fields.map((field) => [field, template[field]])),
            ),
          ),
        )
        .digest("hex");
      contract.template_migrations.push(entry);
      await writeFile(contractPath, JSON.stringify(contract));
      const result = generate(dir);
      assert.notEqual(result.status, 0);
      assert.match(result.stderr, /Invalid declared staff access template/);
      assert.deepEqual(
        await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8"))),
        before,
      );
    });
  }
});

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

test("closed discovery catalogs reject invalid or expanded legacy snapshots before writes", async (t) => {
  const cases = [
    [
      "missing",
      (c) => {
        delete c.template_catalogs;
      },
    ],
    ["duplicate", (c) => c.template_catalogs.push(structuredClone(c.template_catalogs[1]))],
    ["unknown_template", (c) => c.template_catalogs[1].template_keys.push("workforce.unknown.v1")],
    [
      "unreviewed",
      (c) => {
        c.template_catalogs[1].definitions_sha256 = "0".repeat(64);
      },
    ],
    [
      "expanded_legacy",
      (c) => {
        const catalog = c.template_catalogs[0];
        catalog.template_keys.push("workforce.hr_intake.v1");
        const templates = c.template_migrations.flatMap((migration) => migration.templates);
        const fields = c.models.TemplateDefinition.map((field) => field.name);
        catalog.definitions_sha256 = createHash("sha256")
          .update(
            JSON.stringify(
              catalog.template_keys.map((key) => {
                const template = templates.find((template) => template.key === key);
                return Object.fromEntries(fields.map((field) => [field, template[field]]));
              }),
            ),
          )
          .digest("hex");
      },
    ],
  ];
  for (const [name, mutate] of cases) {
    await t.test(name, async (t) => {
      const { dir, contract, contractPath } = await fixture(t);
      const before = await Promise.all(
        artifacts.map((file) => readFile(path.join(dir, file), "utf8")),
      );
      mutate(contract);
      await writeFile(contractPath, JSON.stringify(contract));
      const result = generate(dir);
      assert.notEqual(result.status, 0);
      assert.deepEqual(
        await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8"))),
        before,
      );
    });
  }
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
        c.template_migrations.at(-1).definitions_sha256 = "invalid";
      },
    ],
    [
      "wrong definitions digest",
      (c) => {
        c.template_migrations.at(-1).definitions_sha256 = "0".repeat(64);
      },
    ],
    [
      "unknown definition fields",
      (c) => {
        c.template_migrations.at(-1).templates[0].unreviewed = true;
      },
    ],
    [
      "invalid digest",
      (c) => {
        c.template_migrations.at(-1).sql_sha256 = "invalid";
      },
    ],
    [
      "wrong reviewed digest",
      (c) => {
        c.template_migrations.at(-1).sql_sha256 = "0".repeat(64);
      },
    ],
    [
      "reused migration ID",
      (c) => {
        c.template_migrations.at(-1).id = c.template_migrations[0].id;
      },
    ],
    [
      "reused numeric migration phase",
      (c) => {
        c.template_migrations.at(-1).id = "0014_iam_zeta";
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
        c.template_migrations.at(-1).file = c.template_migrations[0].file;
      },
    ],
    [
      "path traversal",
      (c) => {
        c.template_migrations.at(-1).file = "../../outside.sql";
      },
    ],
    [
      "reused template key",
      (c) => {
        c.template_migrations.at(-1).templates[0].key = c.template_migrations[0].templates[0].key;
      },
    ],
    [
      "empty introduction",
      (c) => {
        c.template_migrations.at(-1).templates = [];
      },
    ],
    [
      "unknown family",
      (c) => {
        c.template_migrations.at(-1).templates[0].family = "unreviewed";
      },
    ],
    [
      "unsupported compound template",
      (c) => {
        c.template_migrations.at(-1).templates[0].permissions.push("workforce.documents.manage");
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

test("employment reviewer catalog preserves earlier catalogs and historical SQL", async (t) => {
  const { dir, contract } = await fixture(t);
  const before = await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8")));
  const reviewer = contract.template_migrations
    .flatMap((entry) => entry.templates)
    .find((template) => template.key === "workforce.employment_reviewer.v1");
  assert.deepEqual(reviewer.permissions, ["workforce.employment.review"]);
  assert.equal(reviewer.family, "employment");
  assert.equal(
    contract.template_catalogs.find((catalog) => catalog.key === "legacy-v1").template_keys.length,
    3,
  );
  assert.equal(
    contract.template_catalogs.find((catalog) => catalog.key === "hr-intake-v1").template_keys
      .length,
    4,
  );
  assert.equal(
    contract.template_catalogs.find((catalog) => catalog.key === "employment-v1").template_keys
      .length,
    5,
  );
  const result = generate(dir, "--check");
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(
    await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8"))),
    before,
  );
});

test("employment reviewers cannot carry another permission or a broader bundle", async (t) => {
  for (const permissions of [
    [],
    ["workforce.leave.approve"],
    ["workforce.employment.review", "workforce.documents.manage"],
  ]) {
    const { dir, contract, contractPath } = await fixture(t);
    const before = await Promise.all(
      artifacts.map((file) => readFile(path.join(dir, file), "utf8")),
    );
    const entry = contract.template_migrations.find(
      (entry) => entry.id === "0021_iam_employment_templates",
    );
    entry.templates[0].permissions = permissions;
    const fields = contract.models.TemplateDefinition.map((field) => field.name);
    entry.definitions_sha256 = createHash("sha256")
      .update(
        JSON.stringify(
          entry.templates.map((template) =>
            Object.fromEntries(fields.map((field) => [field, template[field]])),
          ),
        ),
      )
      .digest("hex");
    await writeFile(contractPath, JSON.stringify(contract));
    const result = generate(dir);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /employment.*permission/i);
    assert.deepEqual(
      await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8"))),
      before,
    );
  }
});
