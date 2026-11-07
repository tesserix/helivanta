import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { ReferenceContract } from "./stock-product-contract";

const wire: unknown = JSON.parse(
  readFileSync(
    resolve(process.cwd(), "../../backend/pkg/stockproduct/testdata/reference.json"),
    "utf8",
  ),
);

describe("stock product owner wire contract", () => {
  it("accepts the exact same reference that Go emits without dropping fields", () => {
    expect(ReferenceContract.parse(wire)).toEqual(wire);
  });

  it("rejects integer quantities that cannot survive the JSON boundary exactly", () => {
    const reference = ReferenceContract.parse(wire);
    expect(
      ReferenceContract.safeParse({ ...reference, pack_size: Number.MAX_SAFE_INTEGER + 1 }).success,
    ).toBe(false);
  });
});
