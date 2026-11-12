import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export function configure(root) {
  for (const name of [
    "apt-mirrors.txt",
    "apt-mirrors-security.txt",
    "sources.list.d/ubuntu.sources",
  ]) {
    const file = path.join(root, name);
    if (!existsSync(file)) continue;
    const original = readFileSync(file, "utf8");
    const updated = original
      .replaceAll("http://azure.archive.ubuntu.com/ubuntu", "https://archive.ubuntu.com/ubuntu")
      .replaceAll("https://azure.archive.ubuntu.com/ubuntu", "https://archive.ubuntu.com/ubuntu");
    if (updated !== original) writeFileSync(file, updated);
  }
}

if (path.resolve(process.argv[1] ?? "") === fileURLToPath(import.meta.url)) {
  configure("/etc/apt");
}
