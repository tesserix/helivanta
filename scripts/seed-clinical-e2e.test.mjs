import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

const script = fileURLToPath(new URL("./seed-clinical-e2e.mjs", import.meta.url));
for (const mode of ["", "production", "staging"]) {
  test(`clinical fixture refuses HELIVANTA_ENV=${mode || "unset"} before invoking an owner command`, () => {
    const result = spawnSync(process.execPath, [script], {
      env: { ...process.env, PATH: "", HELIVANTA_ENV: mode },
      encoding: "utf8",
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /require HELIVANTA_ENV=dev/);
    assert.doesNotMatch(result.stderr, /ENOENT/);
  });
}
for (const database of [
  "postgres://fixture@db.example.invalid/helivanta",
  "postgres://fixture@localhost/not-the-development-database",
  "http://localhost/helivanta",
]) {
  test(`clinical fixture refuses an unintended database: ${new URL(database).hostname}${new URL(database).pathname}`, () => {
    const result = spawnSync(process.execPath, [script], {
      env: { ...process.env, PATH: "", HELIVANTA_ENV: "dev", ADMIN_DATABASE_URL: database },
      encoding: "utf8",
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /require the local helivanta development database/);
    assert.doesNotMatch(result.stderr, /ENOENT/);
  });
}

for (const override of [
  "host=db.example.invalid",
  "user=some_other_role",
  "dbname=some_other_database",
]) {
  test(`clinical fixture refuses a database query override: ${override.split("=")[0]}`, () => {
    const result = spawnSync(process.execPath, [script], {
      env: {
        ...process.env,
        PATH: "",
        HELIVANTA_ENV: "dev",
        ADMIN_DATABASE_URL: `postgres://fixture@localhost/helivanta?${override}`,
      },
      encoding: "utf8",
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /refuse database routing or identity overrides/);
    assert.doesNotMatch(result.stderr, /ENOENT/);
  });
}
