import { copyFile, lstat, mkdir, readdir } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";

const required = [
  "supplier-credit-recovered",
  "supplier-credit-matched",
  "supplier-credit-match-reversed",
  "supplier-credit-applied",
  "supplier-credit-application-reversed",
  "commercial-return-note-print",
];
export async function retainSupplierReturnEvidence(source, target) {
  const evidence = [];
  for (const entry of await readdir(source, { withFileTypes: true })) {
    if (!entry.isDirectory() || !entry.name.startsWith("pharmacy-supplier-return-")) continue;
    const directory = path.join(source, entry.name, "attachments");
    const stat = await lstat(directory).catch((error) => {
      if (error.code === "ENOENT") return null;
      throw error;
    });
    if (!stat?.isDirectory()) continue;
    for (const file of await readdir(directory, { withFileTypes: true })) {
      if (
        file.isFile() &&
        file.name.endsWith(".png") &&
        required.some((prefix) => file.name.startsWith(`${prefix}-`))
      )
        evidence.push({ from: path.join(directory, file.name), name: file.name });
    }
  }
  if (required.some((prefix) => !evidence.some((file) => file.name.startsWith(`${prefix}-`))))
    throw new Error(
      "Missing required synthetic supplier screenshots; visual acceptance remains incomplete.",
    );
  await mkdir(target, { recursive: true });
  for (const file of evidence) await copyFile(file.from, path.join(target, file.name));
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await retainSupplierReturnEvidence(
    path.resolve("e2e/test-results"),
    path.resolve("e2e/supplier-return-evidence"),
  );
  console.log("Retained named synthetic supplier-return visual evidence.");
}
