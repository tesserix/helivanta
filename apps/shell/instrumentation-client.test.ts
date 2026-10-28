import { expect, it, vi } from "vitest";
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
