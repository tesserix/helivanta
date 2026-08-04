# HMS Frontend Standards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Machine-enforced frontend standards for all HMS zone apps: shared ESLint/Prettier/Vitest configs, a typed `@hms/api` data layer (TanStack Query + sonner), a UX kit (ConfirmDialog, RHF+zod forms, EmptyState, formatters), App Router conventions, a standards doc + CLAUDE.md + repo skill, a `new-zone` generator, and all existing panels migrated as reference implementations.

**Architecture:** `@hms/config` grows exported tool configs; new `@hms/api` workspace package owns fetch/query/env; `@hms/ui` grows the UX kit; each app wires lint/test scripts and App Router boundary files; docs + skill encode the non-mechanical rules; a generator stamps future zones.

**Tech Stack:** ESLint 9 flat config, typescript-eslint 8, eslint-plugin-react-hooks, eslint-plugin-jsx-a11y, @next/eslint-plugin-next, Prettier 3, Vitest 3 + jsdom + Testing Library, @tanstack/react-query 5, sonner 2, react-hook-form 7 + zod 3.24 + @hookform/resolvers 3, dompurify 3.

**Spec:** `docs/superpowers/specs/2026-08-04-frontend-standards-design.md`

## Global Constraints

- JS package manager pnpm via corepack, Node 22. Run `pnpm install` from repo root after changing any package.json.
- Workspace names: `@hms/config`, `@hms/api`, `@hms/ui`, `@hms/shell`, `@hms/medicore`, `@hms/pharmacy`, `@hms/lab`.
- Ports: shell 4301, medicore 4302, pharmacy 4303, lab 4304; next free zone port 4305+.
- All cross-zone/sidebar links stay plain `<a>`; zone nav lives only in `packages/ui/src/zones.ts`.
- Tokens only — no hardcoded colors in app code. `packages/ui/styles.css` documents two `@tesserix/web` pitfalls (`!important` for token overrides; unlayered `border-color` base rule) — do not "fix" them.
- Banned in app code (lint-enforced): `alert`/`confirm`/`prompt`, `console.log` (warn/error allowed), `any`, `dangerouslySetInnerHTML` (except via `sanitizeHtml`).
- Every form sets `noValidate`; validation is react-hook-form + zod with inline errors.
- The Playwright E2E (e2e/tests/smoke.spec.ts) must keep passing: it depends on label "Patient name", buttons "Create visit"/"Dispense"/"Save result"/"Sign in", label "Result for {patient}", texts "Dispensed", "Result: WBC 6.1", heading "Departments", link href `/medicore/opd`, login labels "Email"/"Password".
- Conventional single-line commits, no signatures, no Co-Authored-By.
- Zod stays at ^3.24 (pairs with @hookform/resolvers ^3); do not use zod 4.

---

### Task 1: `@hms/config` — ESLint, Prettier, Vitest presets wired into every app

**Files:**
- Create: `packages/config/eslint.config.mjs`, `packages/config/prettier.config.mjs`, `packages/config/vitest-preset.mjs`, `packages/config/vitest.setup.ts`
- Modify: `packages/config/package.json`, `turbo.json`, `.github/workflows/ci.yml`, every `apps/*/package.json` (lint/test scripts + devDeps), `apps/*/eslint.config.mjs` (create per app), `packages/ui/package.json`
- Test: running `pnpm turbo lint` across the repo

**Interfaces:**
- Produces: `@hms/config/eslint` (flat-config array factory `hmsEslint(dirname)`), `@hms/config/prettier`, `@hms/config/vitest` (vitest `defineProject`-compatible preset object factory `hmsVitest(dirname)`), setup file registering jest-dom matchers. Apps consume via 3-line config files. Turbo gains `lint` and keeps `test`; CI web job runs `pnpm turbo lint type-check test build`.

- [ ] **Step 1: Update `packages/config/package.json`**

```json
{
  "name": "@hms/config",
  "version": "0.0.0",
  "private": true,
  "exports": {
    "./tsconfig.base.json": "./tsconfig.base.json",
    "./eslint": "./eslint.config.mjs",
    "./prettier": "./prettier.config.mjs",
    "./vitest": "./vitest-preset.mjs",
    "./vitest-setup": "./vitest.setup.ts"
  },
  "dependencies": {
    "@eslint/js": "^9.18.0",
    "@next/eslint-plugin-next": "^16.0.0",
    "eslint": "^9.18.0",
    "eslint-plugin-jsx-a11y": "^6.10.2",
    "eslint-plugin-react-hooks": "^5.1.0",
    "typescript-eslint": "^8.20.0",
    "@testing-library/jest-dom": "^6.6.3",
    "jsdom": "^26.0.0",
    "vitest": "^3.0.0"
  }
}
```

- [ ] **Step 2: Write `packages/config/eslint.config.mjs`**

```js
import js from "@eslint/js";
import nextPlugin from "@next/eslint-plugin-next";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

// Shared HMS flat config. Apps call hmsEslint(import.meta.dirname).
export function hmsEslint(rootDir) {
  return tseslint.config(
    { ignores: [".next/**", "node_modules/**", "dist/**", "coverage/**"] },
    js.configs.recommended,
    ...tseslint.configs.recommended,
    jsxA11y.flatConfigs.recommended,
    {
      plugins: { "@next/next": nextPlugin, "react-hooks": reactHooks },
      rules: {
        ...nextPlugin.configs.recommended.rules,
        ...reactHooks.configs.recommended.rules,
        // HMS UX vocabulary: browser dialogs are banned (spec D4).
        "no-alert": "error",
        "no-console": ["error", { allow: ["warn", "error"] }],
        "@typescript-eslint/no-explicit-any": "error",
        "no-restricted-syntax": [
          "error",
          {
            selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
            message: "Use sanitizeHtml from @hms/ui instead of raw dangerouslySetInnerHTML.",
          },
        ],
      },
      settings: { next: { rootDir } },
    },
  );
}
```

- [ ] **Step 3: Write `packages/config/prettier.config.mjs`**

```js
/** Shared HMS Prettier config. */
export default {
  semi: true,
  singleQuote: false,
  trailingComma: "all",
  printWidth: 100,
};
```

- [ ] **Step 4: Write the Vitest preset + setup**

`packages/config/vitest-preset.mjs`:

```js
// Shared Vitest preset for HMS packages/apps (jsdom + Testing Library).
export function hmsVitest() {
  return {
    test: {
      environment: "jsdom",
      globals: true,
      setupFiles: ["@hms/config/vitest-setup"],
      passWithNoTests: true,
    },
  };
}
```

`packages/config/vitest.setup.ts`:

```ts
import "@testing-library/jest-dom/vitest";
```

- [ ] **Step 5: Wire every app and @hms/ui**

For EACH of `apps/shell`, `apps/medicore`, `apps/pharmacy`, `apps/lab`:

Create `apps/<app>/eslint.config.mjs`:

```js
import { hmsEslint } from "@hms/config/eslint";

export default hmsEslint(import.meta.dirname);
```

Create `apps/<app>/vitest.config.mts`:

```ts
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import { hmsVitest } from "@hms/config/vitest";

export default defineConfig({ plugins: [react()], ...hmsVitest() });
```

In each `apps/<app>/package.json` scripts add:

```json
    "lint": "eslint .",
    "test": "vitest run"
```

and add devDependencies:

