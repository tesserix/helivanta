import { ESLint } from "eslint";
import { describe, expect, it } from "vitest";
import { hmsEslint } from "./eslint.config.mjs";

// The design rules (#1255, spec D2/D6): colours come from tokens and every
// dropdown is the design-system Select. Asserted on the messages ESLint
// actually reports for real snippets, so a selector that silently matches
// nothing fails here.
function linter(options) {
  return new ESLint({
    cwd: import.meta.dirname,
    overrideConfigFile: true,
    overrideConfig: hmsEslint(import.meta.dirname, options),
  });
}

async function messages(code, options = { design: true }) {
  const [result] = await linter(options).lintText(code, { filePath: "fixture.tsx" });
  return result.messages.map((message) => message.message);
}

describe("design rules", () => {
  it("rejects a raw Tailwind colour", async () => {
    expect(await messages(`export const A = () => <div className="p-2 bg-teal-600" />;`)).toEqual([
      expect.stringMatching(/design-token utility/),
    ]);
  });

  it("rejects a raw colour in a template class string", async () => {
    const code =
      "export const A = ({ on }: { on: boolean }) => <div className={`p-2 ${on ? 'x' : ''} text-white`} />;";
    expect(await messages(code)).toEqual([expect.stringMatching(/design-token utility/)]);
  });

  it("rejects a hex arbitrary value", async () => {
    expect(await messages(`export const A = () => <div className="bg-[#fff000]" />;`)).toEqual([
      expect.stringMatching(/design-token utility/),
    ]);
  });

  it("rejects a native select", async () => {
    const code = `export const A = () => <select aria-label="x"><option>a</option></select>;`;
    expect(await messages(code)).toEqual([
      expect.stringMatching(/SelectControl or FormSelectField/),
    ]);
  });

  it("accepts token utilities", async () => {
    const code = `export const A = () => <div className="bg-primary text-foreground border-border text-(--hms-warn)" />;`;
    expect(await messages(code)).toEqual([]);
  });

  it("is off unless asked for", async () => {
    expect(await messages(`export const A = () => <div className="bg-teal-600" />;`, {})).toEqual(
      [],
    );
  });
});
