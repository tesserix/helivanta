import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const require = createRequire(path.join(root, "e2e/package.json"));
const cli = path.join(path.dirname(require.resolve("@playwright/test/package.json")), "cli.js");

test("main browser selection retains every spec once and schedules native recovery dependencies", () => {
  const result = spawnSync(
    process.execPath,
    [cli, "test", "--list", "--project=specs", "--project=bulk", "--reporter=json"],
    {
      cwd: path.join(root, "e2e"),
      encoding: "utf8",
      timeout: 30_000,
      maxBuffer: 4 * 1024 * 1024,
    },
  );
  assert.equal(result.status, 0, result.stderr);
  const report = JSON.parse(result.stdout);
  const entries = [];
  function collect(suites) {
    for (const suite of suites) {
      for (const spec of suite.specs ?? []) {
        for (const target of spec.tests)
          entries.push({
            file: path.basename(spec.file),
            line: spec.line,
            column: spec.column,
            title: spec.title,
            project: target.projectName,
          });
      }
      collect(suite.suites ?? []);
    }
  }
  collect(report.suites);
  const expected = readdirSync(path.join(root, "e2e/tests"))
    .filter(
      (file) =>
        file.endsWith(".spec.ts") &&
        !["idle-timeout.spec.ts", "session-renewal.spec.ts"].includes(file),
    )
    .sort();
  assert.deepEqual([...new Set(entries.map((entry) => entry.file))].sort(), expected);
  const unique = new Set(
    entries.map(({ file, line, column, title }) => JSON.stringify([file, line, column, title])),
  );
  assert.equal(unique.size, entries.length, "a browser case is registered more than once");
  for (const file of [
    "account-lockout-native.spec.ts",
    "staff-mfa-recovery-native.spec.ts",
    "staff-mfa-pharmacy-native.spec.ts",
  ]) {
    const cases = entries.filter((entry) => entry.file === file);
    assert.equal(cases.length, 1, file);
    assert.equal(cases[0].project, "native-recovery", file);
  }
});
