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
  "backend/internal/modules/iam/contract/assignment_department_scope_generated.go",
  "backend/internal/modules/organization/contract/assignment_department_generated.go",
  ...catalog.template_migrations
    .filter((entry) => entry.file !== "assignment_templates_generated.sql")
    .map((entry) => "backend/internal/modules/iam/" + entry.file),
  "backend/internal/modules/iam/contract/assignment_governance_scope_generated.go",
  "backend/internal/modules/workforce/contract/assignment_governance_scope_generated.go",
  "backend/internal/modules/workforce/contract/assignment_governance_hospital_scope_generated.go",
  "backend/internal/modules/workforce/contract/operating_governance_employer_scope_generated.go",
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

test("posting catalogs refuse incomplete, duplicated and expanded role authority before writes", async (t) => {
  for (const [key, permissions] of [
    ["workforce.posting_cutoff_admin.v1", ["workforce.posting.cutoff"]],
    [
      "workforce.posting_cutoff_admin.v1",
      [
        "workforce.posting.read",
        "workforce.posting.proposal.read",
        "workforce.posting.cutoff",
        "workforce.staff_actor.use",
        "workforce.posting.review",
      ],
    ],
    ["workforce.posting_source_consumer.v1", ["workforce.posting_source.use"]],
    [
      "workforce.posting_source_consumer.v1",
      ["workforce.posting_source.use", "workforce.staff_actor.use", "workforce.posting.read"],
    ],
    ["workforce.posting_management_reviewer.v1", ["workforce.posting.management.review"]],
    [
      "workforce.posting_management_reviewer.v1",
      [
        "workforce.posting.management.review",
        "workforce.posting.review",
        "workforce.staff_actor.use",
      ],
    ],
    ["workforce.posting_submitter.v1", ["workforce.posting.submit"]],
    ["workforce.posting_reviewer.v1", ["workforce.posting.review"]],
    [
      "workforce.posting_reviewer.v1",
      [
        "workforce.posting.read",
        "workforce.posting.proposal.read",
        "workforce.posting.review",
        "workforce.posting.prepare",
        "organization.posting_choices.read",
        "workforce.staff_actor.use",
      ],
    ],
    [
      "workforce.posting_submitter.v1",
      [
        "workforce.posting.read",
        "workforce.posting.prepare",
        "workforce.posting.proposal.read",
        "workforce.posting.submit",
        "workforce.posting.review",
        "organization.posting_choices.read",
        "workforce.staff_actor.use",
      ],
    ],
    ["workforce.posting_reader.v1", ["workforce.posting.read"]],
    [
      "workforce.posting_reader.v1",
      ["workforce.posting.read", "workforce.posting.prepare", "workforce.staff_actor.use"],
    ],
    ["workforce.posting_reader.v1", ["workforce.posting.read", "workforce.posting.read"]],
    [
      "workforce.posting_preparer.v1",
      ["workforce.posting.read", "workforce.posting.prepare", "workforce.staff_actor.use"],
    ],
    [
      "workforce.posting_preparer.v1",
      [
        "workforce.posting.read",
        "workforce.posting.prepare",
        "organization.structure.manage",
        "workforce.staff_actor.use",
      ],
    ],
    [
      "workforce.posting_preparer.v1",
      [
        "workforce.posting.read",
        "workforce.posting.prepare",
        "organization.posting_choices.read",
        "workforce.staff_actor.use",
        "workforce.payroll.approve",
      ],
    ],
  ]) {
    await t.test(`${key}: ${permissions.join(",")}`, async (t) => {
      const { dir, contract, contractPath } = await fixture(t);
      const before = await Promise.all(
        artifacts.map((file) => readFile(path.join(dir, file), "utf8")),
      );
      const template = contract.template_migrations
        .flatMap((migration) => migration.templates)
        .find((template) => template.key === key);
      assert.ok(template);
      template.permissions = permissions;
      await writeFile(contractPath, JSON.stringify(contract));
      const result = generate(dir);
      assert.notEqual(result.status, 0);
      assert.match(
        result.stderr,
        /Posting templates require their exact role and Staff source permissions/,
      );
      const after = await Promise.all(
        artifacts.map((file) => readFile(path.join(dir, file), "utf8")),
      );
      assert.deepEqual(after, before);
    });
  }
});