```json
    "@vitejs/plugin-react": "^4.3.4",
    "vitest": "^3.0.0",
    "@testing-library/react": "^16.1.0",
    "@testing-library/user-event": "^14.5.2"
```

(eslint itself resolves from `@hms/config`'s dependency via pnpm hoisting; if `eslint .` fails to resolve, add `"eslint": "^9.18.0"` to the app's devDependencies too.)

For `packages/ui/package.json`: add the same `lint`/`test` scripts, the same vitest devDeps, an `eslint.config.mjs` (same 3 lines), and a `vitest.config.mts` (same content, plugin-react included).

- [ ] **Step 6: turbo + CI**

In `turbo.json` tasks add:

```json
    "lint": {},
```

(`test` already exists.) In `.github/workflows/ci.yml` web job change the turbo line to:

```yaml
      - run: pnpm turbo lint type-check test build
```

- [ ] **Step 7: Install, lint, fix violations**

Run: `pnpm install && pnpm turbo lint`
Expected: ESLint actually executes per app. Fix any violations it finds in existing code (expected: none or trivial — there are no `console.log`/`alert`/`any` in the current app code; jsx-a11y may flag something small — fix, don't disable, unless the rule is genuinely wrong for the case, then disable with an inline comment explaining why).

Run: `pnpm turbo test`
Expected: PASS (no test files yet → passWithNoTests).

- [ ] **Step 8: Commit**

```bash
git add packages/config turbo.json .github apps packages/ui pnpm-lock.yaml
git commit -m "feat: shared eslint, prettier and vitest configs wired into all apps"
```

---

### Task 2: `@hms/api` — typed client, envelope, poll constant, env helper

**Files:**
- Create: `packages/api/package.json`, `packages/api/tsconfig.json`, `packages/api/eslint.config.mjs`, `packages/api/vitest.config.mts`, `packages/api/src/index.ts`, `packages/api/src/client.ts`, `packages/api/src/env.ts`
- Test: `packages/api/src/client.test.ts`, `packages/api/src/env.test.ts`

**Interfaces:**
- Produces (consumed by Tasks 3, 6–8):
  - `apiFetch<T>(path: string, init?: RequestInit): Promise<T>` — prefixes `/api/v1`, sends/parses JSON, throws `ApiError` on non-2xx.
  - `class ApiError extends Error { code: string; status: number }` — `code` from the envelope's `error` field (fallback `"internal"`), `message` from `message` (fallback generic).
  - `unwrapData<T>(body: { data?: T }): T[]`-style helper is NOT provided — list endpoints return `{data: T[]}`; callers type `apiFetch<{ data: Row[] }>` directly.
  - `POLL_INTERVAL_MS = 3000`.
  - `defineEnv<S extends z.ZodRawShape>(shape: S, values: Record<string, string | undefined>): z.infer<z.ZodObject<S>>` — parses, throws with a readable message listing missing/invalid keys.

- [ ] **Step 1: Package scaffolding**

`packages/api/package.json`:

```json
{
  "name": "@hms/api",
  "version": "0.0.0",
  "private": true,
  "exports": {
    ".": "./src/index.ts",
    "./testing": "./src/testing.tsx"
  },
  "scripts": {
    "lint": "eslint .",
    "test": "vitest run",
    "type-check": "tsc --noEmit"
  },
  "dependencies": {
    "@tanstack/react-query": "^5.62.0",
    "sonner": "^2.0.0",
    "zod": "^3.24.1"
  },
  "peerDependencies": {
    "react": "^19.0.0"
  },
  "devDependencies": {
    "@hms/config": "workspace:*",
    "@testing-library/react": "^16.1.0",
    "@types/react": "^19",
    "@vitejs/plugin-react": "^4.3.4",
    "react": "^19.0.0",
    "react-dom": "^19.0.0",
    "typescript": "^5.7.0",
    "vitest": "^3.0.0"
  }
}
```

`packages/api/tsconfig.json` — copy `packages/ui/tsconfig.json` verbatim.
`packages/api/eslint.config.mjs` and `packages/api/vitest.config.mts` — same 3-line pattern as Task 1 Step 5.

- [ ] **Step 2: Write the failing client tests**

`packages/api/src/client.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, POLL_INTERVAL_MS, apiFetch } from "./client";

function mockFetch(status: number, body: unknown) {
  const fn = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fn);
  return fn;
}

afterEach(() => vi.unstubAllGlobals());

describe("apiFetch", () => {
  it("prefixes /api/v1 and returns parsed JSON", async () => {
    const fn = mockFetch(200, { data: [{ id: "1" }] });
    const body = await apiFetch<{ data: { id: string }[] }>("/medicore/visits");
    expect(fn).toHaveBeenCalledWith("/api/v1/medicore/visits", expect.any(Object));
    expect(body.data[0].id).toBe("1");
  });

  it("sends JSON bodies with the right content type", async () => {
    const fn = mockFetch(202, { id: "abc" });
    await apiFetch("/medicore/visits", {
      method: "POST",
      body: JSON.stringify({ patient_name: "X" }),
    });
    const init = fn.mock.calls[0][1] as RequestInit;
    expect(new Headers(init.headers).get("Content-Type")).toBe("application/json");
  });

  it("throws a typed ApiError from the platform envelope", async () => {
    mockFetch(409, { error: "conflict", message: "already dispensed" });
    const err = await apiFetch("/x").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).code).toBe("conflict");
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).message).toBe("already dispensed");
  });

  it("falls back to a generic error when the body is not the envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("boom", { status: 500 })),
    );
    const err = await apiFetch("/x").catch((e: unknown) => e);
    expect((err as ApiError).code).toBe("internal");
  });

  it("exports the shared poll interval", () => {
    expect(POLL_INTERVAL_MS).toBe(3000);
  });
});
```

`packages/api/src/env.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { z } from "zod";
import { defineEnv } from "./env";

describe("defineEnv", () => {
  it("returns parsed values", () => {
    const env = defineEnv({ API_URL: z.string().url() }, { API_URL: "http://localhost:8080" });
    expect(env.API_URL).toBe("http://localhost:8080");
  });

  it("throws naming every bad key", () => {
    expect(() =>
      defineEnv({ A: z.string().min(1), B: z.string().min(1) }, { A: "" }),
    ).toThrowError(/A[\s\S]*B/);
  });
});
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `pnpm install && pnpm --filter @hms/api test`
Expected: FAIL — `./client` / `./env` don't exist.

- [ ] **Step 4: Implement**

`packages/api/src/client.ts`:

```ts
const API_PREFIX = "/api/v1";

export const POLL_INTERVAL_MS = 3000;

export class ApiError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

type Envelope = { error?: string; message?: string };

// Same-origin client for the Go API. Every zone reaches the backend
// through its /api rewrite, so the session cookie flows automatically.
export async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(`${API_PREFIX}${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...init.headers },
  });
  if (!res.ok) {
    let envelope: Envelope = {};
    try {
      envelope = (await res.json()) as Envelope;
    } catch {
      // non-JSON error body — fall through to the generic error
    }
    throw new ApiError(
      envelope.error ?? "internal",
      envelope.message ?? "Something went wrong. Try again.",
      res.status,
    );
  }
  return (await res.json()) as T;
}
```

`packages/api/src/env.ts`:

```ts
import { z } from "zod";

// Fail-fast env access: call at module load with the exact process.env
// keys the app needs. Throws one readable error naming every bad key.
export function defineEnv<S extends z.ZodRawShape>(
  shape: S,
  values: Record<string, string | undefined>,
): z.infer<z.ZodObject<S>> {
  const parsed = z.object(shape).safeParse(values);
  if (!parsed.success) {
    const lines = parsed.error.issues.map((i) => `  ${i.path.join(".")}: ${i.message}`);
    throw new Error(`Invalid environment:\n${lines.join("\n")}`);
  }
  return parsed.data;
}
```

`packages/api/src/index.ts`:

```ts
export { ApiError, POLL_INTERVAL_MS, apiFetch } from "./client";
export { defineEnv } from "./env";
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `pnpm --filter @hms/api test && pnpm --filter @hms/api type-check && pnpm --filter @hms/api lint`
Expected: PASS / clean.

- [ ] **Step 6: Commit**

```bash
git add packages/api pnpm-lock.yaml
git commit -m "feat: hms api package with typed client, envelope errors and env helper"
```

---

### Task 3: `@hms/api` — AppProviders, query/mutation hooks, testing utils

**Files:**
- Create: `packages/api/src/providers.tsx`, `packages/api/src/hooks.ts`, `packages/api/src/testing.tsx`
- Modify: `packages/api/src/index.ts`
- Test: `packages/api/src/hooks.test.tsx`

**Interfaces:**
- Consumes: `apiFetch`, `ApiError`, `POLL_INTERVAL_MS` (Task 2).
- Produces (consumed by Tasks 5–8):
  - `<AppProviders>{children}</AppProviders>` — client component mounting `QueryClientProvider` (staleTime 5s, retry 1) and sonner `<Toaster richColors position="top-right" />`.
  - `useApiQuery<T>(key: unknown[], path: string, opts?: { poll?: boolean })` — returns TanStack `useQuery` result; `poll: true` sets `refetchInterval: POLL_INTERVAL_MS`.
  - `useApiMutation<TData, TVars>(fn: (vars: TVars) => Promise<TData>, opts?: { successToast?: string; invalidate?: unknown[][]; onSuccess?: (d: TData) => void })` — toasts `error.message` on failure (`toast.error`), toasts `successToast` when given, invalidates the given query keys.
  - `renderWithProviders(ui: ReactElement)` from `@hms/api/testing` — renders inside a fresh QueryClient (retry off) + Toaster for component tests.

- [ ] **Step 1: Write the failing hook test**

`packages/api/src/hooks.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useApiMutation, useApiQuery } from "./hooks";
import { renderWithProviders } from "./testing";

function QueryProbe() {
  const q = useApiQuery<{ data: { id: string }[] }>(["probe"], "/probe");
  if (q.isPending) return <p>loading</p>;
  if (q.isError) return <p role="alert">{q.error.message}</p>;
  return <p>{q.data.data[0].id}</p>;
}

function MutationProbe() {
  const m = useApiMutation(() => Promise.reject(new Error("nope")));
  return <button onClick={() => m.mutate(undefined)}>go</button>;
}

afterEach(() => vi.unstubAllGlobals());

describe("useApiQuery", () => {
  it("fetches through apiFetch and renders data", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ data: [{ id: "v-1" }] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    renderWithProviders(<QueryProbe />);
    await waitFor(() => expect(screen.getByText("v-1")).toBeInTheDocument());
  });
});

