import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";

const appRequire = createRequire(new URL("../apps/ambulanceconnect/package.json", import.meta.url));
const expoRequire = createRequire(appRequire.resolve("expo/package.json"));
const cliRequire = createRequire(expoRequire.resolve("@expo/cli/package.json"));
const { z } = cliRequire("zod");
const { PluginSchema, DevToolsPluginOutputSchema } = cliRequire(
  "./build/src/start/server/DevToolsPlugin.schema.js",
);
const { createMCPDevToolsExtensionSchema } = cliRequire(
  "./build/src/start/server/createMCPDevToolsExtensionSchema.js",
);

test("Expo's resolved validator stops at a collection limit", () => {
  const result = z.array(z.string()).max(2).safeParse(Array(1000).fill(null));
  assert.equal(result.success, false);
  assert.equal(result.error.issues.length, 1);
  assert.equal(result.error.issues[0].code, "too_big");
});

test("Expo plugin validation preserves command types and rejects unknown input", () => {
  const plugin = PluginSchema.parse({
    packageName: "synthetic-plugin",
    packageRoot: "/synthetic",
    cliExtensions: {
      description: "Synthetic command",
      entryPoint: "index.js",
      commands: [
        {
          name: "inspect",
          title: "Inspect",
          environments: ["mcp"],
          parameters: [{ name: "count", type: "number" }],
        },
      ],
    },
  });
  const schema = createMCPDevToolsExtensionSchema(plugin);
  assert.deepEqual(schema.parse({ command: "inspect", count: 2 }), {
    command: "inspect",
    count: 2,
  });
  for (const input of [
    { command: "missing" },
    { command: "inspect", count: "2" },
    { command: "inspect", unknown: true },
  ]) {
    assert.equal(schema.safeParse(input).success, false);
  }
  assert.equal(PluginSchema.safeParse({ packageName: "" }).success, false);
  assert.deepEqual(
    DevToolsPluginOutputSchema.parse([{ type: "text", text: "ready", level: "info" }]),
    [{ type: "text", text: "ready", level: "info" }],
  );
  assert.equal(
    DevToolsPluginOutputSchema.safeParse([{ type: "audio", url: "invalid" }]).success,
    false,
  );
});
