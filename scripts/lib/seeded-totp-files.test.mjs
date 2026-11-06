import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { readSeededTOTPSecret } from "../../e2e/tests/support/totp.ts";

test("readSeededTOTPSecret preserves the missing-file cause and seed instructions", () => {
  const root = realpathSync(mkdtempSync(join(tmpdir(), "helivanta-seeded-totp-")));
  const cwd = process.cwd();
  mkdirSync(join(root, "e2e"));
  process.chdir(join(root, "e2e"));
  try {
    assert.throws(readSeededTOTPSecret, (error) => {
      assert.match(error.message, /run 'make seed' first/);
      assert.equal(error.cause?.code, "ENOENT");
      assert.equal(error.cause.path, join(root, "dev", "zitadel", "secrets", "mfa-totp.secret"));
      return true;
    });
  } finally {
    process.chdir(cwd);
    rmSync(root, { recursive: true, force: true });
  }
});
