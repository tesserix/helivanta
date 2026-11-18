import { expect, it, vi } from "vitest";
const construction = vi.hoisted(() => ({ first: undefined as boolean | undefined }));
vi.mock("zod", async (importOriginal) => {
  const actual = await importOriginal<typeof import("zod")>();
  actual.z.config({ jitless: false });
  return {
    ...actual,
    z: {
      ...actual.z,
      object: (...args: Parameters<typeof actual.z.object>) => {
        construction.first ??= actual.z.config().jitless;
        return actual.z.object(...args);
      },
    },
  };
});
import "./instrumentation-client";
import { z } from "zod";

it("validates browser objects without attempting dynamic code evaluation", () => {
  const evaluate = vi.spyOn(globalThis, "Function");
  try {
    const schema = z.object({ quantity: z.number().int().min(1) });
    expect(schema.safeParse({ quantity: 1 }).success).toBe(true);
    expect(schema.safeParse({ quantity: 0 }).success).toBe(false);
    expect(evaluate).not.toHaveBeenCalled();
  } finally {
    evaluate.mockRestore();
  }
});

it("configures CSP-safe parsing before constructing startup reporter schemas", () => {
  expect(construction.first).toBe(true);
});