async function extension(contract) {
  const sql = await readFile(
    path.join(root, "backend/internal/modules/iam/testdata/staff_template_append.sql"),
    "utf8",
  );
  const templates = [
    {
      ...contract.template_migrations[0].templates[0],
      key: "workforce.leave_reviewer.v2",
      scope_kinds: ["hospital"],
      establishment_kinds: ["hospital"],
    },
  ];
  const fields = contract.models.TemplateDefinition.map((field) => field.name);
  const normalized = templates.map((template) =>
    Object.fromEntries(fields.map((field) => [field, template[field]])),
  );
  return {
    definitions_sha256: createHash("sha256").update(JSON.stringify(normalized)).digest("hex"),
    // IAM 0033 is the hand-written establishment root, so appended templates start at 0034.
    id:
      String(
        Math.max(Number.parseInt(contract.template_migrations.at(-1).id.slice(0, 4), 10) + 1, 34),
      ).padStart(4, "0") + "_iam_test_templates",
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
      scope_kinds: ["hospital"],
      establishment_kinds: ["hospital"],
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
  )
    .replace(
      "WHEN 'workforce.leave_reviewer.v2' THEN '[\"workforce.leave.approve\"]'::jsonb",
      "WHEN 'workforce.hr_intake.v2' THEN '[\"workforce.enrollment.manage\",\"workforce.documents.manage\"]'::jsonb",
    )
    .replaceAll(
      "WHEN 'workforce.leave_reviewer.v2' THEN ARRAY['hospital']::text[]",
      "WHEN 'workforce.hr_intake.v2' THEN ARRAY['hospital']::text[]",
    );
  entry.sql_sha256 = createHash("sha256").update(sql).digest("hex");
  return { entry, sql };
}