describe("useApiMutation", () => {
  it("surfaces failures as an error toast", async () => {
    const { user } = renderWithProviders(<MutationProbe />);
    await user.click(screen.getByRole("button", { name: "go" }));
    await waitFor(() => expect(document.body.textContent).toContain("nope"));
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter @hms/api test`
Expected: FAIL — `./hooks` / `./testing` missing.

- [ ] **Step 3: Implement**

`packages/api/src/providers.tsx`:

```tsx
"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "sonner";
import { useState, type ReactNode } from "react";

// Every zone layout wraps its children in AppProviders: one query
// client + one toast outlet per app (spec D2/D4).
export function AppProviders({ children }: { children: ReactNode }) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: { queries: { staleTime: 5_000, retry: 1 } },
      }),
  );
  return (
    <QueryClientProvider client={client}>
      {children}
      <Toaster richColors position="top-right" />
    </QueryClientProvider>
  );
}
```

`packages/api/src/hooks.ts`:

```ts
"use client";

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { toast } from "sonner";
import { POLL_INTERVAL_MS, apiFetch } from "./client";

export function useApiQuery<T>(
  key: unknown[],
  path: string,
  opts: { poll?: boolean } = {},
): UseQueryResult<T, Error> {
  return useQuery({
    queryKey: key,
    queryFn: () => apiFetch<T>(path),
    refetchInterval: opts.poll ? POLL_INTERVAL_MS : undefined,
  });
}

export function useApiMutation<TData, TVars = void>(
  fn: (vars: TVars) => Promise<TData>,
  opts: {
    successToast?: string;
    invalidate?: unknown[][];
    onSuccess?: (data: TData) => void;
  } = {},
): UseMutationResult<TData, Error, TVars> {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (data) => {
      if (opts.successToast) toast.success(opts.successToast);
      for (const key of opts.invalidate ?? []) {
        void queryClient.invalidateQueries({ queryKey: key });
      }
      opts.onSuccess?.(data);
    },
    onError: (error) => {
      toast.error(error.message);
    },
  });
}
```

`packages/api/src/testing.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Toaster } from "sonner";
import type { ReactElement } from "react";

// Component-test harness: fresh query client (no retries), toast outlet,
// and a wired-up user-event instance.
export function renderWithProviders(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const user = userEvent.setup();
  const result = render(
    <QueryClientProvider client={client}>
      {ui}
      <Toaster />
    </QueryClientProvider>,
  );
  return { ...result, user };
}
```

Append to `packages/api/src/index.ts`:

```ts
export { AppProviders } from "./providers";
export { useApiMutation, useApiQuery } from "./hooks";
```

Add `@testing-library/user-event": "^14.5.2"` to `packages/api` devDependencies.

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm install && pnpm --filter @hms/api test && pnpm --filter @hms/api lint type-check`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add packages/api pnpm-lock.yaml
git commit -m "feat: app providers with react query and sonner plus typed hooks"
```

---

### Task 4: `@hms/ui` UX kit — ConfirmDialog, form primitives, EmptyState, formatters, sanitizeHtml

**Files:**
- Create: `packages/ui/src/confirm-dialog.tsx`, `packages/ui/src/form.tsx`, `packages/ui/src/empty-state.tsx`, `packages/ui/src/format.ts`, `packages/ui/src/sanitize.ts`
- Modify: `packages/ui/src/index.ts`, `packages/ui/package.json`
- Test: `packages/ui/src/confirm-dialog.test.tsx`, `packages/ui/src/form.test.tsx`, `packages/ui/src/format.test.ts`

**Interfaces:**
- Consumes: `@tesserix/web` `Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, Button, Input, Label`; `react-hook-form`; `zod`; `dompurify`.
- Produces (consumed by Tasks 6–8 and the generator):
  - `<ConfirmDialog open onOpenChange title description confirmLabel onConfirm busy? />` — destructive-styled confirm button; the ONLY sanctioned confirmation UI.
  - `HmsForm` helpers: `useZodForm(schema, defaults?)` (RHF `useForm` with `zodResolver` + `mode: "onTouched"`), `<Field label error id>{input}</Field>` rendering label + inline `<p role="alert">` error.
  - `<EmptyState icon title hint />`.
  - `formatTime(iso: string): string` and `formatDateTime(iso: string): string` — `Intl.DateTimeFormat` based, stable across the app.
  - `sanitizeHtml(html: string): { __html: string }` — the only allowed path to `dangerouslySetInnerHTML`.

