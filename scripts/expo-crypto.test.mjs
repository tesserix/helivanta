import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { readFile } from "node:fs/promises";
import test from "node:test";

const appRequire = createRequire(new URL("../apps/ambulanceconnect/package.json", import.meta.url));
const expoRequire = createRequire(appRequire.resolve("expo/package.json"));
const cliRequire = createRequire(expoRequire.resolve("@expo/cli/package.json"));
const forge = cliRequire("node-forge");
const fixture = JSON.parse(
  await readFile(new URL("./fixtures/forge-digest-algorithm.json", import.meta.url), "utf8"),
);

// Upstream public vectors: digitalbazaar/forge PR #1152 and v1.4.0 tests/unit/rsa.js.
test("Expo's actual node-forge rejects nested DigestAlgorithm garbage", () => {
  const publicKey = forge.pki.rsa.setPublicKey(
    new forge.jsbn.BigInteger(fixture.modulusHex, 16),
    new forge.jsbn.BigInteger("3"),
  );
  const md = forge.md.sha256.create().update("hello world!");
  assert.throws(
    () =>
      publicKey.verify(
        md.digest().getBytes(),
        forge.util.hexToBytes(fixture.nestedGarbageSignatureHex),
      ),
    /ASN.1 object does not contain a valid/,
  );
});

test("Expo's node-forge still verifies a valid SHA-256 RSA signature", () => {
  const publicKey = forge.pki.publicKeyFromPem(fixture.publicKeyPem);
  const md = forge.md.sha256.create().update(forge.util.hexToBytes(fixture.controlMsg));
  assert.equal(
    publicKey.verify(md.digest().getBytes(), forge.util.hexToBytes(fixture.controlSig)),
    true,
  );
});

test("a changed message does not verify against the valid signature", () => {
  const publicKey = forge.pki.publicKeyFromPem(fixture.publicKeyPem);
  const md = forge.md.sha256.create().update("changed message");
  assert.equal(
    publicKey.verify(md.digest().getBytes(), forge.util.hexToBytes(fixture.controlSig)),
    false,
  );
});
