import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

const fixture = fileURLToPath(new URL("./e2e-evidence-provider.mjs", import.meta.url));
for (const environment of [undefined, "production", "staging"]) {
  test(`evidence fixture rejects ${environment ?? "unset"} mode before starting dependencies`, () => {
    const result = spawnSync(process.execPath, [fixture], {
      env: { PATH: "", HELIVANTA_E2E: "1", ...(environment ? { HELIVANTA_ENV: environment } : {}) },
      encoding: "utf8",
    });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /requires explicit dev E2E mode/);
  });
}
test("evidence fixture requires the explicit E2E marker", () => {
  const result = spawnSync(process.execPath, [fixture], {
    env: { PATH: "", HELIVANTA_ENV: "dev" },
    encoding: "utf8",
  });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires explicit dev E2E mode/);
});
test("evidence fixture refuses invalid ports and remote databases", () => {
  for (const extra of [
    { HELIVANTA_E2E_EVIDENCE_PORT: "65535" },
    { ADMIN_DATABASE_URL: "postgres://TEST:TEST@production.invalid/helivanta" },
  ]) {
    const result = spawnSync(process.execPath, [fixture], {
      env: { PATH: "", HELIVANTA_ENV: "dev", HELIVANTA_E2E: "1", ...extra },
      encoding: "utf8",
    });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /Invalid fixture port|explicitly selected loopback dev database/);
  }
});
