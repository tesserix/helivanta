import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

export function verifyTemporaryAdvisories(ignored, now) {
  if (
    ignored.includes("GHSA-fc5q-39xc-vjpf") &&
    now.getTime() >= Date.parse("2026-11-26T00:00:00Z")
  )
    throw new Error(
      "The approved picomatch advisory exception expired; remove it or obtain a fresh reviewed decision.",
    );
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const config = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
  verifyTemporaryAdvisories(config.pnpm.auditConfig.ignoreGhsas, new Date());
}