- [ ] **Step 1: Add dependencies to `packages/ui/package.json`**

```json
  "dependencies": {
    "lucide-react": "^0.469.0",
    "dompurify": "^3.2.3",
    "react-hook-form": "^7.54.2",
    "@hookform/resolvers": "^3.9.1",
    "zod": "^3.24.1"
  },
  "peerDependencies": {
    "react": "^19.0.0",
    "@tesserix/web": "^1.8.0"
  }
```

(Also add `@tesserix/web` + react/react-dom + Testing Library + @vitejs/plugin-react to devDependencies so the package's own tests can render.)

- [ ] **Step 2: Write the failing tests**

`packages/ui/src/confirm-dialog.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ConfirmDialog } from "./confirm-dialog";

describe("ConfirmDialog", () => {
  it("confirms and cancels", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    const onOpenChange = vi.fn();
    render(
      <ConfirmDialog
        open
        onOpenChange={onOpenChange}
        title="Delete medication?"
        description="This cannot be undone."
        confirmLabel="Delete"
        onConfirm={onConfirm}
      />,
    );
    expect(screen.getByText("This cannot be undone.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Delete" }));
    expect(onConfirm).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
```

`packages/ui/src/form.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { Field, useZodForm } from "./form";

const schema = z.object({ name: z.string().min(1, "Name is required") });

function Probe({ onValid }: { onValid: (v: { name: string }) => void }) {
  const form = useZodForm(schema, { name: "" });
  return (
    <form noValidate onSubmit={form.handleSubmit(onValid)}>
      <Field id="name" label="Name" error={form.formState.errors.name?.message}>
        <input id="name" {...form.register("name")} />
      </Field>
      <button type="submit">Save</button>
    </form>
  );
}

describe("useZodForm + Field", () => {
  it("shows inline zod errors instead of native validation", async () => {
    const user = userEvent.setup();
    const onValid = vi.fn();
    render(<Probe onValid={onValid} />);
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Name is required");
    expect(onValid).not.toHaveBeenCalled();
    await user.type(screen.getByLabelText("Name"), "Asha");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(onValid).toHaveBeenCalledWith({ name: "Asha" }, expect.anything());
  });
});
```

`packages/ui/src/format.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { formatDateTime, formatTime } from "./format";

describe("formatters", () => {
  it("formats times and datetimes deterministically", () => {
    const iso = "2026-08-04T04:05:06Z";
    expect(formatTime(iso)).toMatch(/\d/);
    expect(formatDateTime(iso)).toMatch(/\d{4}|\d{2}/);
    expect(formatTime(iso)).toBe(formatTime(iso));
  });
});
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `pnpm install && pnpm --filter @hms/ui test`
Expected: FAIL — modules missing.

- [ ] **Step 4: Implement**

`packages/ui/src/confirm-dialog.tsx`:

```tsx
"use client";

import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@tesserix/web";

// The only sanctioned confirmation UI (spec D4): reserved for
// destructive actions. Never use window.confirm.
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  onConfirm,
  busy = false,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel: string;
  onConfirm: () => void;
  busy?: boolean;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={onConfirm} disabled={busy}>
            {busy ? "Working…" : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

`packages/ui/src/form.tsx`:

```tsx
"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, type DefaultValues, type FieldValues } from "react-hook-form";
import type { ReactNode } from "react";
import type { z } from "zod";

// HMS forms are react-hook-form + zod with inline errors (spec D3).
// Always set noValidate on the <form> — native validation is banned.
export function useZodForm<S extends z.ZodType<FieldValues>>(
  schema: S,
  defaultValues?: DefaultValues<z.infer<S>>,
) {
  return useForm<z.infer<S>>({
    resolver: zodResolver(schema),
    defaultValues,
    mode: "onTouched",
  });
}

export function Field({
  id,
  label,
  error,
  children,
}: {
  id: string;
  label: string;
  error?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5 text-sm font-medium">
      <label htmlFor={id}>{label}</label>
      {children}
      {error && (
        <p role="alert" className="text-sm font-normal text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
```

`packages/ui/src/empty-state.tsx`:

```tsx
import type { LucideIcon } from "lucide-react";

export function EmptyState({
  icon: Icon,
  title,
  hint,
}: {
  icon: LucideIcon;
  title: string;
  hint?: string;
}) {
  return (
    <div className="flex flex-col items-center gap-2 px-5 py-10 text-center">
      <Icon className="h-8 w-8 text-muted-foreground/60" aria-hidden="true" />
      <p className="text-sm font-medium text-foreground">{title}</p>
      {hint && <p className="text-sm text-muted-foreground">{hint}</p>}
    </div>
  );
}
```

`packages/ui/src/format.ts`:

```ts
// One place for clinical timestamp rendering — never call
// toLocaleTimeString directly in components.
const time = new Intl.DateTimeFormat(undefined, { timeStyle: "medium" });
const dateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
});

export function formatTime(iso: string): string {
  return time.format(new Date(iso));
}

export function formatDateTime(iso: string): string {
  return dateTime.format(new Date(iso));
}
```

`packages/ui/src/sanitize.ts`:

```ts
import DOMPurify from "dompurify";

// The only allowed path to dangerouslySetInnerHTML (lint-enforced).
export function sanitizeHtml(html: string): { __html: string } {
  return { __html: DOMPurify.sanitize(html) };
}
```

Append to `packages/ui/src/index.ts`:

```ts
export { ConfirmDialog } from "./confirm-dialog";
export { Field, useZodForm } from "./form";
export { EmptyState } from "./empty-state";
export { formatDateTime, formatTime } from "./format";
export { sanitizeHtml } from "./sanitize";
```

Note: the sanitize module must NOT itself trip the lint ban — the ban targets the JSX attribute, and this file has none.

- [ ] **Step 5: Run tests to verify they pass**

Run: `pnpm --filter @hms/ui test && pnpm --filter @hms/ui lint type-check`
Expected: PASS. If a `@tesserix/web` Dialog import fails at test time due to CSS imports, add `css: false`-style handling by mocking `@tesserix/web` styles — do NOT mock the components themselves.

- [ ] **Step 6: Commit**

```bash
git add packages/ui pnpm-lock.yaml
git commit -m "feat: ux kit with confirm dialog, zod forms, empty state and formatters"
```

---

### Task 5: App Router conventions — providers, error/loading/not-found, .env.example

**Files:**
- Modify: `apps/{shell,medicore,pharmacy,lab}/app/layout.tsx`, `apps/{shell,medicore,pharmacy,lab}/package.json` (add `"@hms/api": "workspace:*"`, extend `transpilePackages` in next.config.ts to `["@hms/ui", "@hms/api"]`)
- Create per app: `app/error.tsx`, `app/loading.tsx`, `app/not-found.tsx`, `.env.example`
- Create: `apps/shell/lib/env.ts` (typed env for the firebase vars)

**Interfaces:**
- Consumes: `AppProviders` (Task 3), `defineEnv` (Task 2), `@tesserix/web` `ListSkeleton` (or `Skeleton`).
- Produces: every zone renders inside `AppProviders`; every zone has friendly error/loading/not-found; `.env.example` documents each app's vars.

- [ ] **Step 1: Wrap layouts**

In EACH app's `app/layout.tsx`, wrap children:

```tsx
import type { Metadata } from "next";
import { AppProviders } from "@hms/api";
import "./globals.css";

export const metadata: Metadata = { title: "HMS" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" data-theme="default">
      <body>
        <AppProviders>{children}</AppProviders>
      </body>
    </html>
  );
}
```

Add `"@hms/api": "workspace:*"` to each app's dependencies and change each `next.config.ts` to `transpilePackages: ["@hms/ui", "@hms/api"]`.

- [ ] **Step 2: Boundary files (identical content per app)**

`app/error.tsx`:

```tsx
"use client";

import { Button } from "@tesserix/web";

export default function ErrorBoundary({
  error,
  reset,
}: {
  error: Error;
  reset: () => void;
}) {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-4 p-6 text-center">
      <h1 className="text-xl font-semibold text-foreground">Something went wrong</h1>
      <p className="max-w-md text-sm text-muted-foreground">
        {error.message || "An unexpected error occurred."}
      </p>
      <Button onClick={reset}>Try again</Button>
    </main>
  );
}
```

`app/loading.tsx`:

```tsx
import { ListSkeleton } from "@tesserix/web";

export default function Loading() {
  return (
    <main className="p-6">
      <ListSkeleton />
    </main>
  );
}
```

(If `ListSkeleton` renders poorly, use `<Skeleton className="h-32 w-full max-w-2xl" />` from `@tesserix/web` instead — check visually.)

`app/not-found.tsx`:

```tsx
export default function NotFound() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-2 p-6 text-center">
      <h1 className="text-xl font-semibold text-foreground">Page not found</h1>
      <p className="text-sm text-muted-foreground">
        Check the address, or head back to the <a className="underline underline-offset-4" href="/">dashboard</a>.
      </p>
    </main>
  );
}
```

- [ ] **Step 3: Typed env for shell + .env.example files**

`apps/shell/lib/env.ts`:

```ts
import { z } from "zod";
import { defineEnv } from "@hms/api";

// NEXT_PUBLIC_ vars are inlined at build time, so they must be read
// statically (no dynamic process.env access).
export const env = defineEnv(
  {
    NEXT_PUBLIC_GIP_API_KEY: z.string().min(1).default("demo-key"),
    NEXT_PUBLIC_GIP_PROJECT_ID: z.string().min(1).default("demo-hms"),
  },
  {
    NEXT_PUBLIC_GIP_API_KEY: process.env.NEXT_PUBLIC_GIP_API_KEY,
    NEXT_PUBLIC_GIP_PROJECT_ID: process.env.NEXT_PUBLIC_GIP_PROJECT_ID,
  },
);
```

Then update `apps/shell/lib/firebase.ts` to read from `env` instead of raw `process.env` for those two vars (keep the existing emulator-host handling and dev fallbacks exactly as they are — check the file first; only swap the two reads that have `?? "demo-key"` / `?? "demo-hms"` fallbacks to use `env.NEXT_PUBLIC_GIP_API_KEY` / `env.NEXT_PUBLIC_GIP_PROJECT_ID`; zod defaults now supply the fallbacks).

`.env.example` per app — shell:

```
# GIP (Firebase) web config — defaults target the local emulator stack.
NEXT_PUBLIC_GIP_API_KEY=demo-key
NEXT_PUBLIC_GIP_PROJECT_ID=demo-hms
NEXT_PUBLIC_AUTH_EMULATOR_HOST=localhost:9099
# Server-side rewrite targets
MEDICORE_URL=http://localhost:4302
PHARMACY_URL=http://localhost:4303
LAB_URL=http://localhost:4304
API_URL=http://localhost:8080
```

medicore/pharmacy/lab (identical):

```
# Server-side rewrite target for direct-hit /api/* (bypassing the shell)
API_URL=http://localhost:8080
```

- [ ] **Step 4: Verify**

Run: `pnpm install && pnpm turbo lint type-check build`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add apps pnpm-lock.yaml
git commit -m "feat: app providers, error and loading boundaries, typed env examples"
```

---

### Task 6: Migrate medicore panels (visits + pings) to the standard stack

**Files:**
- Modify: `apps/medicore/components/visit-panel.tsx`, `apps/medicore/components/ping-panel.tsx`, `apps/medicore/package.json` (add `"@hms/api": "workspace:*"` if Task 5 didn't, plus test devDeps already present)
- Test: `apps/medicore/components/visit-panel.test.tsx`

**Interfaces:**
- Consumes: `useApiQuery`, `useApiMutation`, `apiFetch` (`@hms/api`); `Field`, `useZodForm`, `EmptyState`, `formatTime` (`@hms/ui`); `Badge`, `Button`, `Input` (`@tesserix/web`); `renderWithProviders` (`@hms/api/testing`).
- Produces: the reference form + list implementation the standards doc links to. E2E strings preserved: label "Patient name", button "Create visit".

- [ ] **Step 1: Write the failing component test**

`apps/medicore/components/visit-panel.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { VisitPanel } from "./visit-panel";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => vi.unstubAllGlobals());

describe("VisitPanel", () => {
  it("lists visits and creates one with the form", async () => {
    const fetchMock = vi
      .fn()
      // initial list
      .mockResolvedValueOnce(jsonResponse(200, { data: [] }))
      // create
      .mockResolvedValueOnce(jsonResponse(202, { id: "v-1" }))
      // refetch after invalidation
      .mockResolvedValue(
        jsonResponse(200, {
          data: [
            {
              id: "v-1",
              patient_name: "Asha Rao",
              department: "OPD",
              status: "open",
              created_at: "2026-08-04T04:00:00Z",
            },
          ],
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<VisitPanel department="OPD" />);
    expect(await screen.findByText(/No visits yet/)).toBeInTheDocument();

    await user.type(screen.getByLabelText("Patient name"), "Asha Rao");
    await user.click(screen.getByRole("button", { name: "Create visit" }));

    await waitFor(() => expect(screen.getByText("Asha Rao")).toBeInTheDocument());
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/medicore/visits",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("shows an inline error when the name is empty", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(200, { data: [] })));
    const { user } = renderWithProviders(<VisitPanel department="OPD" />);
    await user.click(await screen.findByRole("button", { name: "Create visit" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/required/i);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter @hms/medicore test`
Expected: FAIL — panel still uses useState/fetch and native `required`.

- [ ] **Step 3: Rewrite `visit-panel.tsx`**

```tsx
"use client";

import { z } from "zod";
import { Badge, Button, Input } from "@tesserix/web";
import { CalendarPlus } from "lucide-react";
import { apiFetch, useApiMutation, useApiQuery } from "@hms/api";
import { EmptyState, Field, formatTime, useZodForm } from "@hms/ui";

type Visit = {
  id: string;
  patient_name: string;
  department: string;
  status: string;
  created_at: string;
};

const visitSchema = z.object({
  patient_name: z.string().min(1, "Patient name is required").max(200),
});

export function VisitPanel({ department }: { department: "OPD" | "IPD" }) {
  const visits = useApiQuery<{ data: Visit[] }>(["visits"], "/medicore/visits");
  const form = useZodForm(visitSchema, { patient_name: "" });

  const createVisit = useApiMutation(
    (values: z.infer<typeof visitSchema>) =>
      apiFetch<{ id: string }>("/medicore/visits", {
        method: "POST",
        body: JSON.stringify({ ...values, department }),
      }),
    {
      successToast: "Visit created",
      invalidate: [["visits"]],
      onSuccess: () => form.reset(),
    },
  );

  return (
    <section className="max-w-2xl rounded-lg border bg-card">
      <div className="border-b px-5 py-4">
        <h2 className="text-sm font-semibold text-foreground">Visits</h2>
        <p className="mt-0.5 text-sm text-muted-foreground">
          New visits open a pending dispense in Pharmacy and a pending order in Lab.
        </p>
      </div>
      <form
        noValidate
        onSubmit={form.handleSubmit((values) => createVisit.mutate(values))}
        className="flex flex-wrap items-end gap-3 border-b px-5 py-4"
      >
        <div className="min-w-56 flex-1">
          <Field
            id="patient_name"
            label="Patient name"
            error={form.formState.errors.patient_name?.message}
          >
            <Input id="patient_name" placeholder="e.g. Asha Rao" {...form.register("patient_name")} />
          </Field>
        </div>
        <Button type="submit" disabled={createVisit.isPending}>
          {createVisit.isPending ? "Creating…" : "Create visit"}
        </Button>
      </form>
      <ul className="divide-y text-sm">
        {visits.data?.data.length === 0 && (
          <li>
            <EmptyState
              icon={CalendarPlus}
              title="No visits yet"
              hint="Create the first one above."
            />
          </li>
        )}
        {visits.data?.data.map((v) => (
          <li key={v.id} className="flex items-center justify-between gap-4 px-5 py-3">
            <div className="flex min-w-0 items-center gap-3">
              <span className="truncate font-medium text-foreground">{v.patient_name}</span>
              <Badge variant="secondary">{v.department}</Badge>
            </div>
            <time className="shrink-0 tabular-nums text-muted-foreground">
              {formatTime(v.created_at)}
            </time>
          </li>
        ))}
      </ul>
    </section>
  );
}
```

- [ ] **Step 4: Rewrite `ping-panel.tsx` the same way**

Query `["pings"]` → `/reference/pings` (no polling), mutation POST `/reference/ping` with body `{ message: \`${department} ping\` }`, `successToast: "Ping sent"`, `invalidate: [["pings"]]`. Keep the card layout, `Button variant="outline"` labelled `` `Ping from ${department}` ``, `EmptyState` (icon `Activity` from lucide-react, title "No activity yet"), `formatTime` for timestamps. No form (single button) — no zod needed.

- [ ] **Step 5: Run tests to verify they pass**

Run: `pnpm --filter @hms/medicore test && pnpm turbo lint type-check build --filter=@hms/medicore`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/medicore
git commit -m "refactor: medicore panels on react query, zod forms and toasts"
```

---

### Task 7: Migrate pharmacy + lab panels

**Files:**
- Modify: `apps/pharmacy/components/dispense-list.tsx`, `apps/pharmacy/components/medications-panel.tsx`, `apps/lab/components/order-list.tsx`
- Test: `apps/pharmacy/components/dispense-list.test.tsx`, `apps/lab/components/order-list.test.tsx`

**Interfaces:**
- Consumes: same `@hms/api` + `@hms/ui` surfaces as Task 6.
- Produces: queue reference implementations. E2E strings preserved: button "Dispense", text "Dispensed", label `Result for {patient}`, button "Save result", text `Result: WBC 6.1`.

- [ ] **Step 1: Write the failing tests**

`apps/pharmacy/components/dispense-list.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { DispenseList } from "./dispense-list";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const pendingRow = {
  id: "d-1",
  visit_id: "v-1",
  patient_name: "Asha Rao",
  medication: "",
  status: "pending",
  dispensed_at: null,
  created_at: "2026-08-04T04:00:00Z",
};

afterEach(() => vi.unstubAllGlobals());

describe("DispenseList", () => {
  it("dispenses a pending row", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, { data: [pendingRow] }))
      .mockResolvedValueOnce(jsonResponse(200, { id: "d-1", status: "dispensed" }))
      .mockResolvedValue(
        jsonResponse(200, {
          data: [{ ...pendingRow, status: "dispensed", medication: "Paracetamol 500mg", dispensed_at: "2026-08-04T04:01:00Z" }],
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<DispenseList />);
    await user.click(await screen.findByRole("button", { name: "Dispense" }));
    await waitFor(() => expect(screen.getByText("Dispensed")).toBeInTheDocument());
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/pharmacy/dispenses/d-1/dispense",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("shows the empty state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(200, { data: [] })));
    renderWithProviders(<DispenseList />);
    expect(await screen.findByText(/No dispense tasks yet/)).toBeInTheDocument();
  });
});
```

`apps/lab/components/order-list.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { OrderList } from "./order-list";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const pendingOrder = {
  id: "o-1",
  visit_id: "v-1",
  patient_name: "Asha Rao",
  test_name: "CBC",
  status: "pending",
  result_value: null,
  resulted_at: null,
  created_at: "2026-08-04T04:00:00Z",
};

afterEach(() => vi.unstubAllGlobals());

describe("OrderList", () => {
  it("saves a result for a pending order", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, { data: [pendingOrder] }))
      .mockResolvedValueOnce(jsonResponse(200, { id: "o-1", status: "completed" }))
      .mockResolvedValue(
        jsonResponse(200, {
          data: [{ ...pendingOrder, status: "completed", result_value: "WBC 6.1", resulted_at: "2026-08-04T04:01:00Z" }],
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<OrderList />);
    await user.type(await screen.findByLabelText("Result for Asha Rao"), "WBC 6.1");
    await user.click(screen.getByRole("button", { name: "Save result" }));
    await waitFor(() => expect(screen.getByText("Result: WBC 6.1")).toBeInTheDocument());
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pnpm --filter @hms/pharmacy test && pnpm --filter @hms/lab test`
Expected: FAIL (old implementations don't invalidate/refetch through React Query; assertions on fetch shape fail).

- [ ] **Step 3: Rewrite the three components**

`dispense-list.tsx`: `useApiQuery<{data: Dispense[]}>(["dispenses"], "/pharmacy/dispenses", { poll: true })`; medication input stays a plain `Input` + `useState` (it's a parameter, not a validated form); mutation `(id: string) => apiFetch(\`/pharmacy/dispenses/${id}/dispense\`, { method: "POST", body: JSON.stringify({ medication }) })` with `successToast: "Dispensed"`, `invalidate: [["dispenses"]]`; per-row busy via `createVisit.isPending && variables === id` pattern — use `mutation.isPending && mutation.variables === d.id`. Keep card layout, `Badge` status, pending-count subtitle, `EmptyState` (icon `Pill`, title "No dispense tasks yet", hint "They appear here when a visit is created in MediCore."), `formatTime` for dispensed_at.

`medications-panel.tsx`: query `["medications"]` → `/pharmacy/medications` (no poll); zod schema `{ name: z.string().min(1, "Name is required").max(200), strength: z.string().max(100) }` with `useZodForm` + `Field` (ids `name`, `strength`); mutation POST `/pharmacy/medications`, `successToast: "Medication added"`, `invalidate: [["medications"]]`, reset on success; `EmptyState` icon `ClipboardList`.

`order-list.tsx`: query `["orders"]` → `/lab/orders`, `{ poll: true }`; drafts stay `useState<Record<string,string>>` (per-row inputs, not a form); mutation `({ id, value }: { id: string; value: string }) => apiFetch(\`/lab/orders/${id}/result\`, { method: "POST", body: JSON.stringify({ result_value: value }) })` with `successToast: "Result saved"`, `invalidate: [["orders"]]`; keep sr-only label `Result for {patient_name}`, Save disabled while pending-for-that-row or draft empty; `EmptyState` icon `FlaskConical`; `formatTime` for resulted_at.

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm --filter @hms/pharmacy test && pnpm --filter @hms/lab test && pnpm turbo lint type-check build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/pharmacy apps/lab
git commit -m "refactor: pharmacy and lab panels on react query with toasts and empty states"
```

---

### Task 8: Migrate the shell login page to RHF + zod

**Files:**
- Modify: `apps/shell/app/login/page.tsx`
- Test: `apps/shell/app/login/login.test.tsx`

**Interfaces:**
- Consumes: `useZodForm`, `Field` (`@hms/ui`); `Button`, `Input` (`@tesserix/web`). Firebase sign-in flow unchanged (`signInWithEmailAndPassword` + POST `/api/session`).
- Produces: E2E strings preserved: labels "Email"/"Password", button "Sign in". Dev prefill stays via `defaultValues` gated on `NODE_ENV !== "production"`.

- [ ] **Step 1: Write the failing test**

`apps/shell/app/login/login.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import LoginPage from "./page";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn() }),
}));
vi.mock("firebase/auth", () => ({
  signInWithEmailAndPassword: vi.fn(),
}));
vi.mock("@/lib/firebase", () => ({ firebaseAuth: () => ({}) }));

describe("LoginPage", () => {
  it("prefills dev credentials and validates inline", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);
    expect(screen.getByLabelText("Email")).toHaveValue("test@hms.dev");
    await user.clear(screen.getByLabelText("Email"));
    await user.clear(screen.getByLabelText("Password"));
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findAllByRole("alert")).not.toHaveLength(0);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter @hms/shell test`
Expected: FAIL — current page uses controlled state + `required` attributes (native validation blocks submit, no role=alert).

- [ ] **Step 3: Rewrite the form part of `login/page.tsx`**

Keep the firebase/session logic and the card layout; replace state with:

```tsx
const loginSchema = z.object({
  email: z.string().email("Enter a valid email"),
  password: z.string().min(1, "Password is required"),
});

const form = useZodForm(loginSchema, {
  email: process.env.NODE_ENV !== "production" ? "test@hms.dev" : "",
  password: process.env.NODE_ENV !== "production" ? "password123" : "",
});
```

Form element gets `noValidate onSubmit={form.handleSubmit(onSubmit)}`; fields use `Field id="email" label="Email" error={...}` wrapping `<Input id="email" type="email" {...form.register("email")} />` (same for password); the sign-in failure path keeps the existing `role="alert"` paragraph driven by a `useState<string | null>` error. Submit button disabled on `form.formState.isSubmitting`.

- [ ] **Step 4: Run tests, build, and the E2E**

Run: `pnpm --filter @hms/shell test && pnpm turbo lint type-check build && cd e2e && npx playwright test`
Expected: unit PASS, build green, E2E journey PASS against the running stack (restart `pnpm turbo dev` if it is not running).

- [ ] **Step 5: Commit**

```bash
git add apps/shell
git commit -m "refactor: login form on react-hook-form and zod with inline errors"
```

---

### Task 9: Standards doc, CLAUDE.md, repo skill

**Files:**
- Create: `docs/standards/frontend.md`, `CLAUDE.md` (hms repo root), `.claude/skills/hms-frontend/SKILL.md`

**Interfaces:**
- Consumes: everything shipped in Tasks 1–8 (the doc links to the migrated panels as reference implementations).
- Produces: the written standards; agent enforcement files.

- [ ] **Step 1: Write `docs/standards/frontend.md`**

Sections (write full prose; the spec `docs/superpowers/specs/2026-08-04-frontend-standards-design.md` section 5 lists exact content — copy its bullet content and expand each into 2–5 sentences with code snippets where noted):

1. **Zone-app anatomy** — port table (shell 4301, medicore 4302, pharmacy 4303, lab 4304, next zone 4305+), `basePath`, `output: "standalone"`, `transpilePackages: ["@hms/ui", "@hms/api"]`, direct-hit `/api/:path*` rewrite with `basePath: false`, the shell rewrite pair, globals.css required shape (show the exact current `apps/pharmacy/app/globals.css` content), `pnpm new-zone` pointer.
2. **Server vs client components** — pages/layouts are server components; interactivity lives in `components/*` client components; data fetching happens client-side through `@hms/api` (session cookie flows via the browser; server components must not call the Go API).
3. **Data fetching** — `useApiQuery`/`useApiMutation` only; no raw fetch in components; polling via `{ poll: true }`; mutations invalidate their query keys; reference: `apps/medicore/components/visit-panel.tsx`.
4. **Forms** — `useZodForm` + `Field`, `noValidate` mandatory, inline errors; reference: visit-panel and login page.
5. **UX vocabulary** — sonner toasts for action feedback (success verbs match button verbs: "Create visit" → "Visit created"); `ConfirmDialog` only for destructive confirmations; banned: `alert`/`confirm`/`prompt`, native validation popups.
6. **Chrome & navigation** — plain `<a>` everywhere for cross-zone/sidebar; zone nav only in `packages/ui/src/zones.ts`; `HmsShell` `active` prop is the current absolute path.
7. **Tokens & the two @tesserix/web pitfalls** — quote the comment blocks from `packages/ui/styles.css` verbatim.
8. **App Router files** — error/loading/not-found required per zone; layouts wrap `AppProviders`.
9. **Copy rules** — sentence case; buttons are verbs; errors say what happened + how to recover; no apologies.
10. **A11y baseline** — label association, `aria-current`, visible focus, keyboard reachability, `role="alert"`, reduced motion.
11. **Testing** — every panel gets a Vitest + Testing Library test (happy path + empty + validation/error) using `renderWithProviders`; Playwright smoke covers each zone journey; commands (`pnpm turbo lint type-check test build`, `cd e2e && npx playwright test`).
12. **Env** — `defineEnv` per app, `.env.example` kept current, `NEXT_PUBLIC_` only for values safe to ship to browsers.

- [ ] **Step 2: Write `CLAUDE.md` (hms root)**

```markdown
# HMS — agent rules

Binding rules for all frontend work. Full document: docs/standards/frontend.md

- Data fetching: `useApiQuery`/`useApiMutation` from `@hms/api` only. Never raw fetch/useState/setInterval in components.
- Forms: `useZodForm` + `Field` from `@hms/ui`, `noValidate` on every form, inline zod errors. Native browser validation is banned.
- Feedback: sonner toasts (success verb matches the button verb). `ConfirmDialog` only for destructive confirmations. `alert`/`confirm`/`prompt` are lint errors.
- Navigation: cross-zone and sidebar links are plain `<a>`. Zone nav lives only in `packages/ui/src/zones.ts`.
- Styling: design tokens only — no hardcoded colors. Read the pitfall comments in `packages/ui/styles.css` before touching sidebar/border styles.
- New zone apps: run `pnpm new-zone <name>`; never hand-copy an app.
- Every new/changed panel component needs a Vitest test using `renderWithProviders` from `@hms/api/testing`.
- Before done: `pnpm turbo lint type-check test build` green; keep `e2e/tests/smoke.spec.ts` selectors working.
- Backend: modules under `backend/internal/modules/*` never import each other; tenant tables need forced RLS (see phase specs in docs/superpowers/specs/).
```

- [ ] **Step 3: Write `.claude/skills/hms-frontend/SKILL.md`**

```markdown
---
name: hms-frontend
description: Use when writing or modifying any HMS frontend code (apps/*, packages/ui, packages/api) — loads the binding standards, reference implementations, and known pitfalls.
---

# HMS frontend standards

Read `docs/standards/frontend.md` for the full rules. The short version and where to copy from:

| Concern | Rule | Reference |
|---|---|---|
| Data | useApiQuery/useApiMutation from @hms/api | apps/medicore/components/visit-panel.tsx |
| Forms | useZodForm + Field, noValidate, inline errors | apps/shell/app/login/page.tsx |
| Feedback | sonner toasts; ConfirmDialog for destructive only | packages/ui/src/confirm-dialog.tsx |
| Empty/loading | EmptyState + app/loading.tsx skeletons | apps/pharmacy/components/dispense-list.tsx |
| Nav | plain <a>; registry packages/ui/src/zones.ts | packages/ui/src/hms-shell.tsx |
| Tokens | no hardcoded colors; read styles.css pitfalls | packages/ui/styles.css |
| New zone | pnpm new-zone <name> | scripts/new-zone.mjs |
| Tests | renderWithProviders per panel | apps/medicore/components/visit-panel.test.tsx |

Never: alert/confirm/prompt, native form validation, raw fetch in components, next/link for cross-zone hops, hex colors in classNames.
```

- [ ] **Step 4: Commit**

```bash
git add docs/standards CLAUDE.md .claude
git commit -m "docs: frontend standards, agent rules and hms-frontend skill"
```

---

### Task 10: `pnpm new-zone` generator

**Files:**
- Create: `scripts/new-zone.mjs`
- Modify: root `package.json` (add `"new-zone": "node scripts/new-zone.mjs"` to scripts)

**Interfaces:**
- Consumes: the final-state file shapes from Tasks 1, 5 (an app's package.json, next.config.ts, tsconfig, postcss, globals.css, layout with AppProviders, error/loading/not-found, eslint.config.mjs, vitest.config.mts, .env.example).
- Produces: `pnpm new-zone <name>` creates `apps/<name>` ready to `pnpm install && pnpm turbo build --filter=@hms/<name>`, and prints follow-ups.

- [ ] **Step 1: Write the generator**

`scripts/new-zone.mjs` — plain Node, no deps. Behavior:

1. `const name = process.argv[2]` — validate `/^[a-z][a-z0-9-]*$/`, refuse if `apps/<name>` exists.
2. Port: scan `apps/*/package.json` `dev` scripts for `-p (\d+)`, take `max + 1`.
3. Write these files from template literals (copy the EXACT current content of the pharmacy app as the template source at implementation time, substituting name/port/basePath):
   - `package.json` (name `@hms/<name>`, dev/start `-p <port>`, same deps/devDeps as pharmacy including `@hms/api`, `@hms/ui`, test/lint scripts)
   - `next.config.ts` (basePath `/<name>`, `transpilePackages: ["@hms/ui", "@hms/api"]`, direct-hit rewrite)
   - `tsconfig.json`, `postcss.config.mjs`, `eslint.config.mjs`, `vitest.config.mts`, `.env.example`
   - `app/globals.css` (the standard shape), `app/layout.tsx` (AppProviders), `app/error.tsx`, `app/loading.tsx`, `app/not-found.tsx`
   - `app/page.tsx` — HmsShell wrapper with `active="/<name>"` and an example panel
   - `components/example-panel.tsx` — a minimal `useApiQuery` panel with `EmptyState`
   - `components/example-panel.test.tsx` — a `renderWithProviders` test with a mocked fetch returning `{data: []}` asserting the empty state renders
4. Print follow-ups (colored/plain console.warn is fine — script context, `no-console` doesn't apply to scripts; add `/* eslint-disable no-console */` header):

```
Created apps/<name> on port <port>. Manual follow-ups:
1. apps/shell/next.config.ts — add:
   const <NAME>_URL = process.env.<NAME>_URL ?? "http://localhost:<port>";
   { source: "/<name>", destination: `${<NAME>_URL}/<name>` },
   { source: "/<name>/:path*", destination: `${<NAME>_URL}/<name>/:path*` },
2. packages/ui/src/zones.ts — add a Zone entry (icon + pages).
3. README/Makefile — mention the new port.
Then: pnpm install && pnpm turbo lint type-check test build --filter=@hms/<name>
```

- [ ] **Step 2: Smoke-test the generator**

Run:

```bash
pnpm new-zone testzone
pnpm install
pnpm turbo lint type-check test build --filter=@hms/testzone
```

Expected: all green. Then delete the scratch zone completely:

```bash
rm -rf apps/testzone && pnpm install
git status --short   # must show only scripts/new-zone.mjs + package.json + lockfile
```

- [ ] **Step 3: Commit**

```bash
git add scripts/new-zone.mjs package.json pnpm-lock.yaml
git commit -m "feat: new-zone generator stamping standards-compliant zone apps"
```

---

### Task 11: Full verification

**Files:** none — verification + fix-forward only (small fixes committed as `fix:`; structural problems reopen the owning task).

- [ ] **Step 1: Whole-repo suites**

```bash
pnpm turbo lint type-check test build
cd backend && go test -race ./...
```

Expected: all green (backend untouched — this confirms no accidental damage).

- [ ] **Step 2: Live E2E**

Stack up (infra containers may already run): `make dev-infra && make seed`, background `make dev-api` and `pnpm turbo dev`, wait for `curl -s localhost:8080/readyz` → ready and 4301–4304 responding.

```bash
cd e2e && npx playwright test
```

Expected: journey passes. Also visually confirm (agent-browser or Playwright screenshots): toasts appear on visit create/dispense/result actions; submitting the empty visit form shows an inline "Patient name is required" error and NO native browser bubble; error boundary renders when the API is stopped (optional spot check).

- [ ] **Step 3: Teardown background dev servers if this task started them; commit any fix-forwards**

```bash
git status
git add -A && git commit -m "fix: frontend standards verification fixes"   # only if changes exist
```

---

## Self-review notes

- Spec coverage: D1→Tasks 1,9,10; D2→Tasks 2,3,6–8; D3→Tasks 4,6,8; D4→Tasks 3,4 (ConfirmDialog ships in kit; no current delete flows use it — generator example and doc reference it); D5→Tasks 1–8 tests; D6→doc Task 9. Spec deliverable 4 (App Router files)→Task 5; deliverable 7 (generator)→Task 10; deliverable 8 (migration)→Tasks 6–8; env validation→Tasks 2,5.
- Name consistency: `apiFetch`/`ApiError`/`POLL_INTERVAL_MS`/`defineEnv` (T2), `AppProviders`/`useApiQuery`/`useApiMutation`/`renderWithProviders` (T3), `ConfirmDialog`/`useZodForm`/`Field`/`EmptyState`/`formatTime`/`formatDateTime`/`sanitizeHtml` (T4) — used identically in T5–T10.
- E2E-critical strings called out in Global Constraints and repeated in Tasks 6–8.
- Known risk (documented in tasks): `@tesserix/web` component rendering under Vitest/jsdom may need its CSS import mocked — handled in Task 4 Step 5 without mocking components.
