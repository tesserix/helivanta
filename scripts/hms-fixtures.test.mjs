import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { test } from "node:test";

const cli = new URL("./hms-fixtures.mjs", import.meta.url);

test("CLI emits only synthetic JSON and refuses unmapped legacy inputs", () => {
  const generated = spawnSync(process.execPath, [cli.pathname], { encoding: "utf8" });
  assert.equal(generated.status, 0, generated.stderr);
  assert.equal(JSON.parse(generated.stdout).classification, "synthetic");
  const raw = "DO-NOT-ECHO-CUSTOMER-IDENTIFIER";
  const rejected = spawnSync(process.execPath, [cli.pathname, "--input", raw], {
    encoding: "utf8",
  });
  assert.equal(rejected.status, 1);
  assert.equal(rejected.stdout, "");
  assert.equal(rejected.stderr.includes(raw), false);
  assert.match(rejected.stderr, /does not accept legacy records/);
});
