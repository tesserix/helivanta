import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readdir, symlink, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { retainSupplierReturnEvidence } from "./retain-supplier-return-evidence.mjs";

test("retains only named synthetic supplier screenshots before later phases overwrite reports", async () => {
  const temp = await mkdtemp(path.join(tmpdir(), "helivanta-supplier-evidence-"));
  try {
    const source = path.join(temp, "results"),
      target = path.join(temp, "evidence");
    const fixtures = path.join(source, "pharmacy-supplier-return-dispatch-specs", "attachments");
    const unrelated = path.join(source, "other-case-specs", "attachments");
    await mkdir(fixtures, { recursive: true });
    await mkdir(unrelated, { recursive: true });
    for (const name of [
      "supplier-credit-recovered-1.png",
      "supplier-credit-match-reversed-1.png",
      "supplier-credit-matched-1.png",
      "commercial-return-note-print-1.png",
      "PRIVATE.png",
      "trace.zip",
    ])
      await writeFile(path.join(fixtures, name), "TEST synthetic data");
    await writeFile(
      path.join(unrelated, "supplier-credit-recovered-private.png"),
      "PRIVATE unrelated data",
    );
    await symlink(
      path.join(fixtures, "PRIVATE.png"),
      path.join(fixtures, "supplier-credit-recovered-link.png"),
    );
    await retainSupplierReturnEvidence(source, target);
    assert.deepEqual((await readdir(target)).sort(), [
      "commercial-return-note-print-1.png",
      "supplier-credit-match-reversed-1.png",
      "supplier-credit-matched-1.png",
      "supplier-credit-recovered-1.png",
    ]);
  } finally {
    await rm(temp, { recursive: true, force: true });
  }
});
test("refuses to claim complete visual evidence if a required screenshot is missing", async () => {
  const temp = await mkdtemp(path.join(tmpdir(), "helivanta-supplier-evidence-"));
  try {
    const source = path.join(temp, "results"),
      fixture = path.join(source, "pharmacy-supplier-return-dispatch-specs", "attachments");
    await mkdir(fixture, { recursive: true });
    await writeFile(path.join(fixture, "supplier-credit-recovered-1.png"), "TEST image");
    await assert.rejects(
      () => retainSupplierReturnEvidence(source, path.join(temp, "output")),
      /Missing required synthetic supplier screenshots/,
    );
  } finally {
    await rm(temp, { recursive: true, force: true });
  }
});
