// @vitest-environment node
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// #858: Chrome and Safari paint autofilled fields with their own colours
// (rgb(232,240,254) / black) over the dark login card. styles.css overrides
// that state. These assertions pin the two properties that make the override
// correct rather than merely present. The real-browser behaviour, Chrome's
// genuine autofill state via CDP Autofill.trigger, is recorded in the #858
// spec.
const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");

/** The declaration block of the rule whose selector list contains `selector`. */
function ruleFor(selector: string): string {
  const at = css.indexOf(selector);
  expect(at, `no rule for ${selector} in styles.css`).toBeGreaterThan(-1);
  const open = css.indexOf("{", at);
  return css.slice(open + 1, css.indexOf("}", open));
}

const rules = {
  "-webkit-autofill (Chrome, Safari)": ruleFor("input:-webkit-autofill,"),
  ":autofill (Firefox, standard)": ruleFor("input:autofill,"),
};

describe("autofill styling (#858)", () => {
  for (const [name, body] of Object.entries(rules)) {
    describe(name, () => {
      it("takes every colour from a theme token, so a token change cannot leave autofill behind", () => {
        expect(body).toMatch(/-webkit-text-fill-color:\s*var\(--foreground\)/);
        expect(body).toMatch(/caret-color:\s*var\(--foreground\)/);
        expect(body).toMatch(/inset 0 0 0 1000px var\(--background\)/);
        // No literal colour anywhere in the rule: a hardcoded value is the
        // exact way this bug comes back the next time the tokens move.
        // Tailwind's own transparent fallback inside var(--tw-…, 0 0 #0000)
        // is not a colour this rule chooses, so it is set aside first.
        const chosen = body.replace(/var\(--tw-[a-z-]+, 0 0 #0000\)/g, "");
        expect(chosen).not.toMatch(/#[0-9a-fA-F]{3,8}\b|rgba?\(|hsla?\(/);
      });

      it("keeps Tailwind's ring and shadow, so autofilled fields still show a focus ring", () => {
        for (const v of ["--tw-ring-offset-shadow", "--tw-ring-shadow", "--tw-shadow"]) {
          expect(body).toContain(`var(${v}`);
        }
      });
    });
  }

  it("keeps the two pseudo-classes in separate rules, so one unknown to a browser cannot void the other", () => {
    expect(rules["-webkit-autofill (Chrome, Safari)"]).not.toBe(
      rules[":autofill (Firefox, standard)"],
    );
    const webkitSelector = css.slice(
      css.indexOf("input:-webkit-autofill,"),
      css.indexOf("{", css.indexOf("input:-webkit-autofill,")),
    );
    expect(webkitSelector).not.toMatch(/:autofill\b/);
  });

  it("removes Firefox's autofill tint", () => {
    expect(rules[":autofill (Firefox, standard)"]).toMatch(/filter:\s*none/);
  });
});
