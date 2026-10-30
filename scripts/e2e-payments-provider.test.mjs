import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

const fixture = fileURLToPath(new URL("./e2e-payments-provider.mjs", import.meta.url));
for (const environment of [undefined, "production", "staging"]) {
  test(`payment fixture refuses ${environment ?? "unset"} environment before starting services`, () => {
    const result = spawnSync(process.execPath, [fixture], {
      env: { PATH: "", ...(environment ? { HELIVANTA_ENV: environment } : {}) },
      encoding: "utf8",
    });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /requires HELIVANTA_ENV=dev/);
  });
}
test("payment fixture rejects invalid listener ports before starting services", () => {
  const result = spawnSync(process.execPath, [fixture], {
    env: { PATH: "", HELIVANTA_ENV: "dev", HELIVANTA_E2E_PAYMENT_PORT: "0" },
    encoding: "utf8",
  });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Invalid local fixture port/);
});
