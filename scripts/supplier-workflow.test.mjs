import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = fileURLToPath(new URL("../", import.meta.url));
test("every exact supplier workflow selector names a real test in its package", async () => {
  const workflow = await readFile(
    path.join(root, ".github/workflows/supplier-workflow.yml"),
    "utf8",
  );
  const commands = [
    ...workflow.matchAll(/go test[^\n]*?\s(\.\/[^\s]+)\s-run '\^(Test[A-Za-z0-9_]+)\$'/g),
  ];
  assert.ok(commands.length > 0, "supplier workflow must contain exact acceptance selectors");
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
  }
});
