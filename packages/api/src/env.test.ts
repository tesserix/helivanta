import { describe, expect, it } from "vitest";
import { z } from "zod";
import { defineEnv } from "./env";

describe("defineEnv", () => {
  it("returns parsed values", () => {
    const env = defineEnv({ API_URL: z.string().url() }, { API_URL: "http://localhost:8080" });
    expect(env.API_URL).toBe("http://localhost:8080");
  });

  it("throws naming every bad key", () => {
    expect(() => defineEnv({ A: z.string().min(1), B: z.string().min(1) }, { A: "" })).toThrowError(
      /A[\s\S]*B/,
    );
  });
});