async function pharmacyBindingExtension(contract, kind = "preparer") {
  const entry = await extension(contract);
  entry.id = entry.id.replace("_test_templates", "_pharmacy_binding_templates");
  entry.file = "assignment_pharmacy_binding_templates_generated.sql";
  entry.templates = [
    {
      key: `pharmacy.department_binding_${kind}.v2`,
      name: `Pharmacy department binding ${kind}`,
      description:
        "Prepare outlet-to-department relationships within one hospital; cannot approve relationships or operate a pharmacy.",
      family: "pharmacy_binding",
      permissions: [
        "pharmacy.staff_binding.read",
        `pharmacy.staff_binding.${kind === "reviewer" ? "review" : "prepare"}`,
        "organization.posting_choices.read",
        "workforce.staff_actor.use",
      ],
      scope_kinds: ["hospital"],
      establishment_kinds: ["hospital"],
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
  const template = entry.templates[0];
  const sql = (
    await readFile(
      path.join(root, "backend/internal/modules/iam/testdata/staff_template_append.sql"),
      "utf8",
    )
  )
    .replace(
      "WHEN 'workforce.leave_reviewer.v2' THEN '[\"workforce.leave.approve\"]'::jsonb",
      `WHEN '${template.key}' THEN '${JSON.stringify(template.permissions)}'::jsonb`,
    )
    .replaceAll(
      "WHEN 'workforce.leave_reviewer.v2' THEN ARRAY['hospital']::text[]",
      `WHEN '${template.key}' THEN ARRAY['hospital']::text[]`,
    );
  entry.sql_sha256 = createHash("sha256").update(sql).digest("hex");
  return { entry, sql };
}

test("a Pharmacy binding preparer can use canonical Staff without gaining review or operating authority", async (t) => {
  const { dir, contract, contractPath } = await fixture(t);
  const historical = await Promise.all(
    artifacts
      .filter((file) => file.endsWith(".sql"))
      .map((file) => readFile(path.join(dir, file), "utf8")),
  );
  const { entry, sql } = await pharmacyBindingExtension(contract);
  contract.template_migrations.push(entry);
  await writeFile(contractPath, JSON.stringify(contract));
  const result = generate(dir);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(
    await readFile(path.join(dir, "backend/internal/modules/iam", entry.file), "utf8"),
    sql,
  );
  assert.deepEqual(
    await Promise.all(
      artifacts
        .filter((file) => file.endsWith(".sql"))
        .map((file) => readFile(path.join(dir, file), "utf8")),
    ),
    historical,
  );
  const publicDefinitions = await readFile(
    path.join(dir, "packages/api/src/staff-access-templates.ts"),
    "utf8",
  );
  assert.match(publicDefinitions, /pharmacy\.department_binding_preparer\.v2/);
  assert.equal(generate(dir, "--check").status, 0);
});

test("a separate Pharmacy binding reviewer has review rather than preparation authority", async (t) => {
  const { dir, contract, contractPath } = await fixture(t);
  const { entry, sql } = await pharmacyBindingExtension(contract, "reviewer");
  contract.template_migrations.push(entry);
  await writeFile(contractPath, JSON.stringify(contract));
  const result = generate(dir);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(
    await readFile(path.join(dir, "backend/internal/modules/iam", entry.file), "utf8"),
    sql,
  );
  const publicDefinitions = await readFile(
    path.join(dir, "packages/api/src/staff-access-templates.ts"),
    "utf8",
  );
  assert.match(publicDefinitions, /pharmacy\.department_binding_reviewer\.v2/);
  assert.equal(generate(dir, "--check").status, 0);
});

test("Pharmacy relationship templates reject missing sources and unrelated authority before writes", async (t) => {
  for (const kind of ["preparer", "reviewer"]) {
    for (const change of [
      (template) => {
        template.permissions.pop();
      },
      (template) => {
        template.permissions.splice(2, 1);
      },
      (template) => {
        template.permissions.push("workforce.enrollment.manage");
      },
      (template) => {
        template.permissions.push("pharmacy.prescription.read");
      },
      (template) => {
        template.permissions.push("pharmacy.sale.create");
      },
      (template) => {
        template.permissions.push("pharmacy.staff_binding.prepare");
      },
      (template) => {
        template.key = "workforce.department_binding_preparer.v1";
      },
      (template) => {
        template.key = "pharmacy.department_binding_admin.v1";
      },
    ]) {
      const { dir, contract, contractPath } = await fixture(t);
      const before = await Promise.all(
        artifacts.map((file) => readFile(path.join(dir, file), "utf8")),
      );
      const { entry } = await pharmacyBindingExtension(contract, kind);
      change(entry.templates[0]);
      contract.template_migrations.push(entry);
      await writeFile(contractPath, JSON.stringify(contract));
      const result = generate(dir);
      assert.notEqual(result.status, 0);
      assert.match(
        result.stderr,
        /Pharmacy binding templates require their exact role and Staff source permissions/,
      );
      assert.deepEqual(
        await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8"))),
        before,
      );
    }
  }
});

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

async function rejectsBeforeWrites(t, mutate, pattern) {
  const { dir, contract, contractPath } = await fixture(t);
  const before = await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8")));
  await mutate(contract);
  await writeFile(contractPath, JSON.stringify(contract));
  const result = generate(dir);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, pattern);
  assert.deepEqual(
    await Promise.all(artifacts.map((file) => readFile(path.join(dir, file), "utf8"))),
    before,
  );
}
const departmentKey = "workforce.department_posting_reviewer.v1";
const departmentTemplate = (contract) =>
  contract.template_migrations
    .flatMap((migration) => migration.templates)
    .find((template) => template.key === departmentKey);

test("templates introduced from 0030 must declare their scope kinds before writes", async (t) => {
  await rejectsBeforeWrites(
    t,
    async (contract) => {
      const entry = await extension(contract);
      delete entry.templates[0].scope_kinds;
      contract.template_migrations.push(entry);
    },
    /scope kinds/i,
  );
});

test("scope kinds must be a reviewed ordered subset before writes", async (t) => {
  for (const scopeKinds of [
    [],
    ["ward"],
    ["department", "hospital"],
    ["hospital", "hospital"],
    "hospital",
    null,
  ]) {
    await t.test(JSON.stringify(scopeKinds), async (t) => {
      await rejectsBeforeWrites(
        t,
        async (contract) => {
          const entry = await extension(contract);
          entry.templates[0].scope_kinds = scopeKinds;
          contract.template_migrations.push(entry);
        },
        /scope kinds/i,
      );
    });
  }
});

test("historical templates stay hospital-only and cannot declare a scope", async (t) => {
  await rejectsBeforeWrites(
    t,
    async (contract) => {
      contract.template_migrations[0].templates[0].scope_kinds = ["hospital"];
    },
    /scope kinds|immutable|digest/i,
  );
});

test("department posting reviewer keeps exact reviewer authority and department-only scope", async (t) => {
  for (const [name, mutate] of [
    ["hospital scope added", (template) => (template.scope_kinds = ["hospital", "department"])],
    ["hospital scope only", (template) => (template.scope_kinds = ["hospital"])],
    [
      "broader permissions",
      (template) => template.permissions.splice(1, 0, "workforce.posting.prepare"),
    ],
    ["missing staff actor", (template) => template.permissions.pop()],
  ]) {
    await t.test(name, async (t) => {
      await rejectsBeforeWrites(
        t,
        async (contract) => {
          const template = departmentTemplate(contract);
          assert.ok(template, "the department posting reviewer must be declared");
          mutate(template);
        },
        /Posting templates require their exact role|department scope/i,
      );
    });
  }
});

const departmentLeaveKey = "workforce.department_leave_reviewer.v1";

test("department leave reviewer keeps exact leave authority and department-only scope", async (t) => {
  for (const [name, mutate] of [
    ["hospital scope added", (template) => (template.scope_kinds = ["hospital", "department"])],
    ["hospital scope only", (template) => (template.scope_kinds = ["hospital"])],
    ["broader permissions", (template) => template.permissions.push("workforce.leave.configure")],
    ["missing staff actor", (template) => template.permissions.pop()],
    ["posting family", (template) => (template.family = "posting")],
  ]) {
    await t.test(name, async (t) => {
      await rejectsBeforeWrites(
        t,
        async (contract) => {
          const template = contract.template_migrations
            .flatMap((migration) => migration.templates)
            .find((entry) => entry.key === departmentLeaveKey);
          assert.ok(template, "the department leave reviewer must be declared");
          mutate(template);
        },
        /Leave templates require their exact|Posting templates require their exact role|department scope/i,
      );
    });
  }
});

test("a hospital leave template cannot gain department scope", async (t) => {
  await rejectsBeforeWrites(
    t,
    async (contract) => {
      const entry = await extension(contract);
      entry.templates[0].scope_kinds = ["department"];
      contract.template_migrations.push(entry);
    },
    /department scope/i,
  );
});

test("only the reviewed department template may declare department scope", async (t) => {
  await rejectsBeforeWrites(
    t,
    async (contract) => {
      const entry = await extension(contract);
      entry.templates[0].scope_kinds = ["hospital", "department"];
      contract.template_migrations.push(entry);
    },
    /department scope/i,
  );
});

test("generated templates publish scope kinds and the department scope function", async () => {
  const go = await readFile(
    path.join(root, "backend/internal/modules/iam/staff_access_templates_generated.go"),
    "utf8",
  );
  assert.match(
    go,
    /Key: "workforce\.leave_reviewer\.v1",[^\n]*?ScopeKinds: \[\]string\{"hospital"\}, EstablishmentKinds: \[\]string\{"hospital"\}\}/,
  );
  assert.match(
    go,
    /Key: "workforce\.department_posting_reviewer\.v1",[^\n]*?ScopeKinds: \[\]string\{"department"\}, EstablishmentKinds: \[\]string\{"hospital"\}\}/,
  );
  const ts = await readFile(path.join(root, "packages/api/src/staff-access-templates.ts"), "utf8");
  assert.match(ts, /scope_kinds: \["department"\]/);
  const entry = catalog.template_migrations.find((migration) =>
    migration.templates.some((template) => template.key === departmentKey),
  );
  assert.ok(entry && entry.id.startsWith("0030_iam_"));
  const sql = await readFile(path.join(root, "backend/internal/modules/iam", entry.file), "utf8");
  assert.match(sql, /CREATE OR REPLACE FUNCTION iam_assignment_template_scope_kinds\(key text\)/);
  assert.match(sql, /WHEN 'workforce\.leave_reviewer\.v1' THEN ARRAY\['hospital'\]::text\[\]/);
  assert.match(
    sql,
    /WHEN 'workforce\.department_posting_reviewer\.v1' THEN ARRAY\['department'\]::text\[\]/,
  );
  assert.match(
    go,
    /Key: "workforce\.department_leave_reviewer\.v1",[^\n]*?Family: "leave"[^\n]*?ScopeKinds: \[\]string\{"department"\}, EstablishmentKinds: \[\]string\{"hospital"\}\}/,
  );
  const leave = catalog.template_migrations.find((migration) =>
    migration.templates.some((template) => template.key === departmentLeaveKey),
  );
  assert.ok(leave && leave.id === "0032_iam_department_leave_review_templates");
  assert.match(
    await readFile(path.join(root, "backend/internal/modules/iam", leave.file), "utf8"),
    /WHEN 'workforce\.department_leave_reviewer\.v1' THEN ARRAY\['department'\]::text\[\]/,
  );
  for (const historical of catalog.template_migrations.filter((m) => m.id < "0027"))
    assert.doesNotMatch(
      await readFile(path.join(root, "backend/internal/modules/iam", historical.file), "utf8"),
      /scope_kinds/,
    );
});

