# e2e runs against production builds of the web apps

**Issue:** [#976](https://github.com/tesserix/helivanta/issues/976)

## The problem, stated precisely

`make e2e` drives Playwright against the shell and the three zone apps running
under `next dev` (Turbopack). The shell reaches each zone through its
`rewrites()` proxy. About once in 60 runs, the proxy gets `read ECONNRESET`
from a zone's dev server for one Turbopack chunk and answers 500 (issue
comment, 2026-10-12, with traces and a local reproduction). The page then
never hydrates and never calls the API, so whichever spec is on it times out.

What triggers the reset was not found. A dev-only chunk compiler and HMR
client sit in the failing path. Production does not run them, so e2e was
testing a server production never runs. The decision (option 1 in the issue)
is to run e2e against production builds, so the test exercises what ships.

## D1 — Production standalone builds, run the way the image runs them

Every web app is built with `next build` (`output: "standalone"`, already
configured) and run with `node <app>/server.js`. That is `Dockerfile.shell`'s
CMD, and the static assets and `public/` are copied beside it the same way.
No `next start`: Next warns it is not the supported way to run a standalone
build.

## D2 — Three shell builds, because two values are fixed at build time

`NEXT_PUBLIC_ZITADEL_CLIENT_ID` is inlined into the client bundle, and the
shell's `rewrites()` destinations, `API_URL` included, are resolved into the
routes manifest at build time. The three e2e stacks differ in exactly those:

| Stack | Zitadel app (client id from) | `API_URL` | Port |
|---|---|---|---|
| main | `dev/zitadel/secrets/zitadel.env` | `:8080` | 4301 |
| idle-timeout | `…/zitadel-idle-timeout.env` | `:8099` | 4399 |
| renewal | `…/zitadel-renewal.env` | `:8098` | 4398 |

So the shell is built three times. The zones are built once each, against the
main stack, which is what they served before.

## D3 — Builds are assembled into one folder per app, inside the repo

Each build is copied out of `apps/<app>/.next` into
`$REPO_ROOT/.e2e-web/<name>/`, which is gitignored. That means
`.next/standalone`, then `.next/static` and `public/` placed beside the
server. The next build of the same app then overwrites `.next` freely.

Why not a different `distDir` per variant: `next build` rewrites the app's
`tsconfig.json` `include` to add the dist dir's type folders, which would
dirty the working tree on every run.

Why inside the repo: the standalone `server.js` calls `process.chdir(__dirname)`,
and `e2e.sh` will only stop a process whose working directory is in this
repository (`scripts/lib/repo-owns.sh`). A build in `$TMPDIR` could never be
stopped by its own script.

## D4 — `e2e.sh` owns the web tier for the run

- Before phase 1, it stops this repo's processes on 4301–4304, remembering
  whether any were running, builds everything, and starts the four
  main-stack servers.
- Phases 2 and 3 start their own shell build on 4399 or 4398. The main web
  apps **no longer need stopping**. They were stopped only because two
  `next dev` servers cannot share `apps/shell`. Standalone builds share no
  directory.
- On exit it stops every server it started. It restarts `make dev-web`
  only if a dev web stack was running when it began, so a developer's
  stack is put back and CI's is not.
- The API stays `go run`, already a compiled binary. It is started by the
  caller (main) or by `e2e.sh` (fixtures), as before.

CI's e2e job stops starting `dev-web` and waiting on the zone ports. It starts
only `dev-api`.

## D5 — An ingress that routes like production, not the shell's rewrites (added while implementing)

The first full run against production builds **reproduced the reset**:
`pagination.spec` died the way the `next dev` runs did, with the shell
logging `Failed to proxy http://localhost:4302/medicore/_next/static/chunks/…
Error: read ECONNRESET`. So the trigger is not `next dev`. It is the shell's
`rewrites()` proxy to the zones.

Two facts explain it. Next's bundled proxy (`next/dist/compiled/httpxy`) keeps
its own keep-alive agent (`keepAlive: true`) to the zone. The zone's Node
server closes idle keep-alive connections after 5 s
(`server.keepAliveTimeout`, Node's default). When a request reuses a socket the
server is closing at that moment, the proxy reads a reset and answers 500.

Production never takes that path. The Istio VirtualService routes `/api/v1/`
to the API, and per its own notes will route the zones directly, "and the
shell's baked rewrites stay unused in production". So the e2e stack now
routes the same way. A Caddy container (`docker-compose.dev.yml`'s
`e2e-ingress`, profile `e2e`, configured by `dev/e2e/Caddyfile`) sits on the
ports browsers and Zitadel's redirect URIs use (4301, 4399, 4398). It sends
`/api/auth/*` to the shell, `/api/v1/*` to the API with the `/api` prefix
stripped, each zone to its own server, and everything else to that stack's
shell, which now listens on an internal port (4311, 4409, 4408).

Its upstream keep-alive is **4 s**, below the servers' 5 s, so the proxy
always closes an idle connection before the server can. That makes the race
impossible by construction rather than rare. Production's Envoy has the same
exposure once zones deploy (#890). Envoy's default upstream idle timeout is
1 h, so that rule belongs in the zone routes when they are added.

## D6 — The tests read the stored session, and the shell's test hook is gone (added while implementing)

`ratelimit.spec` and `signout.spec` read the ID token through
`window.__hmsUserManager`, which `apps/shell/lib/oidc.ts` set only when
`NODE_ENV !== "production"`. Production builds never set it, so those specs
now read oidc-client-ts's own `sessionStorage` entry
(`e2e/tests/support/stored-session.ts`). That is the same value `getUser()`
returned, and it is what script in the page, the attacker `signout.spec`
models, actually reaches. The hook and its type file are deleted: nothing
else used them, and production code should not carry a test-only branch.

## What this slice does not cover

- `make dev` still proxies zones through the shell's rewrites, so the reset
  can still appear as a dev-time glitch. Production builds and the e2e
  suite no longer take that path.
- Caching builds across CI runs. Every run builds from source, which is the
  point.
- Zone routes in production's VirtualService (#890). When they are added,
  they need an upstream idle timeout below the zones' 5 s keep-alive (D5).

## Proof

- Locally, a repeat run of smoke and journey against the production stack,
  at least as many runs as the 60 that reproduced one failure under `next dev`.
- The full `make e2e`, all three phases, green locally and on CI, with the
  working tree clean afterwards.
- `make e2e` started with a developer's `dev-web` already running leaves it
  running afterwards. Started with none, it leaves none.
