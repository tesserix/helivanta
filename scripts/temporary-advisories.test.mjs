import assert from "node:assert/strict";
import test from "node:test";
import { verifyTemporaryAdvisories } from "./temporary-advisories.mjs";

const advisory = "GHSA-fc5q-39xc-vjpf";
test("accepts the approved advisory during its limited window", () => {
  verifyTemporaryAdvisories([advisory], new Date("2026-11-25T23:59:59.999Z"));
});
test("refuses the exception at its expiry boundary", () => {
  assert.throws(
    () => verifyTemporaryAdvisories([advisory], new Date("2026-11-26T00:00:00Z")),
    /expired/,
  );
});
test("allows the advisory to be removed after expiry", () => {
  verifyTemporaryAdvisories([], new Date("2026-11-26T00:00:00Z"));
});
