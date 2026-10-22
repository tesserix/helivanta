import { ESLint } from "eslint";
import { describe, expect, it } from "vitest";
import { hmsEslint, hmsPublicPages, hmsWorkspacePages } from "./eslint.config.mjs";

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

  // Enterprise restraint (#1255 spec D3): the landing page's marketing type
  // stays on the public pages.
  it.each([
    ["serif", "font-serif"],
    ["italic", "italic"],
    ["gradient", "bg-linear-to-r from-primary"],
    ["legacy gradient", "bg-gradient-to-r"],
    ["tracked uppercase eyebrow", "text-xs uppercase tracking-[0.18em]"],
    ["wide-tracked eyebrow", "text-xs font-semibold tracking-[0.18em]"],
    ["widest tracking", "tracking-widest"],
  ])("rejects marketing type: %s", async (_name, classes) => {
    expect(await messages(`export const A = () => <p className="${classes}" />;`)).toEqual([
      expect.stringMatching(/Marketing type/),
    ]);
  });

  it("accepts tight or normal tracking", async () => {
    expect(
      await messages(
        `export const A = () => <p className="tracking-tight tracking-wide tracking-[-0.02em]" />;`,
      ),
    ).toEqual([]);
  });

  it("accepts uppercase without tracking (table headers)", async () => {
    expect(await messages(`export const A = () => <th className="text-xs uppercase" />;`)).toEqual(
      [],
    );
  });

  it("lets a public page keep marketing type but no raw colour", async () => {
    const eslint = new ESLint({
      cwd: import.meta.dirname,
      overrideConfigFile: true,
      overrideConfig: [
        ...hmsEslint(import.meta.dirname, { design: true }),
        hmsPublicPages(["page.tsx"]),
      ],
    });
    const lint = async (code) =>
      (await eslint.lintText(code, { filePath: "page.tsx" }))[0].messages.map((m) => m.message);
    expect(await lint(`export const A = () => <em className="font-serif italic" />;`)).toEqual([]);
    expect(await lint(`export const A = () => <em className="bg-teal-600" />;`)).toEqual([
      expect.stringMatching(/design-token utility/),
    ]);
    expect(
      await lint(`export const A = () => <div dangerouslySetInnerHTML={{ __html: x }} />;`),
    ).toEqual([expect.stringMatching(/sanitizeHtml/)]);
  });

  // #1255 final review: one-character variants must not walk through.
  it.each([
    "md:italic",
    "hover:font-serif",
    "sm:tracking-widest",
    "text-xs uppercase md:tracking-wider",
    "tracking-[0.08em]",
    "tracking-[2px]",
    "font-[ui-serif]",
  ])("rejects variant marketing type: %s", async (classes) => {
    expect(await messages(`export const A = () => <p className="${classes}" />;`)).toEqual([
      expect.stringMatching(/Marketing type/),
    ]);
  });

  it.each([
    "bg-[rgb(0,0,0)]",
    "text-[oklch(0.5_0.1_200)]",
    "[color:#fff]",
    "outline-white",
    "ring-offset-white",
    "shadow-black",
  ])("rejects variant raw colour: %s", async (classes) => {
    expect(await messages(`export const A = () => <p className="${classes}" />;`)).toEqual([
      expect.stringMatching(/design-token utility/),
    ]);
  });

  it("rejects a hex colour in a style or SVG attribute", async () => {
    expect(await messages(`export const A = () => <svg><path fill="#ff0000" /></svg>;`)).toEqual([
      expect.stringMatching(/design-token utility/),
    ]);
  });

  it("leaves fragment links and hover tokens alone", async () => {
    const code = `export const A = () => <a href="#emergency-help" className="hover:bg-accent md:tracking-tight">x</a>;`;
    expect(await messages(code)).toEqual([]);
  });

  // Tests assert on colour values themselves (the contrast test), so the UI
  // design rules apply to shipped code only; sanitize still applies everywhere.
  it("exempts test files from the design rules but not from sanitize", async () => {
    const lint = async (code) =>
      (await linter({ design: true }).lintText(code, { filePath: "x.test.tsx" }))[0].messages.map(
        (m) => m.message,
      );
    expect(await lint(`export const pair = ["#000000", "#ffffff"];`)).toEqual([]);
    expect(
      await lint(`export const A = () => <div dangerouslySetInnerHTML={{ __html: x }} />;`),
    ).toEqual([expect.stringMatching(/sanitizeHtml/)]);
  });
});

// One page heading (#1274): workspace pages open with PageHeading under the
// shell's own h1, so a second h1, a display-size heading or an accent eyebrow
// is an error there, and nowhere else.
describe("workspace heading rules", () => {
  const eslint = new ESLint({
    cwd: import.meta.dirname,
    overrideConfigFile: true,
    overrideConfig: [
      ...hmsEslint(import.meta.dirname, { design: true }),
      hmsWorkspacePages(["workspace/**"]),
    ],
  });
  const lint = async (code, filePath = "workspace/page.tsx") =>
    (await eslint.lintText(code, { filePath }))[0].messages.map((m) => m.message);

  it("rejects an h1 in a workspace file", async () => {
    expect(await lint(`export const A = () => <h1 className="text-xl">x</h1>;`)).toEqual([
      expect.stringMatching(/shell header owns the h1/),
    ]);
  });

  it.each(["text-2xl", "text-3xl font-semibold tracking-tight", "mt-2 md:text-4xl"])(
    "rejects a display-size heading: %s",
    async (classes) => {
      expect(await lint(`export const A = () => <h2 className="${classes}">x</h2>;`)).toEqual([
        expect.stringMatching(/PageHeading/),
      ]);
    },
  );

  it("rejects a display-size heading in a template class", async () => {
    const code =
      "export const A = ({ c }: { c: string }) => <h3 className={`${c} text-3xl`}>x</h3>;";
    expect(await lint(code)).toEqual([expect.stringMatching(/PageHeading/)]);
  });

  it.each([
    "text-xs font-semibold text-(--hms-accent)",
    "mb-2 flex items-center gap-2 text-xs font-semibold text-(--hms-accent)",
    "text-(--hms-accent) font-semibold text-xs",
  ])("rejects an accent eyebrow: %s", async (classes) => {
    expect(await lint(`export const A = () => <p className="${classes}">x</p>;`)).toEqual([
      expect.stringMatching(/eyebrow/),
    ]);
  });

  it("keeps the shared design rules", async () => {
    expect(await lint(`export const A = () => <p className="bg-teal-600" />;`)).toEqual([
      expect.stringMatching(/design-token utility/),
    ]);
  });

  it.each([
    ["a KPI number", `<p className="text-3xl font-semibold tabular-nums">12</p>`],
    ["a section heading", `<h2 className="text-xl font-semibold">x</h2>`],
    [
      "an accent pill",
      `<span className="rounded-full text-xs font-medium text-(--hms-accent)">x</span>`,
    ],
    ["an accent link", `<a href="/x" className="text-sm font-medium text-(--hms-accent)">x</a>`],
  ])("accepts %s", async (_name, jsx) => {
    expect(await lint(`export const A = () => ${jsx};`)).toEqual([]);
  });

  it("leaves files outside the workspace alone", async () => {
    expect(
      await lint(`export const A = () => <h1 className="text-3xl">x</h1>;`, "login/page.tsx"),
    ).toEqual([]);
  });

  it("leaves workspace tests alone", async () => {
    expect(
      await lint(`export const A = () => <h1 className="text-3xl">x</h1>;`, "workspace/a.test.tsx"),
    ).toEqual([]);
  });
});
