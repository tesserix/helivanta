// Side-effect import — MUST come first; see the note on WEB_HOST below.
import "./load-env";

// The hostnames this harness actually runs against — read from the SAME
// environment variables the Makefile and docker-compose.dev.yml use, so
// there is exactly one place the topology is decided (#916 Task 4).
//
// Reading these rather than hardcoding them is not tidiness. The app and
// the IdP sit on DIFFERENT REGISTRABLE DOMAINS on purpose, because that is
// what production does (helivanta.app vs auth.tesserix.app) and therefore
// what makes every browser request between them cross-site — the condition
// #916 turned on and that the old localhost:4301 / localhost:20080 harness
// could not express, ports not being part of a "site".
//
// With the hostnames hardcoded in each spec, someone could set
// HELIVANTA_WEB_HOST and HELIVANTA_ZITADEL_HOST back to `localhost` to
// "simplify local dev", re-run `make dev-infra` — which re-registers the
// redirect URIs, so login keeps working — and put the whole suite back on
// one site while cross-site-harness.spec.ts, hardcoded, went on reporting
// green. A guard that cannot observe the thing it guards is the exact
// appearance-of-coverage failure this task exists to remove, so the guard
// reads the live values from here instead.
//
// `make e2e` forwards these explicitly (Makefile), and they are exported
// for every recipe besides. The `./load-env` import above is what makes the
// OTHER entry point — a bare `pnpm --filter e2e exec playwright test`, which
// scripts/verify-local.sh advertises and which involves no Make at all —
// read the same `.env` the Makefile does, instead of falling back to the
// defaults below and reporting green about a topology that is not running.
// It must stay the FIRST import in this file: ES modules evaluate their
// dependencies in import order, so anything that reads process.env before it
// would read the pre-.env environment.
export const WEB_HOST = process.env.HELIVANTA_WEB_HOST ?? "helivanta.localhost";
export const ZITADEL_HOST = process.env.HELIVANTA_ZITADEL_HOST ?? "auth.tesserix.localhost";
export const ZITADEL_PORT = process.env.HELIVANTA_ZITADEL_PORT ?? "20080";

export const ZITADEL_ORIGIN = `http://${ZITADEL_HOST}:${ZITADEL_PORT}`;

// The three shell instances the suite runs against. Ports are fixed (they
// are Next.js dev servers configured in package scripts and in the
// Makefile's two fixture blocks); only the host is configurable.
export const SHELL_PORT = 4301;
export const IDLE_TIMEOUT_WEB_PORT = 4399; // Makefile's HELIVANTA_IDLE_WEB_PORT
export const RENEWAL_WEB_PORT = 4398; // Makefile's HELIVANTA_RENEWAL_WEB_PORT

export function webOrigin(port: number): string {
  return `http://${WEB_HOST}:${port}`;
}
