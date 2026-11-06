import assert from "node:assert/strict";
import { copyFileSync, mkdirSync, mkdtempSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, test } from "node:test";
import { pathToFileURL } from "node:url";

const root = realpathSync(mkdtempSync(join(tmpdir(), "helivanta-zitadel-files-")));
const library = join(root, "scripts", "lib");
mkdirSync(library, { recursive: true });
const modulePath = join(library, "zitadel.mjs");
copyFileSync(new URL("./zitadel.mjs", import.meta.url), modulePath);
const readers = await import(pathToFileURL(modulePath).href);
after(() => rmSync(root, { recursive: true, force: true }));

for (const [reader, filename] of [
  ["readClientID", "zitadel.env"],
  ["readMachinePAT", "helivanta-seed.pat"],
  ["readLoginClientPAT", "login-client.pat"],
]) {
  test(`${reader} preserves the missing-file cause and setup instructions`, () => {
    const override = process.env.ZITADEL_CLIENT_ID;
    delete process.env.ZITADEL_CLIENT_ID;
    try {
      assert.throws(readers[reader], (error) => {
        assert.match(error.message, /does not exist/);
        assert.equal(error.cause?.code, "ENOENT");
        assert.equal(error.cause.path, join(root, "dev", "zitadel", "secrets", filename));
        return true;
      });
    } finally {
      if (override === undefined) delete process.env.ZITADEL_CLIENT_ID;
      else process.env.ZITADEL_CLIENT_ID = override;
    }
  });
}
