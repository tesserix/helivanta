import { expect, it } from "vitest";
import { SupplierOrderProductContract } from "./supplier-orders-contract";
const product = "019a55c0-1111-7111-8111-111111111112";
const version = "11111111-1111-4111-8111-111111111111";
it("keeps the two stock owners' version schemes distinct", () => {
  expect(
    SupplierOrderProductContract.parse({
      owner: "supply",
      product_id: product,
      version_id: version,
    }),
  ).toEqual({ owner: "supply", product_id: product, version_id: version });
  expect(
    SupplierOrderProductContract.parse({ owner: "pharmacy", product_id: product, version: 3 }),
  ).toEqual({ owner: "pharmacy", product_id: product, version: 3 });
  for (const row of [
    { owner: "supply", product_id: product, version: 3 },
    { owner: "supply", product_id: product },
    { owner: "supply", product_id: product, version: 3, version_id: version },
    { owner: "pharmacy", product_id: version, version: 3 },
    { owner: "pharmacy", product_id: product, version_id: version },
  ])
    expect(SupplierOrderProductContract.safeParse(row).success).toBe(false);
});
