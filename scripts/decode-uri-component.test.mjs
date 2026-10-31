import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";

// SDK 58 routes decoding through its URL helpers, without query-string's decoder.
const appRequire = createRequire(new URL("../apps/ambulanceconnect/package.json", import.meta.url));
const routerRequire = createRequire(appRequire.resolve("expo-router/package.json"));
const { safeDecodeURIComponent: decode } = routerRequire("./build/utils/url.js");

test("the router no longer loads the vulnerable query-string decoder", () => {
  assert.throws(() => routerRequire.resolve("query-string"), { code: "MODULE_NOT_FOUND" });
});

test("a long malformed percent-encoded run decodes in linear time", () => {
  // Quadratic before the backport: tens of seconds; linear after: milliseconds.
  const input = "%C3".repeat(20_000);
  const started = performance.now();
  assert.equal(decode(input), input);
  assert.ok(performance.now() - started < 1_000, "decoding took too long");
});

test("the router decoder preserves Unicode and leaves malformed components intact", () => {
  assert.equal(decode("a%20b+c"), "a b+c");
  assert.equal(decode("%E5%BA%97"), "店");
  for (const input of ["%C2", "%FE%FF", "test%ab%C3%A5", "%80%C3%A5%"])
    assert.equal(decode(input), input);
  assert.equal(decode(1), 1);
});
