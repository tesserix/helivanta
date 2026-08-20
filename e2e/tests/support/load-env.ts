import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// Loads the repo-root `.env` into process.env, for the ONE reason that
// `.env` is where this repo tells developers to put a host override.
//
// #916 Task 4 built the cross-site guard (cross-site-harness.spec.ts) so it
// reads the LIVE hostnames rather than string literals — that is what makes
// it a control rather than a restatement of an assumption. But it can only
// read what is actually in the environment, and there were two ways in with
// different environments:
//
//   - `make e2e` -> scripts/e2e.sh, which forwards HELIVANTA_WEB_HOST /
//     HELIVANTA_ZITADEL_HOST / HELIVANTA_ZITADEL_PORT explicitly (Makefile,
//     which `-include`s .env).
//   - `pnpm --filter e2e exec playwright test` — the command
//     scripts/verify-local.sh itself advertises — which involves no Make at
//     all, so nothing had read `.env`.
//
// On the second path the guard fell back to the compiled-in defaults in
// hosts.ts and reported GREEN about a topology that was not the one running:
// a developer who took .env.example's own advice ("Override these only if
// the names collide with something on your machine", lines 55-56) and set
// HELIVANTA_WEB_HOST=localhost got a suite silently back on one site with a
// spec named "…are cross-site" still passing. That is exactly the
// appearance-of-coverage shape Task 4 existed to remove, reintroduced one
// invocation path over.
//
// Environment WINS over the file, deliberately: `make e2e` and CI export
// these explicitly, and a stale `.env` on a developer's machine must not
// override what the caller actually asked for. Same precedence
// scripts/preflight.sh and scripts/verify-local.sh already use for the same
// variables.
//
// A missing or unreadable `.env` is not an error — a fresh clone has none,
// and the stock defaults are correct for it.
//
// Hand-rolled rather than `dotenv`: this repo's `.env` is a flat list of
// `KEY=value` lines (see .env.example) with no interpolation, no multi-line
// values and no export semantics, and adding a runtime dependency to the e2e
// package to parse eight lines is not a trade worth making.
function loadDotEnv(): void {
  // __dirname, not import.meta.url: Playwright transpiles TypeScript specs
  // and config to CommonJS, where import.meta is a syntax error. Resolved
  // from this file's own location rather than from process.cwd(), which
  // differs between `make e2e` (repo root) and a bare
  // `pnpm --filter e2e exec playwright test` (the e2e package dir).
  const path = resolve(__dirname, "../../../.env");
  let contents: string;
  try {
    contents = readFileSync(path, "utf8");
  } catch {
    return;
  }
  for (const rawLine of contents.split("\n")) {
    const line = rawLine.trim().replace(/^export\s+/, "");
    if (!line || line.startsWith("#")) continue;
    const eq = line.indexOf("=");
    if (eq <= 0) continue;
    const key = line.slice(0, eq).trim();
    if (key in process.env) continue;
    let value = line.slice(eq + 1).trim();
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    process.env[key] = value;
  }
}

loadDotEnv();
