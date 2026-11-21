import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = fileURLToPath(new URL("../", import.meta.url));
test("every exact supplier acceptance test exists and changes to it trigger the workflow", async () => {
  const workflow = await readFile(
    path.join(root, ".github/workflows/supplier-workflow.yml"),
    "utf8",
  );
  const commands = [
    ...workflow.matchAll(/go test[^\n]*?\s(\.\/[^\s]+)\s-run '\^(Test[A-Za-z0-9_]+)\$'/g),
  ];
  assert.ok(commands.length > 0, "supplier workflow must contain exact acceptance selectors");
  const pathBlock = workflow.split("permissions:")[0].split("paths:")[1];
  assert.ok(pathBlock, "supplier workflow must declare its source triggers");
  const triggers = [...pathBlock.matchAll(/- "([^"]+)"/g)].map((match) => match[1]);
  const missingTriggers = new Set();
  for (const [, pkg, name] of commands) {
    const directory = path.join(root, "backend", pkg);
    const files = (await readdir(directory)).filter((file) => file.endsWith("_test.go"));
    const sources = await Promise.all(
      files.map((file) => readFile(path.join(directory, file), "utf8")),
    );
    assert.ok(
      sources.some((source) => new RegExp(`^func ${name}\\(t \\*testing\\.T\\)`, "m").test(source)),
      `${pkg}: ${name} must exist; a missing selector can pass without running acceptance`,
    );
    for (let index = 0; index < sources.length; index++) {
      if (!new RegExp(`^func ${name}\\(t \\*testing\\.T\\)`, "m").test(sources[index])) continue;
      const file = path.posix.join("backend", pkg, files[index]);
      const covered = triggers.some((trigger) =>
        trigger.endsWith("/**")
          ? file.startsWith(trigger.slice(0, -2))
          : trigger.endsWith("*")
            ? file.startsWith(trigger.slice(0, -1))
            : file === trigger,
      );
      if (!covered) missingTriggers.add(file);
    }
  }
  assert.deepEqual(
    [...missingTriggers].sort(),
    [],
    "selected acceptance source files must trigger their own workflow",
  );
});
