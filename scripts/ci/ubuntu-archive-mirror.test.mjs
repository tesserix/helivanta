import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { configure } from "./ubuntu-archive-mirror.mjs";

test("selects official Ubuntu archive only for known Azure mirror files", (t) => {
  const root = mkdtempSync(path.join(tmpdir(), "helivanta-apt-fixture-"));
  t.after(() => rmSync(root, { recursive: true }));
  const relative = ["apt-mirrors.txt", "apt-mirrors-security.txt", "sources.list.d/ubuntu.sources"];
  for (const name of relative) {
    const file = path.join(root, name);
    mkdirSync(path.dirname(file), { recursive: true });
    writeFileSync(
      file,
      "http://azure.archive.ubuntu.com/ubuntu/\nhttps://azure.archive.ubuntu.com/ubuntu/\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\nhttps://packages.example.invalid/ubuntu/\n",
    );
  }
  const untouched = path.join(root, "sources.list.d/custom.sources");
  writeFileSync(untouched, "http://azure.archive.ubuntu.com/ubuntu/");
  configure(root);
  for (const name of relative) {
    const text = readFileSync(path.join(root, name), "utf8");
    assert.equal(text.match(/https:\/\/archive\.ubuntu\.com\/ubuntu\//g).length, 2);
    assert.ok(text.includes("Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg"));
    assert.ok(text.includes("https://packages.example.invalid/ubuntu/"));
  }
  assert.equal(readFileSync(untouched, "utf8"), "http://azure.archive.ubuntu.com/ubuntu/");
  configure(root);
  assert.equal(
    readFileSync(path.join(root, relative[0]), "utf8").match(
      /https:\/\/archive\.ubuntu\.com\/ubuntu\//g,
    ).length,
    2,
  );
});

test("missing optional mirror files leave the runner unchanged", (t) => {
  const root = mkdtempSync(path.join(tmpdir(), "helivanta-apt-fixture-"));
  t.after(() => rmSync(root, { recursive: true }));
  configure(root);
  assert.deepEqual(readdirSync(root), []);
});
