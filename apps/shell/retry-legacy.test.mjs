import { ESLint } from "eslint";
import { describe, expect, it } from "vitest";
import { hmsEslint, hmsReadRetries, READ_RETRY_RULE } from "@helivanta/config/eslint";
import { RETRY_LEGACY } from "./retry-legacy.mjs";

// #1595: the legacy list only shrinks. A converted file must leave it (and is
// then held to the rule), and only workforce, whose slices own the
// conversion, may be listed.
const eslint = new ESLint({
  cwd: import.meta.dirname,
  overrideConfigFile: true,
  overrideConfig: [...hmsEslint(import.meta.dirname), hmsReadRetries(["**/*.tsx"])],
});

describe("read-retry legacy list", () => {
  it.each(RETRY_LEGACY)("%s still draws its own read retry", async (file) => {
    expect(file).toMatch(/^app\/\(workspace\)\/workforce\//);
    const [result] = await eslint.lintFiles([file]);
    expect(result.messages.some((m) => m.ruleId === READ_RETRY_RULE)).toBe(true);
  });

  it("is never added to", () => {
    // Lower this as workforce slices remove files; raising it needs a reason in review.
    expect(RETRY_LEGACY.length).toBeLessThanOrEqual(35);
    expect(new Set(RETRY_LEGACY).size).toBe(RETRY_LEGACY.length);
  });
});