test("partner membership templates refuse missing source checks or combined preparation/review", async (t) => {
  for (const mutate of [
    (template) => {
      template.permissions = template.permissions.filter(
        (permission) => permission !== "workforce.partner_member_source.use",
      );
    },
    (template) => {
      template.permissions.push("iam.partner_group.members.review");
    },
    (template) => {
      template.permissions.push("pharmacy.stock.adjust");
    },
  ]) {
    const { dir, contract, contractPath } = await fixture(t);
    const template = contract.template_migrations
      .flatMap((entry) => entry.templates)
      .find((entry) => entry.key === "iam.partner_group_member_preparer.v1");
    mutate(template);
    await writeFile(contractPath, JSON.stringify(contract));
    const result = generate(dir);
    assert.notEqual(result.status, 0);
    assert.match(
      result.stderr,
      /Partner membership templates require exact hospital-only role, actor and source permissions/,
    );
  }
});
test("templates introduced from 0034 must declare their establishment kinds before writes", async (t) => {
  await rejectsBeforeWrites(
    t,
    async (contract) => {
      const entry = await extension(contract);
      assert.ok(Number(entry.id.slice(0, 4)) >= 34);
      delete entry.templates[0].establishment_kinds;
      contract.template_migrations.push(entry);
    },
    /establishment kinds/i,
  );
});

