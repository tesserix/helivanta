import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";

// The decoder Expo Router actually loads: apps/ambulanceconnect > expo-router >
// query-string > decode-uri-component 0.2.2, patched for GHSA-qr28-fpjm-wq7f.
const appRequire = createRequire(new URL("../apps/ambulanceconnect/package.json", import.meta.url));
const routerRequire = createRequire(appRequire.resolve("expo-router/package.json"));
const queryRequire = createRequire(routerRequire.resolve("query-string/package.json"));
const decode = queryRequire("decode-uri-component");

test("a long malformed percent-encoded run decodes in linear time", () => {
  // Quadratic before the backport: tens of seconds; linear after: milliseconds.
  const input = "%C3".repeat(20_000);
  const started = performance.now();
  assert.equal(decode(input), input);
  assert.ok(performance.now() - started < 1_000, "decoding took too long");
});

test("decoding is unchanged for the inputs query-string relies on", () => {
  assert.equal(decode("a%20b+c"), "a b c", "+ is still a space");
  assert.equal(decode("%E5%BA%97"), "店");
  assert.equal(decode("%C2"), "�");
  assert.equal(decode("%FE%FF"), "��");
  assert.equal(
    decode("test%ab%C3%A5"),
    "test%abå",
    "a bad byte stays literal, the valid run decodes",
  );
  assert.equal(decode("%80%C3%A5%"), "%80å%");
  assert.throws(() => decode(1), TypeError);
});