test("establishment kinds must be a reviewed ordered subset before writes", async (t) => {
  for (const kinds of [
    [],
    ["pharmacy"],
    ["clinic"],
    ["pharmacy", "hospital"],
    ["hospital", "hospital"],
    ["hospital", "pharmacy", "clinic"],
    "hospital",
    null,
  ]) {
    await t.test(JSON.stringify(kinds), async (t) => {
      await rejectsBeforeWrites(
        t,
        async (contract) => {
          const entry = await extension(contract);
          entry.templates[0].establishment_kinds = kinds;
          contract.template_migrations.push(entry);
        },
        /establishment kinds/i,
      );
    });
  }
});

test("templates before 0034 stay hospital-rooted and cannot declare an establishment kind", async (t) => {
  for (const migrationId of [
    "0014_iam_staff_templates",
    "0032_iam_department_leave_review_templates",
  ]) {
    await t.test(migrationId, async (t) => {
      await rejectsBeforeWrites(
        t,
        async (contract) => {
          const historical = contract.template_migrations.find((entry) => entry.id === migrationId);
          assert.ok(historical, "the pre-0034 history fixture must exist");
          historical.templates[0].establishment_kinds = ["hospital"];
        },
        /establishment kinds/i,
      );
    });
  }
});

test("a pharmacy root needs a template that grants hospital-wide scope", async (t) => {
  await rejectsBeforeWrites(
    t,
    async (contract) => {
      const entry = await extension(contract);
      entry.templates = [
        {
          ...contract.template_migrations
            .flatMap((migration) => migration.templates)
            .find((template) => template.key === departmentLeaveKey),
          key: "workforce.department_leave_reviewer.v2",
          establishment_kinds: ["hospital", "pharmacy"],
        },
      ];
      contract.template_migrations.push(entry);
    },
    /pharmacy root/i,
  );
});

test("a pharmacy-assignable template regenerates the establishment kinds function", async (t) => {
  const { dir, contract, contractPath } = await fixture(t);
  const historical = await Promise.all(
    contract.template_migrations.map((migration) =>
      readFile(path.join(dir, "backend/internal/modules/iam", migration.file), "utf8"),
    ),
  );
  const entry = await extension(contract);
  entry.templates[0].establishment_kinds = ["hospital", "pharmacy"];
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
  const expected = (
    await readFile(
      path.join(root, "backend/internal/modules/iam/testdata/staff_template_append.sql"),
      "utf8",
    )
  ).replace(
    /(CREATE OR REPLACE FUNCTION iam_assignment_template_establishment_kinds[\s\S]*?WHEN 'workforce\.leave_reviewer\.v2' THEN )ARRAY\['hospital'\]/,
    "$1ARRAY['hospital','pharmacy']",
  );
  assert.match(
    expected,
    /WHEN 'workforce\.leave_reviewer\.v2' THEN ARRAY\['hospital','pharmacy'\]::text\[\]/,
  );
  entry.sql_sha256 = createHash("sha256").update(expected).digest("hex");
  contract.template_migrations.push(entry);
  await writeFile(contractPath, JSON.stringify(contract));
  const result = generate(dir);
  assert.equal(result.status, 0, result.stderr);
  const sql = await readFile(path.join(dir, "backend/internal/modules/iam", entry.file), "utf8");
  assert.equal(sql, expected);
  assert.match(
    sql,
    /CREATE OR REPLACE FUNCTION iam_assignment_template_establishment_kinds\(key text\) RETURNS text\[\][\s\S]*?WHEN 'workforce\.leave_reviewer\.v1' THEN ARRAY\['hospital'\]::text\[\][\s\S]*?WHEN 'workforce\.department_leave_reviewer\.v1' THEN ARRAY\['hospital'\]::text\[\][\s\S]*?ELSE ARRAY\[\]::text\[\] END/,
  );
  assert.deepEqual(
    await Promise.all(
      contract.template_migrations
        .slice(0, -1)
        .map((migration) =>
          readFile(path.join(dir, "backend/internal/modules/iam", migration.file), "utf8"),
        ),
    ),
    historical,
  );
  assert.match(
    await readFile(path.join(dir, artifacts[5]), "utf8"),
    /Key: "workforce\.leave_reviewer\.v2",[^\n]*?EstablishmentKinds: \[\]string\{"hospital", "pharmacy"\}\}/,
  );
  assert.match(
    await readFile(path.join(dir, artifacts[6]), "utf8"),
    /establishment_kinds: \["hospital", "pharmacy"\]/,
  );
  const check = generate(dir, "--check");
  assert.equal(check.status, 0, check.stderr);
});

test("published templates declare their establishment kinds while historical SQL keeps its bytes", async () => {
  const ts = await readFile(path.join(root, "packages/api/src/staff-access-templates.ts"), "utf8");
  const published = catalog.template_migrations.flatMap((migration) => migration.templates);
  assert.equal((ts.match(/establishment_kinds: \["hospital"\]/g) ?? []).length, published.length);
  assert.ok(
    catalog.models.TemplateDefinition.some((field) => field.name === "establishment_kinds"),
  );
  for (const migration of catalog.template_migrations.filter(
    (entry) => Number(entry.id.slice(0, 4)) < 34,
  ))
    assert.doesNotMatch(
      await readFile(path.join(root, "backend/internal/modules/iam", migration.file), "utf8"),
      /establishment/,
    );
});

test("purchase order templates remain department-only and cannot acquire clinical or financial authority", async (t) => {
  for (const [name, change] of [
    ["stock write", (entry) => entry.permissions.push("pharmacy.stock.receive")],
    ["financial write", (entry) => entry.permissions.push("pharmacy.accounts.payment")],
    ["hospital-wide expansion", (entry) => (entry.scope_kinds = ["hospital", "department"])],
    [
      "independent pharmacy root",
      (entry) => (entry.establishment_kinds = ["hospital", "pharmacy"]),
    ],
    [
      "without actual Staff",
      (entry) =>
        (entry.permissions = entry.permissions.filter(
          (value) => value !== "workforce.purchase_order_actor.use",
        )),
    ],
  ])
    await t.test(name, async (t) => {
      const { dir, contract, contractPath } = await fixture(t);
      const template = contract.template_migrations
        .flatMap((entry) => entry.templates)
        .find((entry) => entry.key === "supply.department_purchase_order_preparer.v1");
      assert.ok(template);
      change(template);
      await writeFile(contractPath, JSON.stringify(contract));
      const result = generate(dir);
      assert.notEqual(result.status, 0);
      assert.match(result.stderr, /Purchase order|department|pharmacy root/i);
    });
});

test("hospital pharmacy commercial reviewers cannot commit physical stock", () => {
  const definitions = catalog.template_migrations.flatMap((entry) => entry.templates);
  const acceptor = definitions.find(
    (entry) => entry.key === "supply.department_goods_pharmacy_acceptor.v1",
  );
  assert.ok(acceptor, "hospital suppliers need a department-scoped commercial acceptance role");
  assert.deepEqual(acceptor.permissions, [
    "supply.receiving_response.record",
    "supply.supplier_order.accept",
    "supply.supplier_order.read",
    "workforce.purchase_order_actor.use",
  ]);
  assert.deepEqual(acceptor.scope_kinds, ["department"]);
  assert.deepEqual(acceptor.establishment_kinds, ["hospital"]);
  const fulfiller = definitions.find(
    (entry) => entry.key === "supply.department_goods_pharmacy_fulfiller.v1",
  );
  assert.ok(!fulfiller.permissions.includes("supply.supplier_order.accept"));
  assert.ok(!acceptor.permissions.includes("pharmacy.hospital_fulfilment.use"));
});
